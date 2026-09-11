#!/usr/bin/env bash
# Verify which framework kernel caches survive being moved to a different
# absolute path, i.e. whether MCV can ship them in one OCI image and mount them
# somewhere other than where they were produced.
#
# Copy into a running container and run it there:
#   podman cp scripts/verify-cache-relocatability.sh <ctr>:/tmp/verify.sh
#   podman exec -it <ctr> bash /tmp/verify.sh
#
# Needs a GPU for the compile checks (skip with SKIP_GPU=1). Writes a report to
# $SANDBOX/report.txt; existing caches are only ever read.
#
# Verdicts:
#   PORTABLE       cache still hits from a different absolute path
#   NOT-PORTABLE   cache misses from a different absolute path (mount must
#                  reproduce the capture path, or paths must be rewritten)
#   INCONCLUSIVE   could not decide from the evidence gathered

set -uo pipefail

SANDBOX=${SANDBOX:-/tmp/mcv-cache-verify}
SKIP_GPU=${SKIP_GPU:-0}
REPORT="$SANDBOX/report.txt"
PY=${PYTHON:-$(command -v python3 || command -v python || true)}

mkdir -p "$SANDBOX"
: >"$REPORT"

say() { printf '%s\n' "$*" | tee -a "$REPORT"; }
verdict() { printf 'RESULT %-28s %s — %s\n' "$1" "$2" "$3" | tee -a "$REPORT"; }

if [ -z "$PY" ]; then
    say "no python interpreter found; aborting"
    exit 1
fi

# ---------------------------------------------------------------------------
# 0. inventory: what a warm vLLM actually left behind, and which compile
#    adaptor vLLM would select (only the legacy InductorAdaptor redirects
#    TRITON_CACHE_DIR, so this decides where Triton artifacts land).
# ---------------------------------------------------------------------------
say "== inventory ($(date -u +%FT%TZ)) =="
say "python : $PY ($("$PY" -V 2>&1))"
for v in VLLM_CACHE_ROOT TRITON_CACHE_DIR TORCHINDUCTOR_CACHE_DIR \
    FLASHINFER_WORKSPACE_BASE NUMBA_CACHE_DIR OUTLINES_CACHE_DIR \
    DG_JIT_CACHE_DIR CUDA_CACHE_DISABLE TILELANG_CLEANUP_TEMP_FILES \
    VLLM_USE_AOT_COMPILE VLLM_USE_STANDALONE_COMPILE VLLM_USE_MEGA_AOT_ARTIFACT \
    VLLM_DISABLE_COMPILE_CACHE XDG_CACHE_HOME HOME; do
    val=${!v:-}
    say "  env ${v}=${val:-<unset>}"
done

say "  -- candidate cache trees (size, entries) --"
seen="|"
for d in "${VLLM_CACHE_ROOT:-$HOME/.cache/vllm}" \
    "${TRITON_CACHE_DIR:-$HOME/.triton/cache}" \
    "${FLASHINFER_WORKSPACE_BASE:+$FLASHINFER_WORKSPACE_BASE/.cache/flashinfer}" \
    "$HOME/.cache/flashinfer" \
    "/tmp/torchinductor_$(id -un)" \
    "${DG_JIT_CACHE_DIR:-${VLLM_CACHE_ROOT:-$HOME/.cache/vllm}/deep_gemm}"; do
    [ -n "$d" ] && [ -e "$d" ] || continue
    case "$seen" in *"|$d|"*) continue ;; esac
    seen="$seen$d|"
    say "     $(du -sh "$d" 2>/dev/null | cut -f1)  $(find "$d" -mindepth 1 -maxdepth 3 2>/dev/null | wc -l) entries  $d"
done

"$PY" - <<'PY' | tee -a "$REPORT"
import os
try:
    import torch
    print(f"  torch {torch.__version__}  cuda_avail {torch.cuda.is_available()}")
except Exception as e:  # pragma: no cover - report only
    print(f"  torch import failed: {e}")
try:
    import triton
    print(f"  triton {triton.__version__}  knobs.cache.dir={os.environ.get('TRITON_CACHE_DIR','<unset>')}")
except Exception as e:
    print(f"  triton import failed: {e}")
try:
    import torch._inductor
    print(f"  standalone_compile available: {hasattr(torch._inductor, 'standalone_compile')}"
          "  (True => InductorStandaloneAdaptor => vLLM never redirects TRITON_CACHE_DIR)")
except Exception as e:
    print(f"  inductor probe failed: {e}")
PY

# Does anything already inside the *real* vLLM cache embed its own absolute
# path? If so, shipping the tree to another path breaks it the same way the
# Triton group files do. Read-only scan.
scan_abs_paths() { # $1 = label, $2 = dir
    local label=$1 dir=$2 hits files
    [ -d "$dir" ] || { say "  ${label}: ${dir} not present — skipped"; return; }
    files=$(find "$dir" -type f 2>/dev/null | head -n 4000)
    if [ -z "$files" ]; then
        say "  ${label}: ${dir} empty — skipped"
        return
    fi
    hits=$(printf '%s\n' "$files" | xargs -r grep -l --binary-files=text -F "$dir" 2>/dev/null | wc -l)
    say "  ${label}: ${hits} of $(printf '%s\n' "$files" | wc -l) files under ${dir} embed that absolute path"
    if [ "$hits" != "0" ]; then
        printf '%s\n' "$files" | xargs -r grep -l --binary-files=text -F "$dir" 2>/dev/null |
            sed -n '1,5p' | sed "s/^/      /" | tee -a "$REPORT"
    fi
}
say "  -- absolute-path embedding (read-only scan of real caches) --"
scan_abs_paths "vllm-cache-root" "${VLLM_CACHE_ROOT:-$HOME/.cache/vllm}"
scan_abs_paths "triton-cache" "${TRITON_CACHE_DIR:-$HOME/.triton/cache}"
scan_abs_paths "inductor-cache" "/tmp/torchinductor_$(id -un)"

if [ "$SKIP_GPU" = "1" ]; then
    say "SKIP_GPU=1 — stopping after inventory"
    exit 0
fi

# ---------------------------------------------------------------------------
# 1. Triton JIT cache: mechanism + functional relocation test.
#    Each measurement runs in a fresh process because Triton also keeps an
#    in-process JIT cache that would mask the disk lookup.
# ---------------------------------------------------------------------------
write_triton_probe() {
    cat >"$SANDBOX/triton_probe.py" <<'PY'
import json
import os
import sys
import time

mode = sys.argv[1]  # fresh | reuse
import torch  # noqa: E402
import triton  # noqa: E402
import triton.language as tl  # noqa: E402

hits, misses = [], []
try:
    from triton import knobs

    def listener(*, src, metadata, metadata_group, times, cache_hit):
        (hits if cache_hit else misses).append(getattr(src, "name", "?"))

    knobs.compilation.listener = listener
except Exception as exc:
    print(f"listener_unavailable {exc}")


@triton.jit
def probe(X, n, BLOCK: tl.constexpr):
    offs = tl.program_id(0) * BLOCK + tl.arange(0, BLOCK)
    tl.store(X + offs, tl.load(X + offs, mask=offs < n) + 1.0, mask=offs < n)


x = torch.ones(1024, device="cuda")
t0 = time.perf_counter()
probe[(1024 // 128,)](x, 1024, BLOCK=128)
torch.cuda.synchronize()
elapsed = time.perf_counter() - t0
print(f"mode={mode} first_launch_s={elapsed:.3f} hits={len(hits)} misses={len(misses)} "
      f"names={sorted(set(hits + misses))[:3]}")
PY
}

run_triton_probe() { # $1 = cache dir, $2 = mode
    TRITON_CACHE_DIR=$1 "$PY" "$SANDBOX/triton_probe.py" "$2" 2>&1 |
        grep -E "mode=|listener_unavailable" | tail -n 2
}

say ""
say "== check 1: Triton JIT cache relocatability =="
say "  (the capture dir is renamed away before the relocated run, otherwise"
say "   get_group() finds the recorded absolute paths and silently reads the"
say "   original tree — which would fake a hit that cannot happen in a pod)"
write_triton_probe
TA="$SANDBOX/triton-A"
TB="$SANDBOX/triton-B"
HIDE="$SANDBOX/triton-A-hidden"
rm -rf "$TA" "$TB" "$HIDE"

fresh=$(run_triton_probe "$TA" fresh)
reuse=$(run_triton_probe "$TA" reuse)
cp -a "$TA" "$TB"
grp=$(find "$TA" -name "__grp__*.json" 2>/dev/null | head -n 1)
mv "$TA" "$HIDE"
reloc=$(run_triton_probe "$TB" relocate)
mv "$HIDE" "$TA"
say "  fresh            : $fresh"
say "  reuse (same path): $reuse"
say "  relocated (orig hidden): $reloc"

if [ -n "$grp" ]; then
    say "  mechanism : $grp ->"
    "$PY" - "$grp" "$TB" <<'PY' | tee -a "$REPORT"
import glob
import json
import os
import sys

grp, alt = sys.argv[1], sys.argv[2]
try:
    paths = list(json.load(open(grp)).get("child_paths", {}).values())
except Exception as exc:
    print(f"      could not parse group file: {exc}")
    raise SystemExit(0)
captured_root = os.path.dirname(os.path.dirname(grp))
outside = [p for p in paths if not p.startswith(alt + os.sep)]
print(f"      {len(paths)} child_paths; all absolute: "
      f"{all(os.path.isabs(p) for p in paths)}; captured root: {captured_root}")
print(f"      sample: {paths[0] if paths else None}")
print(f"      unusable if the tree is mounted at {alt}: {len(outside)}/{len(paths)}")
PY
fi

if echo "$reuse" | grep -q "hits=1" && echo "$reloc" | grep -q "misses=1"; then
    verdict "triton-jit-cache" "NOT-PORTABLE" "hit in place, recompiled once the capture path was absent"
elif echo "$reloc" | grep -q "hits=1"; then
    verdict "triton-jit-cache" "PORTABLE" "hit with the capture path absent"
else
    verdict "triton-jit-cache" "INCONCLUSIVE" "listener absent or unexpected output; weigh the child_paths evidence"
fi

# ---------------------------------------------------------------------------
# 1b. Can MCV make a Triton tree relocatable by rewriting the absolute paths
#     recorded in __grp__*.json? If this hits, path-rewriting at build time is
#     a valid alternative to pinning mount paths.
# ---------------------------------------------------------------------------
say ""
say "== check 1b: Triton cache with rewritten child_paths =="
TC="$SANDBOX/triton-C"
rm -rf "$TC"
cp -a "$TA" "$TC"
"$PY" - "$TA" "$TC" <<'PY' | tee -a "$REPORT"
import glob
import json
import os
import sys

src_root, dst_root = sys.argv[1], sys.argv[2]
changed = 0
for f in glob.glob(os.path.join(dst_root, "*", "__grp__*.json")):
    data = json.load(open(f))
    kids = data.get("child_paths", {})
    new = {k: (v.replace(src_root + os.sep, dst_root + os.sep)
               if isinstance(v, str) and v.startswith(src_root + os.sep) else v)
           for k, v in kids.items()}
    if new != kids:
        data["child_paths"] = new
        with open(f, "w") as fh:
            json.dump(data, fh)
        changed += 1
print(f"  rewrote {changed} group files from {src_root} to {dst_root}")
PY
mv "$TA" "$HIDE"
rew=$(run_triton_probe "$TC" rewrite-relocate)
mv "$HIDE" "$TA"
say "  relocated after rewrite: $rew"
if echo "$rew" | grep -q "hits=1"; then
    verdict "triton-path-rewrite" "WORKS" "hit after rewriting child_paths; build-time rewrite is viable"
elif echo "$rew" | grep -q "misses=1"; then
    verdict "triton-path-rewrite" "FAILS" "still recompiled; must pin the capture path at mount time"
else
    verdict "triton-path-rewrite" "INCONCLUSIVE" "no listener signal"
fi

# ---------------------------------------------------------------------------
# 2. Triton autotune cache (TRITON_CACHE_AUTOTUNING=1) — same tree, different
#    mechanism, so it needs its own verdict.
# ---------------------------------------------------------------------------
# Triton's autotune cache is a plain <name>.autotune.json written through
# FileCacheManager.put and found via get_file (autotuner.py:191-209), i.e. the
# path is derived from the live TRITON_CACHE_DIR instead of being recorded in a
# __grp__*.json. So it should survive relocation, unlike kernel artifacts.
cat >"$SANDBOX/autotune_probe.py" <<'PY'
import sys
import time

import torch
import triton
import triton.language as tl

mode = sys.argv[1]
configs = [triton.Config({"BLOCK": b}, num_warps=w) for b in (64, 128) for w in (1, 2)]


@triton.autotune(configs=configs, key=["n"])
@triton.jit
def probe_a(X, n, BLOCK: tl.constexpr):
    offs = tl.program_id(0) * BLOCK + tl.arange(0, BLOCK)
    tl.store(X + offs, tl.load(X + offs, mask=offs < n) + 1.0, mask=offs < n)


x = torch.ones(4096, device="cuda")
t0 = time.perf_counter()
probe_a[(4096 // 64,)](x, 4096)
torch.cuda.synchronize()
print(f"mode={mode} first_call_s={time.perf_counter() - t0:.3f}")
PY

autotune_state() { # $1 = cache dir -> "<name>.autotune.json" inventory
    "$PY" - "$1" <<'PY'
import glob
import os
import sys

files = glob.glob(os.path.join(sys.argv[1], "*", "*.autotune.json"))
newest = max((os.path.getmtime(f) for f in files), default=0.0)
print(f"autotune_files={len(files)} newest_mtime={newest:.3f}")
PY
}

say ""
say "== check 2: Triton autotune cache relocatability =="
AA="$SANDBOX/autotune-A"
AB="$SANDBOX/autotune-B"
AAH="$SANDBOX/autotune-A-hidden"
rm -rf "$AA" "$AB" "$AAH"
run_autotune_probe() { # $1 = cache dir, $2 = mode
    TRITON_CACHE_DIR=$1 TRITON_CACHE_AUTOTUNING=1 "$PY" "$SANDBOX/autotune_probe.py" "$2" 2>&1 |
        grep -E "mode=|Error|Traceback" | tail -n 3
}
a1=$(run_autotune_probe "$AA" fresh)
s_fresh=$(autotune_state "$AA")
a2=$(run_autotune_probe "$AA" reuse)
s_reuse=$(autotune_state "$AA")
cp -a "$AA" "$AB"
s_before=$(autotune_state "$AB")
mv "$AA" "$AAH"
a3=$(run_autotune_probe "$AB" relocate)
mv "$AAH" "$AA"
s_after=$(autotune_state "$AB")
say "  fresh (same path)     : $a1 | $s_fresh"
say "  reuse (same path)     : $a2 | $s_reuse"
say "  relocated (orig hidden): $a3 | $s_after"
say "  relocated tree before  : $s_before"
if printf '%s' "$a1" | grep -q "Traceback"; then
    verdict "triton-autotune-cache" "INCONCLUSIVE" "probe failed to compile; raw output above"
elif printf '%s' "$s_after" | grep -q "autotune_files=0"; then
    verdict "triton-autotune-cache" "INCONCLUSIVE" "no autotune.json written; probe never autotuned"
elif [ "$s_before" = "$s_after" ]; then
    verdict "triton-autotune-cache" "PORTABLE" "get_file() path is recomputed from TRITON_CACHE_DIR; json neither replaced nor rewritten"
else
    verdict "triton-autotune-cache" "NOT-PORTABLE" "autotune.json re-benchmarked/rewritten after relocation ($s_before -> $s_after)"
fi

# ---------------------------------------------------------------------------
# 3. Inductor cache (only matters for the inline compile path; the AOT path
#    sets TORCHINDUCTOR_CACHE_DIR to .../torch_aot_compile/<hash>/inductor_cache).
# ---------------------------------------------------------------------------
cat >"$SANDBOX/inductor_probe.py" <<'PY'
import sys
import time

import torch
import torch._dynamo.utils as du
import torch._inductor.utils as iu

mode = sys.argv[1]


def counters():
    out = {}
    c = getattr(iu, "compilation_counter", None)
    if c is not None:
        for k in dir(c):
            if k.startswith("_"):
                continue
            v = getattr(c, k, None)
            if isinstance(v, int) and not isinstance(v, bool):
                out["cc." + k] = v
    try:
        for k, v in dict(du.counters.get("inductor", {})).items():
            if isinstance(v, int):
                out["fx." + str(k)] = v
    except Exception:
        pass
    return out


before = counters()


def f(x):
    return (x.sin() * 2 + x.cos()).sum()


x = torch.randn(2048, device="cuda")
cf = torch.compile(f)
t0 = time.perf_counter()
cf(x)
torch.cuda.synchronize()
elapsed = time.perf_counter() - t0
after = counters()
delta = {k: after[k] - before.get(k, 0) for k in after if after[k] != before.get(k, 0)}
print(f"mode={mode} wall_s={elapsed:.2f} "
      f"kernels_created={delta.get('cc.num_fused_kernel_created', 0)} "
      f"fx_hit={delta.get('fx.fxgraph_cache_hit', 0)} "
      f"fx_miss={delta.get('fx.fxgraph_cache_miss', 0)} counters_seen={sorted(before)} delta={delta}")
PY

say ""
say "== check 3: Inductor cache relocatability =="
IA="$SANDBOX/inductor-A"
IB="$SANDBOX/inductor-B"
IAH="$SANDBOX/inductor-A-hidden"
rm -rf "$IA" "$IB" "$IAH"
run_inductor_probe() { # $1 = cache dir, $2 = mode
    TORCHINDUCTOR_CACHE_DIR=$1 TORCHINDUCTOR_COMPILE_THREADS=1 \
        "$PY" "$SANDBOX/inductor_probe.py" "$2" 2>&1 |
        grep -E "mode=|Error|Traceback" | tail -n 3
}
files_in() { find "$1" -type f 2>/dev/null | wc -l; }
i1=$(run_inductor_probe "$IA" fresh)
f_fresh=$(files_in "$IA")
i2=$(run_inductor_probe "$IA" reuse)
cp -a "$IA" "$IB"
f_before=$(files_in "$IB")
mv "$IA" "$IAH"
i3=$(run_inductor_probe "$IB" relocate)
mv "$IAH" "$IA"
f_after=$(files_in "$IB")
say "  fresh (same path)     : $i1"
say "  reuse (same path)     : $i2 | ${f_fresh} files"
say "  relocated (orig hidden): $i3 | files ${f_before} -> ${f_after}"
scan_abs_paths "inductor-copy" "$IB"
if printf '%s' "$i3" | grep -q "Traceback"; then
    verdict "inductor-cache" "INCONCLUSIVE" "probe failed; raw output above"
elif printf '%s' "$i3" | grep -q "fx_miss=[1-9]"; then
    verdict "inductor-cache" "NOT-PORTABLE" "fxgraph cache miss with the capture path absent"
elif printf '%s' "$i3" | grep -qE "kernels_created=[1-9]"; then
    verdict "inductor-cache" "NOT-PORTABLE" "kernels recompiled with the capture path absent"
elif [ "$f_before" != "$f_after" ]; then
    verdict "inductor-cache" "NOT-PORTABLE" "no hit counters but the tree grew by $((f_after - f_before)) files"
elif printf '%s' "$i3" | grep -q "fx_hit=0 " && [ "$f_fresh" = "$f_after" ]; then
    verdict "inductor-cache" "INCONCLUSIVE" "no counters and no growth; both runs may have been in-process no-ops"
else
    verdict "inductor-cache" "PORTABLE" "hit counters moved with the tree and nothing was rewritten"
fi

# ---------------------------------------------------------------------------
# 4. FlashInfer JIT workspace: built .so + generated sources usually bake in
#    absolute build paths. Grep-only (no extra build).
# ---------------------------------------------------------------------------
say ""
say "== check 4: FlashInfer workspace =="
FW=""
for c in "${FLASHINFER_WORKSPACE_BASE:-}/.cache/flashinfer" \
    "${FLASHINFER_WORKSPACE_BASE:-}/flashinfer" \
    "$HOME/.cache/flashinfer" \
    "/tmp/.cache/flashinfer" \
    "/tmp/flashinfer"; do
    [ -n "$c" ] && [ -d "$c" ] && FW=$c && break
done
if [ -z "$FW" ]; then
    found=$(find "${FLASHINFER_WORKSPACE_BASE:-/tmp}" /tmp "$HOME" -maxdepth 4 -type d \
        \( -name 'flashinfer*' -o -path '*/.cache/flashinfer' \) 2>/dev/null | head -n 3)
    say "  fallback search hits: ${found:-none}"
    FW=$(printf '%s\n' "$found" | head -n 1)
fi
if [ -n "$FW" ]; then
    say "  workspace: $FW"
    scan_abs_paths "flashinfer-workspace" "$FW"
    ninja_n=$(find "$FW" -name 'build.ninja' 2>/dev/null | wc -l)
    so_n=$(find "$FW" -name '*.so' 2>/dev/null | wc -l)
    say "  build artifacts: ${ninja_n} build.ninja, ${so_n} .so"
    verdict "flashinfer-workspace" "SEE-EVIDENCE" "count above tells whether abs paths are baked in; if nonzero, mount at the capture path"
else
    verdict "flashinfer-workspace" "INCONCLUSIVE" "no workspace found — start vLLM once with FlashInfer enabled, then rerun"
fi

# ---------------------------------------------------------------------------
# 5. Write access: Triton writes into TRITON_CACHE_DIR whenever a kernel
#    specialization is missing (FileCacheManager.put has no fallback), so a
#    read-only mount can be fatal rather than merely slower. Force a miss
#    inside a read-only copy of the cache.
# ---------------------------------------------------------------------------
cat >"$SANDBOX/ro_probe.py" <<'PY'
import sys

import torch
import triton
import triton.language as tl

mode = sys.argv[1]


@triton.jit
def probe_ro(X, n, BLOCK: tl.constexpr):
    offs = tl.program_id(0) * BLOCK + tl.arange(0, BLOCK)
    tl.store(X + offs, tl.load(X + offs, mask=offs < n) + 1.0, mask=offs < n)


x = torch.ones(1024, device="cuda")
try:
    probe_ro[(1024 // 256,)](x, 1024, BLOCK=256)  # specialization absent from the copy
    torch.cuda.synchronize()
    print(f"mode={mode} result=compiled-ok")
except Exception as exc:  # noqa: BLE001 - the point of the check
    print(f"mode={mode} result=raised {type(exc).__name__}: {str(exc)[:120]}")
PY

say ""
say "== check 5: Triton cache mounted read-only =="
if [ "$(id -u)" = "0" ]; then
    verdict "triton-read-only" "INCONCLUSIVE" "running as uid 0; root bypasses the chmod used to simulate a RO mount — treat as writable-required anyway"
else
    RO="$SANDBOX/triton-RO"
    rm -rf "$RO"
    cp -a "$TA" "$RO"
    chmod -R a-w "$RO"
    r1=$(TRITON_CACHE_DIR="$RO" "$PY" "$SANDBOX/ro_probe.py" readonly 2>&1 | grep "mode=" | tail -n 1)
    chmod -R u+w "$RO"
    say "  forced miss in a read-only tree: ${r1:-<no output>}"
    if printf '%s' "$r1" | grep -q "result=raised"; then
        verdict "triton-read-only" "REQUIRES-WRITE" "a cache miss inside a RO tree raises; the payload must be writable (CachePlan.RequiresWritable)"
    elif printf '%s' "$r1" | grep -q "result=compiled-ok"; then
        verdict "triton-read-only" "READ-ONLY-OK" "miss compiled without writing"
    else
        verdict "triton-read-only" "INCONCLUSIVE" "no probe output"
    fi
fi

say ""
say "== how to read this for MCV =="
say "  NOT-PORTABLE  => one payload subtree must be mounted at exactly the path"
say "                   it was captured from (record it in the manifest), or"
say "                   rewrite the recorded paths at build time."
say "  PORTABLE      => free to nest under VLLM_CACHE_ROOT and point the env at it."
say ""
say "report: $REPORT"
