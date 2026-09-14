# vLLM + Triton host capture layout

Fixed example of a **split capture** from a warmed vLLM container on CUDA
(`sm_75`, e.g. AWS g4dn / T4): torch-compile under **`vllm/`** and Triton JIT
under **`triton/`**, matching `VLLM_CACHE_ROOT=/tmp/vllm` and
`TRITON_CACHE_DIR=/tmp/triton` at capture time.

```text
vllm-capture-root/
├── README.md
├── vllm/          # VLLM_CACHE_ROOT (contains torch_compile_cache/…)
└── triton/        # TRITON_CACHE_DIR
```

## Build a cache image from this tree (Podman)

From the **GKM repo root**, bind-mount the example at the **same absolute paths**
recorded in the image labels (`/tmp/vllm`, `/tmp/triton`):

```bash
export REPO_ROOT=$(git rev-parse --show-toplevel)
export EXAMPLE=$REPO_ROOT/mcv/example/vllm-capture-root
export MCV_IMAGE=quay.io/gkm/mcv:unified
export CACHE_IMAGE=quay.io/YOURUSER/vllm-cache:example-sm75
export CACHE_TAR=$EXAMPLE/cache-image.tar

sudo mkdir -p /tmp/vllm /tmp/triton
sudo mount --bind "$EXAMPLE/vllm" /tmp/vllm
sudo mount --bind "$EXAMPLE/triton" /tmp/triton

sudo podman run --rm \
  -v /tmp/vllm:/tmp/vllm:ro,Z \
  -v /tmp/triton:/tmp/triton:ro,Z \
  -v "$EXAMPLE:/out:Z" \
  --entrypoint sh \
  "$MCV_IMAGE" \
  -c '/mcv --create --image '"$CACHE_IMAGE"' \
        --dir /tmp/vllm --source /tmp/triton --no-gpu --builder buildah && \
      buildah push '"$CACHE_IMAGE"' docker-archive:/out/cache-image.tar'

sudo umount /tmp/vllm /tmp/triton

sudo podman load -i "$CACHE_TAR"
sudo podman inspect "$CACHE_IMAGE" | \
  jq '.[0].OCIv1.config.Labels // .[0].Config.Labels | with_entries(select(.key | startswith("io.kserve.km") or startswith("cache.vllm")))'
```

Expect `io.kserve.km/cache-root-env` = `VLLM_CACHE_ROOT=/tmp/vllm` and
`cache-mounts` with `"absPath":"/tmp/triton"`.

## Serve test without the OCI image

Bind the example directories directly into a vLLM container at `/tmp/vllm` and
`/tmp/triton` (Triton mount should be read-write). See
[End-to-end capture](../../README.md#end-to-end-capture-vllm--triton-on-a-gpu-node-podman)
in the MCV README.

## Provenance

Populated from an EC2 g4dn capture (vLLM OpenAI image, Llama 3.2 1B FP8 class
model, mega-AOT / binary torch-compile layout). Do not expect a cache hit on
other GPU architectures without recapturing.
