# Model Cache Vault (MCV)

<img src="logo/mcv.png" alt="mcv" width="20%" height="auto">

A Model/GPU kernel cache container packaging utility inspired by
[WASM](https://github.com/solo-io/wasm/blob/master/spec/README.md).

## Features

- Build container images containing GPU Kernel/Model caches.
- Extract a cache from an OCI image
- Compatible with docker or buildah
- **Single-layer image output** (squashed) for cosign compatibility
- Client API for retrieving and extracting images
- Artifact and image signing via cosign (indirectly)

### Kernel Cache artifact and image signing

- Cache artifact signing with Cosign
- Container image signing support with Cosign
- **Single-layer images**: MCV produces one squashed compat layer with Docker Schema 2 media types (Docker builder) or OCI layer types (Buildah), compatible with cosign signing, `docker save`, and kind image load

## Build Instructions

### Requirements

- Go 1.25.0 or later

### Install dependencies

```bash
sudo dnf install -y gpgme-devel btrfs-progs-devel
```
OR
```bash
sudo apt install -y libgpgme-dev libbtrfs-dev uidmap
```

On Ubuntu 24.04, *running* `mcv` unprivileged to build cache images (its
embedded buildah creates a user namespace) requires unprivileged user
namespaces, which Ubuntu restricts by default via AppArmor. This is only needed
at runtime — compiling with `make build` does not need it. There are two ways to
allow it.

**Preferred — scoped AppArmor profile.** Grant the `userns` permission only to
the `mcv` binary, leaving the global restriction in place for every other
program. Create `/etc/apparmor.d/mcv` (adjust the path to match your installed
binary):

```bash
abi <abi/4.0>,
include <tunables/global>

profile mcv /home/<user>/go/bin/mcv {
  userns,
  include if exists <local/mcv>
}
```

Then load it:

```bash
sudo apparmor_parser -r /etc/apparmor.d/mcv
```

When running MCV inside a container instead of natively, apply the same `userns`
permission to the container runtime's profile (e.g. `podman`/`rootlesskit`)
rather than to `mcv`.

**Simpler but less secure — disable the restriction globally.** This re-enables
unprivileged user namespaces for *all* programs, weakening a defense-in-depth
protection against kernel exploits that abuse user namespaces. Prefer the scoped
profile above; use this only on disposable/dev machines:

```bash
sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0

# To persist across reboots:
echo 'kernel.apparmor_restrict_unprivileged_userns=0' | sudo tee /etc/sysctl.d/99-userns.conf
 ```

### Build and Install

Build the binary:

```bash
make build
```

After the binary is built, it can be found in an arch specific directory,
something like `./_output/bin/linux_amd64/mcv`. To install the binary in
the local `~/go/bin` directory, run (make sure `~/go/bin` is in $PATH):

```bash
make install
```

## Usage

Below is the `mcv` usage:

```bash
$ mcv -h
A Model cache container image management utility

Usage:
  mcv [flags]

Flags:
  -b, --baremetal          Run baremetal preflight checks
  -c, --create             Create OCI image
  -d, --dir string         A Cache Directory
  -e, --extract            Extract a cache from an OCI image
  -h, --help               help for mcv
  -i, --image string       OCI image name
  -l, --log-level string   Set the logging verbosity level:
                           debug, info, warning or error
      --mount-at string    Override the cache root mount path for serving
      --no-gpu             Allow kernel extraction without GPU
                           present (for testing purposes)
      --source stringArray Extra cache directory to capture (repeatable)
```

> NOTE: The create option is a work in progress.
> For now to create an OCI image containing a GPU Kernel cache directory
> please follow the instructions in [spec-compat.md](./docs/spec-compat.md).

### No-GPU Mode

MCV supports creating and extracting cache images **without GPU hardware** using the `--no-gpu` flag. This is useful for CI/CD pipelines, development environments, and containerized workflows where GPU access isn't available.

**Quick Start:**

```bash
# Create cache image without GPU
mcv --create --image quay.io/myorg/cache:v1 --dir /path/to/cache --no-gpu

# Extract cache without GPU validation
mcv --extract --image quay.io/myorg/cache:v1 --dir /path/to/cache --no-gpu
```

### Capturing caches outside VLLM_CACHE_ROOT

Modern vLLM builds select the standalone Inductor adaptor, which leaves the Triton
JIT cache wherever `TRITON_CACHE_DIR` points (often `/tmp/triton`) instead of under
`VLLM_CACHE_ROOT`. Capturing only the vLLM root therefore leaves most of a cold
start uncached, and the Triton cache is not path-relocatable because its group
files record absolute kernel paths.

Capture those trees with `--source`; each one is stored in the same image layer and
recorded with the path it must reappear at. On **create**, MCV copies everything
under `--dir` except vLLM runtime-only top-level dirs (`modelinfos/`,
`dummy_cache/`) — only compile artifacts such as `torch_compile_cache/` are
packaged.

```bash
mcv --create --image quay.io/myorg/cache:v1 \
  --dir /tmp/vllm --source /tmp/triton --no-gpu
```

The resulting image carries `VLLM_CACHE_ROOT=/tmp/vllm` in
`io.kserve.km/cache-root-env` (the directory it was captured from, not a fixed
default) plus a JSON `io.kserve.km/cache-mounts` label listing the extra trees:

```json
[{"subPath":"triton","absPath":"/tmp/triton","env":"TRITON_CACHE_DIR","requiresWritable":true}]
```

`requiresWritable` matters: a read-only Triton tree does not silently fall back to
compiling, it fails with `PermissionError` when Triton stores a miss. `mcv extract`
writes every tree under `--dir` and logs where each one is expected to live
(see `logMountTargets` in the extract path).

**In-cluster serving is not implemented in this repository.** MCV **create**
stamps `io.kserve.km/*` labels (including `cache-mounts`) and **extract** unpacks
the payload; optional **`PlaceExtraTrees`** can relocate extra subtrees on a
host when you opt in. There is no **`operator/`** or agent code here that reads
those labels and configures a InferenceService pod. Until **KServe Kernel Manager**
(or an equivalent consumer) mounts each `CachePlan.Mounts` entry at the recorded
`absPath`, sets the per-tree environment variables (`TRITON_CACHE_DIR`, …), and
honours **`requiresWritable`**, shipping a cache image alone does **not** warm
Triton (or other out-of-root trees) in-cluster. Treat KM / operator integration as
a **dependency or follow-up PR** when setting merge expectations for split
vLLM + Triton capture.

### End-to-end: capture vLLM + Triton on a GPU node (Podman)

This walkthrough matches a layout where **`VLLM_CACHE_ROOT=/tmp/vllm`** and
**`TRITON_CACHE_DIR=/tmp/triton`** (common on several vLLM/RHAII images). Host
staging uses two sibling directories—the same shape as the checked-in example
**[`example/vllm-capture-root/`](example/vllm-capture-root/)** (`vllm/` + `triton/`).
Use that tree to reproduce **`mcv --create`** without a live GPU server once the
directories are present in your checkout.

Set **`CACHE_IMAGE`** to a strict OCI reference (`quay.io/user/name:tag` or
`docker.io/library/name:tag`). Names like `localhost/...` are rejected by MCV.

Buildah inside **`quay.io/gkm/mcv:unified`** writes to **container-local**
storage. Chain **`buildah push`** to a **docker-archive** on a bind mount (or to
a registry) in the **same** `podman run` before the container exits, then
**`podman load`** on the host. **`mcv --extract` inside the unified image pulls
from a registry** (it does not see the host Podman store unless you push or use
Quay with credentials). For a quick serve test you can skip extract and bind
**`$CAPTURE_ROOT`** directly.

```bash
export CAPTURE_ROOT=$HOME/vllm-capture
mkdir -p "$CAPTURE_ROOT"/{vllm,triton}

export VLLM_IMAGE=docker.io/vllm/vllm-openai:latest   # or your image
export MODEL=RedHatAI/Llama-3.2-1B-Instruct-FP8       # or your model
export MCV_IMAGE=quay.io/gkm/mcv:unified              # or: make -C mcv image-unified
export CACHE_IMAGE=quay.io/YOURUSER/vllm-cache:test   # adjust registry/user
export CACHE_TAR=$CAPTURE_ROOT/cache-image.tar
```

#### 1. Run vLLM and warm the caches

Use the same environment variables the serving container will use:

```bash
sudo podman run -d --name vllm-server \
  --device nvidia.com/gpu=all \
  --ipc=host \
  -p 8000:8000 \
  -e VLLM_CACHE_ROOT=/tmp/vllm \
  -e TRITON_CACHE_DIR=/tmp/triton \
  -v ~/.cache/huggingface:/root/.cache/huggingface \
  "$VLLM_IMAGE" \
  --model "$MODEL" --max-model-len 2048

sudo podman logs -f vllm-server
# Optional readiness check:
curl -s http://127.0.0.1:8000/v1/models | jq .

sudo podman exec vllm-server ls -la /tmp/vllm/torch_compile_cache
sudo podman exec vllm-server ls -la /tmp/triton | head
sudo podman exec vllm-server printenv VLLM_CACHE_ROOT TRITON_CACHE_DIR
```

#### 2. Copy caches to the host

```bash
sudo podman cp vllm-server:/tmp/vllm/. "$CAPTURE_ROOT/vllm/"
sudo podman cp vllm-server:/tmp/triton/. "$CAPTURE_ROOT/triton/"
sudo chown -R "$(whoami):$(whoami)" "$CAPTURE_ROOT"

du -sh "$CAPTURE_ROOT"/vllm "$CAPTURE_ROOT"/triton
test -d "$CAPTURE_ROOT/vllm/torch_compile_cache" && echo "vLLM tree OK"
```

#### 3. Build the MCV cache image and export it to the host

Mount the host copies at the **same paths** used during capture, then
`--dir` + `--source`. Export with **`buildah push`** to a tar on
**`$CAPTURE_ROOT`**:

```bash
sudo podman run --rm \
  -v "$CAPTURE_ROOT/vllm:/tmp/vllm:ro,Z" \
  -v "$CAPTURE_ROOT/triton:/tmp/triton:ro,Z" \
  -v "$CAPTURE_ROOT:/out:Z" \
  --entrypoint sh \
  "$MCV_IMAGE" \
  -c '/mcv --create --image '"$CACHE_IMAGE"' \
        --dir /tmp/vllm --source /tmp/triton --no-gpu --builder buildah && \
      buildah push '"$CACHE_IMAGE"' docker-archive:/out/cache-image.tar'

sudo podman load -i "$CACHE_TAR"
sudo podman images | grep vllm-cache
```

Alternative: push to a registry instead of a tar (then extract can pull it):

```bash
# Inside the same sh -c after --create: buildah login quay.io && buildah push '"$CACHE_IMAGE"'
sudo podman push "$CACHE_IMAGE"
```

Inspect KServe / vLLM labels on the **host** image:

```bash
sudo podman inspect "$CACHE_IMAGE" | \
  jq '.[0].OCIv1.config.Labels // .[0].Config.Labels | with_entries(select(.key | startswith("io.kserve.km") or startswith("cache.vllm")))'
```

Expect `io.kserve.km/cache-root-env` = `VLLM_CACHE_ROOT=/tmp/vllm` and
`io.kserve.km/cache-mounts` listing `/tmp/triton` with `requiresWritable: true`.

#### 4. Extract (optional)

Requires the image on a registry the MCV container can pull (**`podman push`**
after **`podman login`**, or a public repo). Skipping extract is fine when testing
serve from **`$CAPTURE_ROOT`**.

```bash
sudo mkdir -p /tmp/vllm-extracted
sudo podman run --rm -v /tmp/vllm-extracted:/out:Z "$MCV_IMAGE" \
  --extract --image "$CACHE_IMAGE" --dir /out --no-gpu
ls /tmp/vllm-extracted/torch_compile_cache /tmp/vllm-extracted/triton
```

**Layout:** `--dir` is the **cache root** (the same role as `VLLM_CACHE_ROOT` at
capture time). Extract unpacks `torch_compile_cache/` (and extra payload subtrees
like `triton/`) **directly under `--dir`**, not under an extra `vllm/` directory.
For a local serve test, set `VLLM_CACHE_ROOT` to your extract path, or bind-mount
the extract dir at the path recorded in `io.kserve.km/cache-root-env`
(for example `/tmp/vllm`). `modelinfos/` and `dummy_cache/` are not packaged or
restored.

To move extra trees (for example Triton) to the paths recorded in the image
labels on the **host**, add **`--place-extra-trees`** (writes outside `--dir`):

```bash
sudo podman run --rm -v /tmp/vllm-extracted:/out:Z --entrypoint /mcv "$MCV_IMAGE" \
  --extract --image "$CACHE_IMAGE" --dir /out --no-gpu --place-extra-trees
```

#### 5. Serve test from host caches (cache hit)

Stop the original server if it still holds the GPU. Mount Triton **read-write**
(`:Z`, not `:ro`) so misses can append:

```bash
sudo podman stop vllm-server

sudo podman run --rm --device nvidia.com/gpu=all --ipc=host \
  -e VLLM_CACHE_ROOT=/tmp/vllm \
  -e TRITON_CACHE_DIR=/tmp/triton \
  -v "$CAPTURE_ROOT/vllm:/tmp/vllm:Z" \
  -v "$CAPTURE_ROOT/triton:/tmp/triton:Z" \
  "$VLLM_IMAGE" \
  --model "$MODEL" --max-model-len 2048 2>&1 | tee /tmp/restart.log

grep -E 'Directly load|Compiling a graph' /tmp/restart.log
```

`Directly load AOT compilation` (or similar) indicates a compile-cache **hit**;
`Compiling a graph for compile range` suggests a **miss** (paths, model config, or
GPU target mismatch).

See also [unified-mcv-container.md](./docs/unified-mcv-container.md) and
[vllm-binary-cache.md](./docs/vllm-binary-cache.md) for registry push and
Quay workflows.

**Container Images:**

Two image variants are available:

1. **Unified** (~533MB) - NVIDIA + AMD GPU support, auto-detects GPU vendor at runtime
   ```bash
   make build-image-mcv
   # or directly:
   podman build --target mcv-unified -t quay.io/gkm/mcv:unified -f mcv/images/Containerfile .
   ```

2. **No-GPU** (~176MB) - For `--no-gpu` workflows, arm64/mac; no CUDA/ROCm libraries
   ```bash
   make build-image-mcv-no-gpu
   # or directly:
   podman build --target mcv-minimal -t quay.io/gkm/mcv:no-gpu -f mcv/images/Containerfile .
   ```

**How it works:** With `--no-gpu`, MCV extracts GPU information (backend, architecture, warp size) from cache metadata rather than detecting actual hardware. The cache files created by vLLM/Triton already contain all necessary GPU information in environment variables.

**GPU access flags** (e.g., `--gpus all` for NVIDIA, `--device /dev/kfd --device /dev/dri` for AMD) are **ONLY** required for GPU validation/preflight checks. They are **NOT** needed when using `--no-gpu` for cache creation or extraction.
When using `podman run --device`, `--group-add keep-groups` may be needed for device access. But `--group-add keep-groups` requires crun (not runc).

For detailed usage examples, container configuration, GPU access requirements, and CI/CD integration, see [docs/no-gpu-usage.md](./docs/no-gpu-usage.md).

## Dependencies

- [buildah dependencies](https://github.com/containers/buildah/blob/main/install.md#building-from-scratch)

## GPU Kernel Image Container Specification

### Cache Image Container Specification

The Cache Image specification defines how MCV packages and extracts Triton/vLLM
caches as container images. **Compat** images use standard gzip tarball layers;
extract routing is based on **layer media type**, not manifest type. Details:
[spec-compat.md](./docs/spec-compat.md)

### vLLM Binary Cache Support

MCV supports both legacy (triton cache) and new (binary cache) vLLM formats:

1. **vLLM Triton Cache Format** (legacy) - Stores `triton_cache/` and
   `inductor_cache/` inside rank directories
2. **vLLM Binary Cache Format** (new) - Stores prefix directories
   (e.g., `backbone/`) inside rank directories

For detailed information about vLLM binary cache support, see:
[vllm-binary-cache.md](./docs/vllm-binary-cache.md)

### Triton Cache Example

To extract the Triton Cache for the
[01-vector-add.py](https://github.com/triton-lang/triton/blob/main/python/tutorials/01-vector-add.py)
tutorial from [Triton](https://github.com/triton-lang/triton), run the
following:

```bash
mcv -e -i quay.io/gkm/vector-add-cache:rocm
Img fetched successfully!!!!!!!!
Img Digest: sha256:b6d7703261642df0bf95175a64a01548eb4baf265c5755c30ede0fea03cd5d97
Img Size: 525
bash-4.4#
```

This will extract the cache directory from the
`quay.io/gkm/vector-add-cache:rocm` container image and copy it to
`~/.triton/cache/`.

To Create an OCI image for a Triton Cache using docker run the
following:

```bash
mcv -c -i quay.io/gkm/vector-add-cache:rocm -d example/vector-add-cache-rocm
INFO[2026-07-27 21:37:05] Setting log level: info
INFO[2026-07-27 21:37:05] Using docker to build the image
INFO[2026-07-27 21:37:05] Detected cache components: [triton]
INFO[2026-07-27 21:37:05] Dockerfile generated successfully at /tmp/.mcv/docker/Dockerfile
{"stream":"Step 1/9 : FROM scratch AS build"}
{"stream":"\n"}
{"stream":" ---\u003e \n"}
{"stream":"Step 2/9 : COPY \"./io.triton.cache/\" \"./io.triton.cache/\""}
{"stream":"\n"}
{"stream":" ---\u003e aa1fa6bcd3db\n"}
{"stream":"Step 3/9 : COPY \"./io.triton.manifest/manifest.json\" \"./io.triton.manifest/manifest.json\""}
{"stream":"\n"}
{"stream":" ---\u003e 57d1c4815d2e\n"}
{"aux":{"ID":"sha256:57d1c4815d2ede3d2ed3b64a9e5f062577ca8dd501c8b59c5d90b3351bd06b47"}}
{"stream":"Step 4/9 : FROM scratch"}
{"stream":"\n"}
{"stream":" ---\u003e \n"}
{"stream":"Step 5/9 : LABEL org.opencontainers.image.title=vector-add-cache"}
{"stream":"\n"}
{"stream":" ---\u003e Running in 6e7dce4e97bc\n"}
{"stream":" ---\u003e e8b4014c2ae2\n"}
{"stream":"Step 6/9 : COPY --from=build / /"}
{"stream":"\n"}
{"stream":" ---\u003e ddcb3cce60a7\n"}
{"stream":"Step 7/9 : LABEL cache.triton.image/cache-size-bytes=80415"}
{"stream":"\n"}
{"stream":" ---\u003e Running in 1e196c7ace14\n"}
{"stream":" ---\u003e 56aa910decff\n"}
{"stream":"Step 8/9 : LABEL cache.triton.image/entry-count=1"}
{"stream":"\n"}
{"stream":" ---\u003e Running in af4bb8ba633c\n"}
{"stream":" ---\u003e ce2af41bccb7\n"}
{"stream":"Step 9/9 : LABEL cache.triton.image/summary={\"targets\":[{\"backend\":\"hip\",\"arch\":\"gfx90a\",\"warp_size\":64}]}"}
{"stream":"\n"}
{"stream":" ---\u003e Running in d97c0447e121\n"}
{"stream":" ---\u003e 170e5776a1f5\n"}
{"aux":{"ID":"sha256:170e5776a1f56a8e8e3a8a4398aaf814e196f72001d9a63aabcc3055ecd238ae"}}
{"stream":"Successfully built 170e5776a1f5\n"}
{"stream":"Successfully tagged quay.io/gkm/vector-add-cache:rocm\n"}
INFO[2026-07-27 21:37:06] Docker image built successfully
INFO[2026-07-27 21:37:06] OCI image created successfully.
```

To see the new image:

```bash
docker images

IMAGE                               ID             DISK USAGE   CONTENT SIZE   EXTRA
quay.io/gkm/vector-add-cache:rocm   170e5776a1f5        136kB         21.2kB
```

To verify the image has a single layer (important for cosign compatibility):

```bash
docker inspect quay.io/gkm/vector-add-cache:rocm | jq '.[0].RootFS.Layers | length'
1
```

To inspect the docker image with Skopeo (note the **single layer** due to squashing):

> **Note**: Use `docker-daemon:` prefix for Docker images, not `containers-storage:` (which is for Buildah/Podman).

```bash
skopeo inspect docker-daemon:quay.io/gkm/vector-add-cache:rocm
{
    "Name": "quay.io/gkm/vector-add-cache",
    "Digest": "sha256:97bd4cb83b692bebed5adc4cd92647478052719e4c7771562af31d1aef198cb8",
    "RepoTags": [],
    "Created": "2026-07-27T21:37:05.971275986-04:00",
    "DockerVersion": "",
    "Labels": {
        "cache.triton.image/cache-size-bytes": "80415",
        "cache.triton.image/entry-count": "1",
        "cache.triton.image/summary": "{\"targets\":[{\"backend\":\"hip\",\"arch\":\"gfx90a\",\"warp_size\":64}]}",
        "org.opencontainers.image.title": "vector-add-cache"
    },
    "Architecture": "amd64",
    "Os": "linux",
    "Layers": [
        "sha256:4d49b8253e60536d82418c622032c65ab3f31235e92b6d12cb29a9131c2aef04"
    ],
    "LayersData": [
        {
            "MIMEType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
            "Digest": "sha256:4d49b8253e60536d82418c622032c65ab3f31235e92b6d12cb29a9131c2aef04",
            "Size": 93184,
            "Annotations": null
        }
    ],
    "Env": [
        "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
    ]
}
```

> **Note**: If `buildah` is installed it will be favoured to build the image.
The build output is shown below.

```bash
mcv -c -i quay.io/gkm/vector-add-cache:rocm -d example/vector-add-cache-rocm
INFO[2025-05-28 12:23:04] baremetalFlag false
INFO[2025-05-28 12:23:04] Using buildah to build the image
INFO[2025-05-28 12:23:04] Wrote manifest to /tmp/buildah-manifest-dir-2780945232/manifest.json
INFO[2025-05-28 12:23:04] Image built! baadff55392c0ada6f0d358c255d63ca770fb20b87429a732480e00bbf8d044b
INFO[2025-05-28 12:23:04] Temporary directories successfully deleted.
INFO[2025-05-28 12:23:04] OCI image created successfully.
```

To inspect the buildah image with Skopeo

```bash
skopeo inspect containers-storage:quay.io/gkm/vector-add-cache:rocm
{
    "Name": "quay.io/gkm/vector-add-cache",
    "Digest": "sha256:3f8c7b3aeeffd9ee3f673486f3bc681a7f9ed39e21242628e6845755191d6bd4",
    "RepoTags": [],
    "Created": "2025-05-28T15:45:17.379786001Z",
    "DockerVersion": "",
    "Labels": {
        "cache.triton.image/cache-size-bytes": "80415",
        "cache.triton.image/entry-count": "1",
        "cache.triton.image/summary": "{\"targets\":[{\"backend\":\"hip\",\"arch\":\"gfx90a\",\"warp_size\":64}]}"
    },
    "Architecture": "amd64",
    "Os": "linux",
    "Layers": [
        "sha256:ef89050f71ecc3dc925f14c12d2fd406c067f78987eed36a1176b19499c8ea20"
    ],
    "LayersData": [
        {
            "MIMEType": "application/vnd.oci.image.layer.v1.tar",
            "Digest": "sha256:ef89050f71ecc3dc925f14c12d2fd406c067f78987eed36a1176b19499c8ea20",
            "Size": 93184,
            "Annotations": null
        }
    ],
    "Env": null
}
```

To inspect the image labels specifically run:

```bash
skopeo inspect containers-storage:quay.io/gkm/vector-add-cache:rocm | jq -r '.Labels["cache.triton.image/summary"]' | jq .
{
  "targets": [
    {
      "backend": "hip",
      "arch": "gfx90a",
      "warp_size": 64
    }
  ]
}
```

### vLLM Cache example

To Create an OCI image for a vLLM Cache run the following:

```bash
mcv -c -i quay.io/gkm/cache-examples:vllm-example -d example/vllm-cache
INFO[2025-09-03 09:04:15] Hardware accelerator(s) detected (2). GPU support enabled.
INFO[2025-09-03 09:04:15] Using buildah to build the image
INFO[2025-09-03 09:04:23] Detected cache components: [vllm]
INFO[2025-09-03 09:04:24] Image built! 8218fac0225882a7de7a1f11f32aff25df2936f1f12b08c0c26ab30897d19c5a
INFO[2025-09-03 09:04:24] OCI image created successfully.
```

To inspect the image labels specifically run:

```bash
skopeo inspect containers-storage:quay.io/gkm/cache-examples:vllm-example
{
    "Name": "quay.io/gkm/cache-examples",
    "Digest": "sha256:9e731d58adccd608cb18dcefe259acd30ffe976d5e98208a4158ce22c0b5d1e2",
    "RepoTags": [],
    "Created": "2026-02-10T12:04:38.260317569Z",
    "DockerVersion": "",
    "Labels": {
        "cache.vllm.image/cache-size-bytes": "2269180",
        "cache.vllm.image/entry-count": "1",
        "cache.vllm.image/summary": "{\"targets\":[{\"backend\":\"hip\",\"arch\":\"gfx90a\",\"warp_size\":64}]}"
    },
    "Architecture": "amd64",
    "Os": "linux",
    "Layers": [
        "sha256:440b5cbd3b76dc17a6012e17fc56341d4894b88ab7a85b12c5e2f6f7c4b80661"
    ],
    "LayersData": [
        {
            "MIMEType": "application/vnd.oci.image.layer.v1.tar+gzip",
            "Digest": "sha256:440b5cbd3b76dc17a6012e17fc56341d4894b88ab7a85b12c5e2f6f7c4b80661",
            "Size": 250291,
            "Annotations": null
        }
    ],
    "Env": null
}
```

To extract the vLLM Cache run the following:

```bash
mcv -e -i quay.io/gkm/cache-examples:vllm-example
INFO[2025-09-03 09:06:00] Hardware accelerator(s) detected (2). GPU support enabled.
INFO[2025-09-03 09:06:02] Preflight GPU compatibility check passed.
INFO[2025-09-03 09:06:02] Preflight completed                           matched="[0 1]" unmatched="[]"
INFO[2025-09-03 09:06:04] Extracting cache to directory: /home/fedora/.cache/vllm
```

## Signing Container Images

Use [Sigstore Cosign](https://docs.sigstore.dev/) to sign mcv-built images.

1. Install Cosign

```bash
go install github.com/sigstore/cosign/v2/cmd/cosign@latest
```

2. Sign an image

```bash
cosign sign -y quay.io/gkm/vector-add-cache@sha256:<digest>
⏎
Generating ephemeral keys...
Retrieving signed certificate...

    The sigstore service, hosted by sigstore a Series of LF Projects,
    LLC, is provided pursuant to the Hosted Project Tools Terms of
    Use, available at
    https://lfprojects.org/policies/hosted-project-tools-terms-of-use/.
    Note that if your submission includes personal data associated with
    this signed artifact, it will be part of an immutable record.
    This may include the email address associated with the account with
    which you authenticate your contractual Agreement.
    This information will be used for signing this artifact and will be
    stored in public transparency logs and cannot be removed later, and
    is subject to the Immutable Record notice at
    https://lfprojects.org/policies/hosted-project-tools-immutable-records/.

By typing 'y', you attest that (1) you are not submitting the personal
data of any other person; and (2) you understand and agree to the
statement and the Agreement terms at the URLs listed above.
Your browser will now be opened to:
...
```

Cosign will prompt you to authenticate and display legal terms regarding
transparency logs.

3. Confirm and Finish
    - Ephemeral keys will be generated
    - Signature will be pushed to the registry
    - You'll see a success message including the transparency log index

Upon successful completion, you will see an output similar to:

```bash
Successfully verified SCT...
tlog entry created with index: 215011903
Pushing signature to: quay.io/gkm/cache-examples
```

## MCV Client API

### Capturing a Cache, Including the Triton Directory

`BuildCache` packs a cache root plus any number of extra trees into one image
layer. Current vLLM builds keep the Triton JIT cache at `TRITON_CACHE_DIR` rather
than under `VLLM_CACHE_ROOT`, so capturing only the root leaves that tree behind:

```go
package main

import (
	"log"

	"github.com/redhat-et/GKM/mcv/pkg/client"
)

func main() {
	err := client.BuildCache(client.BuildOptions{
		ImageName: "quay.io/myorg/llama3-cache:v1",
		CacheDir:  "/tmp/vllm",   // VLLM_CACHE_ROOT: the dir containing torch_compile_cache
		Sources:   []string{"/tmp/triton"},
		Builder:   "",            // "" auto-detects buildah or docker
	})
	if err != nil {
		log.Fatalf("capture failed: %v", err)
	}
}
```

Each source is recorded with the path it was given, and the image's
`io.kserve.km/cache-root-env` carries `CacheDir` (resolved to an absolute path,
or `MountAt` when the serving container uses a different layout). Capture fails
if a source sits inside `CacheDir`, if the recorded paths do not fit the 4 KiB
label, or if `MountAt` is not absolute.

### Reading the Mount Plan

`InspectCachePlan` returns every directory an image expects to be restored to,
primary cache root first, so callers that build pod specs can mount each tree
themselves:

```go
plan, err := client.InspectCachePlan("quay.io/myorg/llama3-cache:v1")
if err != nil {
	if client.IsUnsupportedCacheType(err) {
		// Bare Triton images carry no mounting metadata; nothing to restore.
		log.Print("image has no serving plan")
		return
	}
	log.Fatalf("inspect failed: %v", err)
}

for _, m := range plan.Mounts {
	// m.SubPath is inside the payload prefix, m.AbsPath is where it belongs in
	// the container, m.Env points the framework at it, and m.RequiresWritable
	// means a read-only mount would fail rather than fall back to compiling.
	log.Printf("mount %s at %s (env %s, writable=%t)", m.SubPath, m.AbsPath, m.Env, m.RequiresWritable)
}
```

### Extracting a Cache from a Container Image

An example snippet of how to use the client API to extract a Cache from a
container image is shown below.

```go
package main

import (
	"log"

	"github.com/redhat-et/GKM/mcv/pkg/client"
)

func main() {
	_, _, err := client.ExtractCache(client.Options{
		ImageName:       "quay.io/gkm/cache-examples:vector-add-cache-cuda",
		CacheDir:        "/tmp/testcache",
		LogLevel:        "debug",
		EnableBaremetal: nil, // or false if explicitly desired

		// Extra trees land in CacheDir/<subpath> by default. Set this to move
		// them to the absolute paths recorded in the image instead.
		PlaceExtraTrees: false,
	})
	if err != nil {
		log.Fatal(err)
	}
}
```

### Detecting System GPU Devices

You can also use the MCV client API to retrieve details about the system's
available GPUs:

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"

    "github.com/redhat-et/GKM/mcv/pkg/client"
)

func main() {
    stub := false
    gpus, err := client.GetSystemGPUInfo(client.HwOptions{EnableStub: &stub})
    if err != nil && gpus == nil {
        log.Fatalf("Error retrieving GPU info: %v", err)
    }

    output, err := json.MarshalIndent(gpus, "", "  ")
    if err != nil {
        log.Fatalf("Failed to format GPU info: %v", err)
    }

    fmt.Println("Detected GPU Devices:")
    fmt.Println(string(output))
}
```

### Checking Image Compatibility with Host GPUs

```go
package main

import (
    "fmt"
    "log"

    "github.com/redhat-et/GKM/mcv/pkg/client"
)

func main() {
    matched, unmatched, err := client.PreflightCheck(
        "quay.io/gkm/cache-examples:vector-add-cache-cuda")
    if err != nil {
        log.Fatalf("Preflight check failed: %v", err)
    }

    fmt.Printf("Compatible GPUs: %d\n", len(matched))
    for i, gpu := range matched {
        fmt.Printf("  MATCH %d: Backend=%s, Arch=%s, WarpSize=%d, "+
            "PTX=%s\n", i, gpu.Backend, gpu.Arch, gpu.WarpSize,
            gpu.PTXVersion)
    }

    fmt.Printf("Incompatible GPUs: %d\n", len(unmatched))
    for i, gpu := range unmatched {
        fmt.Printf("  NO-MATCH %d: Backend=%s, Arch=%s, WarpSize=%d, "+
            "PTX=%s\n", i, gpu.Backend, gpu.Arch, gpu.WarpSize,
            gpu.PTXVersion)
    }
}
```

### Static Device Configuration (Stub Mode)

MCV supports running in environments without GPUs by using a static device
configuration. This is useful for testing or CI environments.

#### Stub Mode Usage

Run MCV with the `--stub` flag. It will use the static config and behave as
if those devices are present.

## Using MCV image to build cache images

MCV provides container images at `quay.io/gkm/mcv`. The default (`quay.io/gkm/mcv:latest`)
is the no-gpu variant (~176MB), which can be used to wrap a vLLM/Triton cache in an OCI
container image that can then be pushed to a container registry (without having to install
mcv locally). For GPU validation, use `quay.io/gkm/mcv:unified` (auto-detects NVIDIA or AMD).

These images can also be used as part of a
[github workflow](./.github/workflows/mcv-build-example-images.yml).

### MCV container image with docker

To use docker on the host with an MCV image, you need to mount the cache
directory to the container and run the following command:

```bash
# Buildah storage is at /home/appuser/.local/share/containers (owned by UID 1000).
# Mount a writable host directory there so --user 1000:1000 can write regardless
# of the host user's UID. Using --user $(id -u):$(id -g) fails whenever the host
# UID is not 1000, because those pre-created paths are owned by UID 1000 in the image.
sudo install -d -o 1000 -g 1000 -m 700 /tmp/mcv-storage

docker run --rm -it \
  --user 1000:1000 \
  --security-opt seccomp=unconfined \
  --security-opt apparmor=unconfined \
  -v <path-to-cache>/example:/example:Z \
  -v /tmp/mcv-storage:/home/appuser/.local/share/containers:Z \
  quay.io/gkm/mcv bash -lc '
    /mcv -c -i quay.io/gkm/vector-add-cache:rocm \
        -d /example/vector-add-cache-rocm --no-gpu &&
    buildah push containers-storage:quay.io/gkm/vector-add-cache:rocm \
        docker-archive:/example/vector-add-cache-rocm.tar:quay.io/gkm/vector-add-cache:rocm
  '
WARN[2025-09-11 16:46:54] running newgidmap: exit status 1: newgidmap: write to gid_map failed: Operation not permitted
WARN[2025-09-11 16:46:54] /usr/bin/newgidmap should be setgid or have filecaps setgid
WARN[2025-09-11 16:46:54] Falling back to single mapping
WARN[2025-09-11 16:46:54] Error running newuidmap: exit status 1: newuidmap: write to uid_map failed: Operation not permitted
WARN[2025-09-11 16:46:54] Falling back to single mapping
INFO[2025-09-11 16:46:54] Setting log level: info
INFO[2025-09-11 16:46:54] Using buildah to build the image
INFO[2025-09-11 16:46:54] Detected cache components: [triton]
INFO[2025-09-11 16:46:55] Image built! 8ce4bc2e98abfa8c0a5a6f6046c1c7bc8ac09805ecb029427a995dc2897828f8
INFO[2025-09-11 16:46:55] OCI image created successfully.
WARN[0000] running newgidmap: exit status 1: newgidmap: write to gid_map failed: Operation not permitted
WARN[0000] /usr/bin/newgidmap should be setgid or have filecaps setgid
WARN[0000] Falling back to single mapping
WARN[0000] Error running newuidmap: exit status 1: newuidmap: write to uid_map failed: Operation not permitted
WARN[0000] Falling back to single mapping
Getting image source signatures
Copying blob 24b82d6fef87 done
Copying config 8ce4bc2e98 done
Writing manifest to image destination
Storing signatures
```

> **NOTE:** The Warnings are known and everything still works fine.
> An include library is making a system call that it doesn't have permission for,
> so it fails and falls back to another method that succeeds.
>
> **Security note — `seccomp=unconfined` / `apparmor=unconfined`.** MCV runs
> buildah *inside* the container to assemble the OCI image, which needs
> `mount`/`unshare`/`pivot_root` and user-namespace operations that Docker's
> default seccomp and AppArmor profiles block for non-privileged containers.
> Disabling both is a deliberate, security-reviewed fallback — it is far narrower
> than `--privileged` (no added capabilities, host devices, or host namespaces),
> and the blast radius is bounded: the container runs as a non-root user
> (`--user`), performs a single packaging task, and is removed on exit (`--rm`).
> To harden further, replace these flags with scoped seccomp and AppArmor
> profiles that allow only buildah's required syscalls/operations, or run the
> same command under **rootless Podman**, which needs neither flag.

Then on host:

```bash
docker load -i <path-to-cache>/example/vector-add-cache-rocm.tar
24b82d6fef87: Loading layer  93.18kB/93.18kB
The image quay.io/gkm/vector-add-cache:rocm already exists, renaming
the old one with ID sha256:5dc90b88f536e44e186c5a076afbb7a54389aed6f0ddfa21365ae2c7f79cb21d to empty string
Loaded image: quay.io/gkm/vector-add-cache:rocm
```

Check the images:

```bash
docker images
REPOSITORY                               TAG       IMAGE ID       CREATED          SIZE
quay.io/gkm/vector-add-cache             rocm      8ce4bc2e98ab   15 seconds ago   80.7kB
```

### MCV container image with podman

To use podman on the host with an MCV image, you need to mount the cache
directory to the container and run the following command:

```bash
podman run --rm -it \
  -v <path-to-cache>/example:/example:Z,U \
  quay.io/gkm/mcv bash -lc '
    /mcv -c -i quay.io/gkm/vector-add-cache:rocm \
        -d /example/vector-add-cache-rocm --no-gpu &&
    buildah push containers-storage:quay.io/gkm/vector-add-cache:rocm \
        oci-archive:/example/vector-add-cache-rocm.oci:quay.io/gkm/vector-add-cache:rocm
  '
```

```bash
podman load -i <path-to-cache>/example/vector-add-cache-rocm.oci
```

```bash
podman images
REPOSITORY                                TAG                         IMAGE ID      CREATED         SIZE
quay.io/gkm/vector-add-cache              rocm                        b1bc2ae6bef1  25 seconds ago  94.7 kB
```
