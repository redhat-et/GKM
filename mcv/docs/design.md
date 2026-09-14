
# Model Cache Vault (MCV) - Design Document

## Overview

**MCV** (Model Cache Vault) is a utility for packaging, validating,
and extracting prebuilt Model caches using OCI container
images. It supports GPU hardware preflight checks, cache manifest
validation, and developer-friendly debugging workflows.

## Goals

- Package prebuilt Triton or vLLM caches into OCI-compatible container images.
- Validate kernel or model caches against the host GPU for compatibility.
- Support multiple container image formats, including Docker and OCI standard.
- Extract and validate cache contents via:
  - Kernel metadata summary (for fast checks)
  - Full manifest analysis (for detailed checks)

## Supported Image Formats

MCV **create** produces **compat** cache images: cache content in a standard
gzip tarball layer. MCV **extract** selects the code path from **layer media
type**, not manifest type (OCI vs Docker Schema 2).

| Layer media type | Typical builder | Extract support |
|------------------|-----------------|-----------------|
| `application/vnd.docker.image.rootfs.diff.tar.gzip` | Docker / MCV `-c` | Yes (compat) |
| `application/vnd.oci.image.layer.v1.tar+gzip` | Buildah / Podman / MCV `-c --buildah` | Yes (compat) |
| `application/cache.<type>.content.layer.v1+<type>` | External / legacy | Yes (fallback only) |

See [spec-compat.md](./spec-compat.md) for the full compat specification.

## Key Features

- **Triton Support**: Supports Triton cache packaging and extraction.
- **vLLM Support**: Supports vLLM model cache packaging and extraction.
- **Cache Summary Validation**: Checks image metadata labels before
  extraction.
- **Full Manifest Validation**: Parses and validates a detailed manifest
  after extraction.
- **GPU Compatibility Checks**: Verifies compatibility of kernel cache
  against local GPUs using backend, architecture, warp size, and optionally
  PTX versions.
- **Clean Extraction**: Extracted kernels are cleaned up if they are not
  compatible.
- **Temporary Directories**: All staging operations occur in `/tmp/.mcv/*`
  to prevent host pollution.
- **Multi-Tree Capture**: `--source` packs cache trees that live outside the
  primary cache root (Triton, Inductor, DeepGEMM) into the same layer and
  records the path each one restores to.

## Image Label Schema

### Generic Labels (Always Present)

<!-- markdownlint-disable  MD013 -->
<!-- Teporarily disable MD013 - Line length to keep the table formatting  -->
| Label                                   | Description                                      |
|-----------------------------------------|--------------------------------------------------|
| `model.cache.image/type`                | Primary cache type (e.g. `triton`, `vllm`)       |
| `model.cache.image/components`          | JSON array of included components                |
| `model.cache.image/entry-count`         | Total number of entries across all components    |
| `model.cache.image/cache-size-bytes`    | Combined cache directory size in bytes           |
| `model.cache.image/summary`             | Summary of supported targets (e.g. arch/backend) |

### Component-Specific Labels (Conditional)

| Label                                     | Description                           |
|-------------------------------------------|---------------------------------------|
| `cache.triton.image/entry-count`          | Number of Triton kernel cache entries |
| `cache.triton.image/summary`              | Summary of Triton-compatible targets  |
| `cache.vllm.image/entry-count`            | Number of vLLM model cache files      |
| `cache.vllm.image/summary`                | Summary of vLLM metadata              |

> **Note**: These labels are only included if the corresponding cache type is detected.

### Mounting Labels (KServe Kernel Manager)

| Label                             | Description                                                     |
|-----------------------------------|-----------------------------------------------------------------|
| `io.kserve.km/framework`          | Framework that produced the cache (`vllm`, `habana`, …)         |
| `io.kserve.km/cache-type`         | Cache type (`torch-compile`, `habana-recipe`)                   |
| `io.kserve.km/cache-hash`         | Cached kernel hash(es); omitted when no hash directory was detected |
| `io.kserve.km/cache-mount-subpath`| Payload subpath backing the primary mount (always set with vLLM/Habana metadata) |
| `io.kserve.km/cache-root-env`     | Cache root env var captured from, e.g. `VLLM_CACHE_ROOT=/tmp/vllm` |
| `io.kserve.km/cache-mounts`       | JSON extra trees from `--source`: subpath, absolute path, env, writable |

The mount path in `cache-root-env` is the directory the cache was captured from,
so out-of-band captures (for example an image that runs vLLM with
`VLLM_CACHE_ROOT=/tmp/vllm`) restore to the path the framework actually reads.
Consumers mount each payload subtree with a **single volume** and one
`volumeMount` per `CachePlan.Mounts` entry: `subPath` selects the directory
inside the extracted payload, `mountPath` is the label's `absPath` (primary
mount: `VLLM_CACHE_ROOT`, extras: e.g. `/tmp/triton`), and `readOnly` must be
false when `requiresWritable` is true. `pkg/cacheplan` decodes these labels into
a typed `CachePlan` whose `Mounts` field lists every directory to populate,
primary first.

See [spec-compat.md](./spec-compat.md) for the full contract.

<!-- markdownlint-enable MD013 -->

## Workflow Summary

### Creating an Image

```bash
mcv -c -i quay.io/example/triton-kernel -d /path/to/.triton/cache
```

- Copies kernel cache into build context, plus any `--source` trees under the
  same payload prefix
- Writes manifest.json with entry metadata
- Builds a single-layer compat image using the Docker or Buildah builder
- Labels image with summary + entry count

### Extracting and Validating

```bash
mcv -e -i quay.io/example/triton-kernel
```

- Downloads image
- Compares summary label to local GPU
- Extracts image if compatible
- Validates manifest
- Removes incompatible kernels if manifest fails

## Debugging & Logging

- Logging level configurable via `--log-level`
- Temporary cache lives under `/tmp/.mcv/`
- Kernel cache extracted under `~/.triton/cache/`

## Sequence Diagrams

### Image Creation

![Create](./images/create-puml.png)

### Image Extraction

![Extract](./images/extract-puml.png)
