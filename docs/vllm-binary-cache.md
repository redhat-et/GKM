# vLLM Binary Cache Support

## Overview

MCV supports two vLLM cache formats:
1. **Triton Cache Format** (legacy/unpacked) - Original format with `triton_cache/` directory
2. **Binary Cache Format** (new) - New format with `rank_X_Y/` directory structure

This document describes the **Binary Cache Format** introduced in recent versions of vLLM.

## Binary Cache Format

### Directory Structure

The binary cache uses a structured directory layout:

```
torch_compile_cache/
└── {hash}/                           # 10-character cache hash
    └── rank_{rank}_{dp_rank}/        # Per-rank cache
        └── {prefix}/                 # Model component
            ├── cache_key_factors.json
            ├── vllm_compile_cache.py
            ├── computation_graph.py
            └── artifact_compile_range_{start}_{end}_subgraph_{i}  # Binary artifacts
```

### Key Components

#### 1. Cache Hash Directory

The hash directory is a 10-character truncated SHA256 hash derived from:
- Code hash (SHA256 of forward code files)
- Configuration hash (hash of vLLM config)
- Compiler hash (Inductor compiler state)
- Environment hash (compilation-affecting env vars)

#### 2. Rank Directory

Format: `rank_{rank}_{dp_rank}`
- `{rank}`: Distributed training rank
- `{dp_rank}`: Data parallel rank
- Allows multiple ranks to maintain separate caches

#### 3. Prefix Directory

Common prefixes:
- `backbone`: Main model component (default)
- `eagle_head`: Speculative decoding draft model

#### 4. Cache Files

**cache_key_factors.json**: Metadata tracking cache key components
```json
{
  "code_hash": "<sha256-hash>",
  "compiler_hash": "<compiler-hash>",
  "config_hash": "<config-hash>",
  "env": {
    "VLLM_TARGET_DEVICE": "cuda",
    "VLLM_COMPILE_CACHE_SAVE_FORMAT": "binary",
    "VLLM_MAIN_CUDA_VERSION": "12.9",
    ...
  }
}
```

**vllm_compile_cache.py**: Python dict mapping compile ranges to artifact handles

**computation_graph.py**: Readable FX graph source code (for debugging)

**artifact_compile_range_* files**: Compiled artifacts
- **Binary format** (default): Single binary file per artifact
- **Unpacked format**: Directory containing Inductor output files

## Storage Formats

vLLM supports two storage formats for artifacts, controlled by `VLLM_COMPILE_CACHE_SAVE_FORMAT`:

### Binary Format (default)

- **Env Var**: `VLLM_COMPILE_CACHE_SAVE_FORMAT=binary`
- **Artifacts**: Regular files
- **Multiprocess Safe**: Yes
- **Inspection**: Cannot easily inspect contents
- **Use Case**: Production deployments

```
{prefix}/
├── artifact_compile_range_{start}_{end}_subgraph_0  (file, ~2.7 MB)
└── artifact_compile_range_{start}_{end}_subgraph_1  (file, ~2.1 MB)
```

### Unpacked Format

- **Env Var**: `VLLM_COMPILE_CACHE_SAVE_FORMAT=unpacked`
- **Artifacts**: Directories with Python/Triton files
- **Multiprocess Safe**: No (race conditions possible)
- **Inspection**: Can view and debug generated code
- **Use Case**: Development and debugging

```
{prefix}/
├── artifact_compile_range_{start}_{end}_subgraph_0/  (directory)
│   ├── kernel_0.py
│   └── kernel_1.py
└── artifact_compile_range_{start}_{end}_subgraph_1/  (directory)
```

## MCV Metadata

### Container Image Labels

When a binary cache is packaged in a container image, MCV adds the following labels:

```json
{
  "cache.vllm.image/entry-count": "1",
  "cache.vllm.image/cache-size-bytes": "35702329",
  "cache.vllm.image/format": "binary",
  "cache.vllm.image/summary": "{\"targets\":[{\"backend\":\"cuda\",\"arch\":\"sm_12.9\",\"warp_size\":32}]}"
}
```

**Label Descriptions:**
- `entry-count`: Number of cache hash directories detected
- `cache-size-bytes`: Total size of the cache in bytes
- `format`: Storage format (`"binary"` or `"unpacked"`)
- `summary`: Hardware target information (JSON)

### Manifest Structure

The `manifest.json` file contains comprehensive metadata:

```json
{
  "vllm": [
    {
      "vllmHash": "{hash}",
      "cacheFormat": "binary",
      "binary": [
        {
          "rank": "rank_{rank}_{dp_rank}",
          "prefix": "{prefix}",
          "artifact_count": 17,
          "artifact_names": [
            "artifact_compile_range_{start}_{end}_subgraph_0",
            "artifact_compile_range_{start}_{end}_subgraph_1",
            ...
          ],
          "code_hash": "<sha256-hash>",
          "config_hash": "<config-hash>",
          "compiler_hash": "<compiler-hash>",
          "cache_save_format": "binary",
          "target_device": "cuda",
          "env": {
            "VLLM_TARGET_DEVICE": "cuda",
            "VLLM_COMPILE_CACHE_SAVE_FORMAT": "binary",
            "VLLM_MAIN_CUDA_VERSION": "12.9",
            ...
          }
        }
      ]
    }
  ]
}
```

**Manifest Fields:**
- `cacheFormat`: Cache structure type (`"binary"` for new format, `"triton"` for legacy/unpacked caches)
- `binary[]`: Array of binary cache entries (one per rank/prefix combination)
- `cache_save_format`: Actual artifact storage format (`"binary"` or `"unpacked"`)
- `target_device`: Target hardware (`"cuda"`, `"rocm"`, `"tpu"`, `"cpu"`)
- `env`: Full environment variables from `cache_key_factors.json`

## Hardware Detection

MCV automatically extracts hardware information from the cache metadata:

### CUDA
```json
{
  "backend": "cuda",
  "arch": "sm_12.9",
  "warp_size": 32
}
```
- **Backend**: Extracted from `VLLM_TARGET_DEVICE`
- **Arch**: Derived from `VLLM_MAIN_CUDA_VERSION`
- **Warp Size**: 32 (CUDA default)

### ROCm/HIP
```json
{
  "backend": "rocm",
  "arch": "gfx90a",
  "warp_size": 64
}
```
- **Backend**: Extracted from `VLLM_TARGET_DEVICE`
- **Arch**: Detected from ROCm environment variables
- **Warp Size**: 64 (AMD wavefront size)

## Format Detection

MCV automatically detects the cache format by inspecting the filesystem:

1. **Binary Format Detection**:
   - Looks for `rank_X_Y/` directories
   - Checks for `cache_key_factors.json`
   - Inspects `artifact_compile_range_*` entries
   - If entries are **files** → Binary format
   - If entries are **directories** → Unpacked format

2. **Triton Format Detection** (fallback):
   - Looks for `triton_cache/` directory
   - Uses legacy/unpacked cache extraction logic

This filesystem-based detection is more reliable than environment variables, especially when caches are copied between systems.

### Format Indicators

MCV uses **three distinct format indicators** to describe vLLM caches. Each serves a different purpose:

#### 1. Manifest `cacheFormat` (Cache Structure Type)

**Location**: `manifest.json` → `vllm[].cacheFormat`
**Values**: `"binary"` or `"triton"`
**Purpose**: Tells MCV extraction logic which directory structure to expect

- `"binary"`: New format with `rank_{rank}_{dp_rank}/{prefix}/` structure
- `"triton"`: Legacy format with `triton_cache/` directory

**Example**:
```json
{
  "vllm": [{
    "cacheFormat": "binary",  // ← Extraction logic uses this
    "binary": [...]
  }]
}
```

This field determines which extraction code path MCV uses and is essential for correctly unpacking the cache from the container image.

#### 2. Manifest `cache_save_format` (Artifact Storage Format)

**Location**: `manifest.json` → `vllm[].binary[].cache_save_format`
**Values**: `"binary"` or `"unpacked"`
**Purpose**: Records the actual artifact storage format detected from the filesystem

- `"binary"`: Artifacts are individual files (multiprocess-safe, production use)
- `"unpacked"`: Artifacts are directories containing Python/Triton source files (debugging use)

**Example**:
```json
{
  "vllm": [{
    "cacheFormat": "binary",
    "binary": [{
      "rank": "rank_0_0",
      "prefix": "backbone",
      "cache_save_format": "binary",  // ← Detected from filesystem
      "artifact_count": 17,
      ...
    }]
  }]
}
```

This field is informational and helps users understand the internal artifact format.

#### 3. Image Label `format` (User-Visible Format)

**Location**: OCI image labels → `cache.vllm.image/format`
**Values**: `"binary"` or `"unpacked"`
**Purpose**: Quick user-visible indicator of artifact storage format

- `"binary"`: For binary cache format with binary artifacts
- `"unpacked"`: For triton cache format OR binary cache format with unpacked artifacts

**Example**:
```json
{
  "cache.vllm.image/format": "binary",  // ← Quick indicator for users
  "cache.vllm.image/entry-count": "1",
  "cache.vllm.image/cache-size-bytes": "35702329"
}
```

This label allows users to quickly inspect cache format using `docker inspect` or `skopeo inspect` without reading the full manifest.

### Format Mapping Table

| Cache Type | Artifact Type | Manifest `cacheFormat` | Manifest `cache_save_format` | Image Label `format` |
|------------|---------------|------------------------|------------------------------|----------------------|
| New binary cache with binary artifacts | Files | `"binary"` | `"binary"` | `"binary"` |
| New binary cache with unpacked artifacts | Directories | `"binary"` | `"unpacked"` | `"unpacked"` |
| Legacy triton cache | Directories | `"triton"` | N/A (not present) | `"unpacked"` |

**Why Three Indicators?**

- **Manifest `cacheFormat`**: Extraction logic must know the directory structure (`rank_X_Y/` vs `triton_cache/`)
- **Manifest `cache_save_format`**: Detailed metadata for debugging and compatibility checking
- **Image Label `format`**: Fast user-facing indicator without parsing full manifest

## Comparison: Binary vs Triton Cache

| Aspect | Triton Cache (Legacy) | Binary Cache (New) |
|--------|----------------------|-------------------|
| **Structure** | `triton_cache/` + `inductor_cache/` | `rank_X_Y/{prefix}/` |
| **Metadata** | Triton kernel JSON files | `cache_key_factors.json` |
| **Storage** | Always unpacked | Binary or unpacked |
| **Multiprocess** | Not guaranteed | Safe in binary mode |
| **Distributed** | Limited support | Full rank/DP support |
| **Manifest Key** | `"triton"` | `"binary"` |
| **Image Label** | `"unpacked"` | `"binary"` or `"unpacked"` |

## Usage Examples

### Building a Cache Image

```bash
# Build from binary cache directory
mcv -c -d /path/to/model-binary-cache \
    -i quay.io/myorg/model-cache:v1 \
    --builder docker

# Result includes labels and manifest
```

### Extracting a Cache Image

```bash
# Extract cache from image
mcv -e -i quay.io/myorg/model-cache:v1

# MCV automatically detects format from manifest
# and extracts to appropriate location
```

### Inspecting Cache Metadata

```bash
# View image labels
skopeo inspect docker://quay.io/myorg/model-cache:v1 | jq '.Labels'

# Expected output:
# {
#   "cache.vllm.image/format": "binary",
#   "cache.vllm.image/summary": "{\"targets\":[{\"backend\":\"cuda\",\"arch\":\"sm_12.9\",\"warp_size\":32}]}",
#   ...
# }
```

## vLLM Source References

Key files in vLLM that implement binary cache:

- `vllm/envs.py:1512-1520` - `VLLM_COMPILE_CACHE_SAVE_FORMAT` definition
- `vllm/compilation/compiler_interface.py:186-327` - `InductorStandaloneAdaptor`
- `vllm/compilation/backends.py:245-346` - Compilation manager
- `vllm/compilation/backends.py:904-935` - `cache_key_factors.json` creation
- `vllm/compilation/backends.py:867-874` - Directory structure creation

## Best Practices

1. **Use binary format in production** for multiprocess safety
2. **Use unpacked format for debugging** to inspect generated code
3. **Include full env in manifest** for cache compatibility checking
4. **Verify hardware match** using image labels before deployment
5. **Check cache_save_format** in manifest when extracting caches

## Migration from Triton Cache

To migrate from triton cache to binary cache:

1. Update vLLM to a version that supports binary cache
2. Set `VLLM_COMPILE_CACHE_SAVE_FORMAT=binary`
3. Run model warmup to generate new binary cache
4. Package new cache with MCV (automatically detected)
5. Both formats are supported, no breaking changes

## See Also

- [spec-compat.md](./spec-compat.md) - OCI image specification
- [design.md](./design.md) - MCV architecture and design
- [vLLM Documentation](https://github.com/vllm-project/vllm) - vLLM project
