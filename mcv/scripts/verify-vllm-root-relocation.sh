#!/usr/bin/env bash
# Does a captured $VLLM_CACHE_ROOT tree still produce a compile-cache hit when
# mounted at a different absolute path?
#
# RHAII produces its cache at /tmp/vllm while MCV injects at
# /home/kserve/.cache/vllm, and a scan of the real tree shows files under
# .../torch_aot_compile/<sha>/inductor_cache embedding "/tmp/vllm". This test
# decides whether that embedding is load-bearing.
#
# Run from the HOST (it drives podman), not inside the container:
#
#   IMAGE=registry.redhat.io/rhaii-early-access/vllm-cuda-rhel9:3.5.0-ea.2 \
#   HOST_CACHE=/path/to/copied-vllm-tree \
#   HOST_HF_CACHE=./rhaii-cache \
#   MODEL=RedHatAI/Llama-3.2-1B-Instruct-FP8 \
#   EXTRA_PODMAN_ARGS='-v ./rhaii-cache/triton:/tmp/triton:Z' \
#   ./scripts/verify-vllm-root-relocation.sh
#
# HOST_CACHE must be a *copy* of the captured cache; the original is untouched.
# Mount the captured Triton tree back at its capture path via EXTRA_PODMAN_ARGS
# so Triton hits and the only variable under test is VLLM_CACHE_ROOT.

set -uo pipefail

IMAGE=${IMAGE:?set IMAGE=<vllm image>}
HOST_CACHE=${HOST_CACHE:?set HOST_CACHE=<host dir holding the captured cache>}
MODEL=${MODEL:-RedHatAI/Llama-3.2-1B-Instruct-FP8}
NEWROOT=${NEWROOT:-/var/tmp/vllm-relocated}
WAIT=${WAIT:-420}
DEVICE=${DEVICE:-nvidia.com/gpu=all}
LOG=$(mktemp)
trap 'rm -f "$LOG"' EXIT

if [ ! -d "$HOST_CACHE/torch_compile_cache" ]; then
    printf 'HOST_CACHE does not look like a vLLM cache root: %s\n' "$HOST_CACHE" >&2
    exit 1
fi

MOUNTS=(-v "$HOST_CACHE:$NEWROOT:Z")
[ -n "${HOST_HF_CACHE:-}" ] && MOUNTS+=(-v "$HOST_HF_CACHE:/opt/app-root/src/.cache:Z")
# shellcheck disable=SC2206
[ -n "${EXTRA_PODMAN_ARGS:-}" ] && read -r -a extra <<<"$EXTRA_PODMAN_ARGS" && MOUNTS+=("${extra[@]}")

# Keep the HF token off the command line (visible in `ps`): podman's `-e VAR`
# pass-through reads it from the environment instead of embedding its value as
# an argument, so export it and add the bare name only when it is actually set.
podman_env=(--env "VLLM_CACHE_ROOT=$NEWROOT")
if [ -n "${HUGGING_FACE_HUB_TOKEN:-}" ]; then
    export HUGGING_FACE_HUB_TOKEN
    podman_env+=(--env HUGGING_FACE_HUB_TOKEN)
fi

printf 'starting %s with VLLM_CACHE_ROOT=%s (cache captured elsewhere)\n' \
    "$IMAGE" "$NEWROOT"

# shellcheck disable=SC2086
timeout "$WAIT" podman run --rm \
    --device "$DEVICE" \
    --security-opt=label=disable \
    --shm-size=4g \
    --userns=keep-id:uid=1001 \
    "${MOUNTS[@]}" \
    "${podman_env[@]}" \
    "$IMAGE" \
    --model "$MODEL" --max-model-len 2048 >"$LOG" 2>&1 &
pid=$!

# Stop as soon as the decisive log line appears; the server itself is not the
# subject of the test.
deadline=$((SECONDS + WAIT))
verdict=""
while [ $SECONDS -lt $deadline ]; do
    kill -0 "$pid" 2>/dev/null || break
    if grep -qE "Directly load AOT compilation|Directly load the compiled graph" "$LOG"; then
        verdict="HIT"
        break
    fi
    if grep -qE "Compiling a graph for compile range|saved AOT compiled function" "$LOG"; then
        verdict="MISS"
        break
    fi
    if grep -qE "Traceback|Engine process failed|error during startup" "$LOG"; then
        verdict="ERROR"
        break
    fi
    sleep 2
done

[ -z "$verdict" ] && grep -qE "Compiling a graph" "$LOG" && verdict="MISS"
[ -z "$verdict" ] && verdict="INCONCLUSIVE"

kill "$pid" 2>/dev/null
wait "$pid" 2>/dev/null

echo
echo "relevant log lines:"
grep -nE "Using cache directory|Directly load|Compiling a graph|torch.compile took|saved AOT|Traceback" \
    "$LOG" | sed 's/^/  /' | tail -n 15
echo
case $verdict in
    HIT)
        printf 'RESULT vllm-cache-root  PORTABLE — artifacts loaded from a different absolute path\n'
        printf '  the embedded capture paths are not load-bearing\n'
        ;;
    MISS)
        printf 'RESULT vllm-cache-root  NOT-PORTABLE — recompiled from a different absolute path\n'
        printf '  MCV must mount at the capture path or rewrite the embedded paths\n'
        ;;
    ERROR)
        printf 'RESULT vllm-cache-root  ERROR — startup failed; inspect the lines above\n'
        ;;
    *)
        printf 'RESULT vllm-cache-root  INCONCLUSIVE — decisive line never appeared within %ss\n' "$WAIT"
        ;;
esac
