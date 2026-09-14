# GPU Kernel Cache Image Specification (compat)

## Introduction

This document describes the **compat** cache image format: the format MCV
**create** produces and MCV **extract** (and `gkm-extract`) expects for
Triton and vLLM kernel caches.

Compat images store cache content in a **standard registry layer** (a gzip
tarball), not a custom OCI artifact media type. They work with Docker, Podman,
Buildah, and registries such as Quay without custom artifact support.

## How to identify a compat image

**Use layer media type, not manifest media type.**

| Signal | Used for extract? | Notes |
|--------|-------------------|-------|
| Layer media type | **Yes** | Must be one of the compat types below |
| Manifest media type (`OCI` vs `Docker Schema 2`) | **No** | Registries often serve compat layers under an OCI manifest |
| `cache.*.image/variant=compat` annotation | **No** | Optional; informational only |
| Image config labels (`cache.triton.image/*`, `cache.vllm.image/*`) | **Yes** | Cache type, size, summary, entry count |

### Compat layer media types

Every layer that contains cache data must use one of:

- `application/vnd.docker.image.rootfs.diff.tar.gzip` (typical for Docker-built images)
- `application/vnd.oci.image.layer.v1.tar+gzip` (typical for Buildah/Podman-built images)

MCV extract accepts **both** types regardless of whether the image manifest
is OCI or Docker Schema 2.

### Common registry layout

After `docker push` or `buildah push`, it is normal to see:

- Manifest: `application/vnd.oci.image.manifest.v1+json`
- Layer: `application/vnd.docker.image.rootfs.diff.tar.gzip`

This is still a compat image. Extract must not branch on manifest type.

## Layer contents

A compat image has **one squashed layer** (recommended) containing:

| Path | Content |
|------|---------|
| `io.triton.cache/` or `io.vllm.cache/` | Cache directory tree |
| `io.vllm.cache/<tree>/` | Optional extra cache tree captured with `--source` (see below) |
| `io.triton.manifest/manifest.json` or `io.vllm.manifest/manifest.json` | Entry metadata written at create time |

The gzip tarball layer holds the paths above. MCV unpacks cache files into
the configured extract directory and manifest into `/tmp/.mcv/manifest/`.

Keep images **single-layered**: MCV stages every captured tree inside the one
payload prefix precisely so no additional `COPY`/`ADD` layer is introduced,
because signatures over multi-layer images (cosign) are unreliable.

## Image config labels

Labels are set on the image config (not the manifest). Required keys depend
on cache type:

### Triton

| Label | Description |
|-------|-------------|
| `cache.triton.image/entry-count` | Number of cache entries |
| `cache.triton.image/summary` | JSON summary of targets (backend, arch, warp size) |
| `cache.triton.image/cache-size-bytes` | Total bytes of **packaged** cache files (see below) |

### vLLM

| Label | Description |
|-------|-------------|
| `cache.vllm.image/entry-count` | Number of cache entries |
| `cache.vllm.image/summary` | JSON summary of targets |
| `cache.vllm.image/cache-size-bytes` | Total bytes of **packaged** cache files |
| `cache.vllm.image/format` | Cache format(s) present (`triton`, `binary`, `aot_compile`, etc.) |

#### `cache-size-bytes` semantics

At **create** time, MCV computes `cache-size-bytes` from the **staging
directory** copied into the image layer (the same bytes that are packaged),
not from unrelated files elsewhere on the build host. After `--source` trees
are copied into that staging directory, the label includes their bytes as well,
so extract size validation stays consistent with the full payload.

At **extract** time, MCV validates that the number of cache bytes written
from the layer tarball matches the label. Pre-existing files in the target
cache directory (for example `~/.triton/cache` or `~/.cache/vllm`) are not
included in validation.

### Directory Structure Labels

When MCV creates an OCI Image, it applies a set of labels describing what
the source directory structure of the cache looked like. This provides hints
to applications like the KServe Kernel Manager, so they can extract the cache
in their workload pods in the same directory format they were generated from.
This is just a hint and applications can extract anywhere they choose.

**Scope:** GKM **MCV** authors these labels and unpacks cache layers; it does not
include an in-repo operator or agent that actuates them on a cluster. End-to-end
in-cluster warm-up requires a consumer (for example **KServe Kernel Manager**) that
derives a `CachePlan` from the labels, mounts each payload subtree at the recorded
paths, and sets the listed env vars. Until that exists, capture + push alone does
not mount Triton at `TRITON_CACHE_DIR` in serving pods.

| Label | Description |
|-------|-------------|
| `io.kserve.km/cache-hash` | Hash or comma-separated list of hashes identifying cached kernels (e.g., `d4ec7c2a7d` or `abc123,def456`); **omitted** when capture metadata has no hash directory |
| `io.kserve.km/cache-mount-subpath` | Relative path from the extracted payload root to the primary cache subtree (e.g., `torch_compile_cache` or `torch_compile_cache/torch_aot_compile`); required for vLLM/Habana serving plans |
| `io.kserve.km/cache-root-env` | Environment variable and value for the framework's cache root directory, set to the directory the cache was captured from (e.g., `VLLM_CACHE_ROOT=/home/kserve/.cache/vllm`, or `VLLM_CACHE_ROOT=/tmp/vllm` when captured from an image that sets `VLLM_CACHE_ROOT=/tmp/vllm`). Override with `--mount-at`. |
| `io.kserve.km/cache-mounts` | Optional JSON array of extra payload subtrees captured with `--source` (see [Extra cache trees](#extra-cache-trees)) |
| `io.kserve.km/cache-type` | Type of cache packaged (e.g., `torch-compile`, `habana-recipe`) |
| `io.kserve.km/framework` | ML framework that generated the cache (`vllm` for torch-compile caches, `habana` for recipe caches) |

### Extra cache trees

vLLM keeps some kernel caches outside `VLLM_CACHE_ROOT`. Most notably, current
builds select the standalone Inductor adaptor, which never redirects the Triton
JIT cache, so it stays wherever `TRITON_CACHE_DIR` points (frequently
`/tmp/triton`). `mcv --create --source <dir>` packs such a tree into the same
layer under `io.vllm.cache/<basename>/` and records where it has to reappear:

```json
[{"subPath":"triton","absPath":"/tmp/triton","env":"TRITON_CACHE_DIR","requiresWritable":true}]
```

| Field | Meaning |
|-------|---------|
| `subPath` | Directory inside the payload prefix (`io.vllm.cache`) |
| `absPath` | Absolute path the tree must be available at in the serving container |
| `env` | Variable pointing the framework at `absPath`; absent when it cannot be derived safely (e.g. `FLASHINFER_WORKSPACE_BASE` names the workspace base, not `.cache/flashinfer`) |
| `requiresWritable` | The runtime appends to this tree, so it must not be mounted read-only |

MCV **create** omits top-level `modelinfos/` and `dummy_cache/` under
`VLLM_CACHE_ROOT` (runtime metadata, not compile cache). **Extract** skips those
paths if present in older images.

`requiresWritable` is not an optimization: a read-only Triton tree does not fall
back to compiling, it fails with `PermissionError` the first time Triton stores a
miss.

Capture aborts rather than producing a half-described image when a source lies
inside the cache root (it is already captured), when the encoded label would
exceed the **4096-byte OCI per-label limit** (`MaxLabelBytes` on the JSON
`cache-mounts` array), or when `--mount-at` is not absolute. Long absolute paths
on several trees can hit that limit in production; a future release may split
metadata across labels or move mount hints into manifest-side fields if needed.

`--source` classification uses directory name and content heuristics
(`looksLikeTritonCache`, basename rules, and similar). Misclassification is
possible; MCV logs a warning when no known env variable can be inferred for a
tree so operators can set serving env themselves.

Consumers that ignore this label still get the primary mount and keep working;
they simply restore less of the cache. Because Triton group files embed absolute
kernel paths, extra trees must be mounted at exactly `absPath` — mounting them
somewhere else silently misses every kernel. Capture and serve from the same
container image and environment to keep those paths valid.

`mcv --extract` writes the primary vLLM payload under
`--dir/<basename(VLLM_CACHE_ROOT)>/` (for `/tmp/vllm` that is `vllm/…`) and
extra trees under `--dir/<subPath>/` (for example `triton/`), then logs each
tree's intended serving path and writability,
since it cannot move files to arbitrary absolute paths on a host unasked.
Use **`--place-extra-trees`** on extract to opt in to relocating extra subtrees
to the recorded absolute paths (this writes **outside** `--dir`; MCV logs a
warning when the flag is set). Programmatic callers get the same information typed via
`client.InspectCachePlan`, and can ask `client.ExtractCache` to place the trees
for them with `Options.PlaceExtraTrees`.

**CachePlan env vars:** `Derive` sets `CachePlan.Env` from `cache-root-env` only
(for example `VLLM_CACHE_ROOT=/tmp/vllm`). Each extra mount in `CachePlan.Mounts`
carries its own `env` and `absPath` (for example `TRITON_CACHE_DIR` at
`/tmp/triton`). KServe and other consumers must apply **both** the root env and
every mount entry when building the pod spec.

## MCV extract algorithm

```
1. Read cache type from config labels (triton | vllm)
2. For each layer:
     if layer media type is compat (docker gzip OR oci gzip):
         extract cache + manifest from tarball
     else:
         fail compat path
3. If compat path failed, optionally try legacy artifact layers
   (application/cache.<type>.content.layer.v1+<type>) — not produced by MCV create
4. Validate cache-size-bytes against bytes written by whichever extraction path succeeded (step 2 or step 3)
```

MCV **create** (Docker and Buildah) only produces compat images. The artifact
layer path exists for older or external images only.

## Examples

### OCI manifest with Docker-format layer (Quay)

Typical after Docker build + push:

```bash
$ skopeo inspect docker://quay.io/example/vector-add-cache:latest
{
    "Digest": "sha256:…",
    "Architecture": "amd64",
    "LayersData": [
        {
            "MIMEType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
            "Size": 19287
        }
    ],
    "Labels": {
        "cache.triton.image/cache-size-bytes": "80415",
        "cache.triton.image/entry-count": "1",
        "cache.triton.image/summary": "{\"targets\":[{\"backend\":\"hip\",\"arch\":\"gfx90a\",\"warp_size\":64}]}"
    }
}
```

The manifest descriptor may still report `application/vnd.oci.image.manifest.v1+json`.

### Buildah layer type

Buildah/Podman builds often report:

```json
"MIMEType": "application/vnd.oci.image.layer.v1.tar+gzip"
```

Extract treats this identically to the Docker layer type above.

## Building compat images

### With MCV (recommended)

```bash
# Docker
mcv -c -i quay.io/example/my-cache:latest -d /path/to/cache

# Buildah / Podman
mcv -c -i quay.io/example/my-cache:latest -d /path/to/cache --buildah
```

MCV stages cache + manifest, squashes to a single layer, and sets labels from
the staged content.

The Docker builder loads a Docker Schema 2 image directly into the daemon
(consistent manifest and layer media types). Buildah uses `commit --squash`.
Manual `docker build` with the Dockerfile below can still produce an OCI manifest
with Docker layer types; use `mcv -c` or `--builder buildah` to avoid that.

### Manual Docker build

A single-stage `FROM scratch` Dockerfile with multiple `COPY` instructions
produces **one layer per COPY** in the final image. MCV instead uses a
**multi-stage** build so the published image has **exactly one** rootfs layer
(cosign-friendly, matches `extractCompatImg` expectations).

#### 1. Prepare the build context

Stage cache and manifest under a build root (same layout MCV uses):

```bash
BUILD_ROOT=/tmp/cache-image-build
mkdir -p "${BUILD_ROOT}/io.triton.cache"
mkdir -p "${BUILD_ROOT}/io.triton.manifest"

cp -a /path/to/.triton/cache/. "${BUILD_ROOT}/io.triton.cache/"
# Write or copy manifest.json into io.triton.manifest/
```

For vLLM, use `io.vllm.cache/` and `io.vllm.manifest/` instead.

#### 2. Dockerfile (single published layer)

This matches `DockerfileTemplate` in MCV:

```dockerfile
FROM scratch AS build
COPY "./io.triton.cache/" "./io.triton.cache/"
COPY "./io.triton.manifest/manifest.json" "./io.triton.manifest/manifest.json"

FROM scratch
LABEL org.opencontainers.image.title=my-cache
COPY --from=build / /
```

- **`build` stage**: collects cache + manifest (intermediate layers are discarded).
- **Final stage**: `COPY --from=build / /` emits **one** gzip tarball layer
  containing both paths.
- Place `org.opencontainers.image.title` on the **final** stage (after the
  second `FROM scratch`), not the build stage.

#### 3. Build and label

Pass cache metadata labels at build time (MCV sets these via `ImageBuildOptions.Labels`):

```bash
cd "${BUILD_ROOT}"

docker build \
  -t quay.io/example/my-cache:latest \
  --label cache.triton.image/entry-count=1 \
  --label cache.triton.image/cache-size-bytes=80415 \
  --label 'cache.triton.image/summary={"targets":[{"backend":"hip","arch":"gfx90a","warp_size":64}]}' \
  -f Dockerfile \
  .
```

Compute `cache-size-bytes` from the staged `io.triton.cache/` (or
`io.vllm.cache/`) tree—the same bytes that end up in the layer.

#### 4. Verify a single layer

```bash
docker inspect quay.io/example/my-cache:latest | jq '.[0].RootFS.Layers | length'
# Expected: 1

docker push quay.io/example/my-cache:latest
```

If `RootFS.Layers | length` is greater than 1, the image was not squashed
into a single published layer (for example a single-stage Dockerfile with
multiple `COPY` lines).

### Manual Buildah build

```bash
buildah from scratch
buildah copy <container> ./io.triton.cache /io.triton.cache
buildah copy <container> ./io.triton.manifest/manifest.json /io.triton.manifest/manifest.json
# set labels …
buildah commit --squash <container> docker://quay.io/example/my-cache:latest
```

## Legacy artifact layers (not MCV create output)

Some images may use a custom layer media type:

`application/cache.triton.content.layer.v1+triton`
`application/cache.vllm.content.layer.v1+vllm`

MCV extract attempts this path only if compat extraction fails. MCV create does
**not** produce these images. New images should use compat layers only.

## Appendix: example cache paths

| Cache type | Staging dir (in layer) | Default extract dir |
|------------|------------------------|---------------------|
| Triton | `io.triton.cache/` | `~/.triton/cache/` |
| vLLM | `io.vllm.cache/` | `~/.cache/vllm/` |
