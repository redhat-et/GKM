# Design: GPU Kernel Manager (GKM) Integration with KServe

<!-- markdownlint-disable MD033 MD046 MD013 MD034 MD031 MD026 MD060 MD032 -->
<!-- markdownlint-disable MD029 MD033 MD046 MD013 MD024 MD022 MD036 -->

## <span style="color:red; font-size:2em;">Status: Draft</span>

## Motivation / Abstract

### Personas

Platform engineers, ML engineers, and cluster operators managing
GPU-accelerated LLM inference workloads.

### Problem Statement

When serving Large Language Models (LLMs), frameworks like vLLM, PyTorch and
Triton translate GPU kernels implemented through higher-level programming
languages into CUDA PTX or ROCm HASCO assembly before being executed by the GPU
driver (for example, via torch.compile). This just-in-time (JIT) compilation
occurs each time a model is loaded and can significantly delay model startup
by 30-120 seconds. KServe's existing Local Model Cache accelerates model
weight downloads but does not cache the GPU kernel binaries generated after
model load, leaving a significant startup performance gap for GPU workloads.

### Feature/Capability

This proposal extends KServe's Local Model Cache to manage GPU kernel caches
alongside model artifacts using a unified control plane architecture. By
integrating GPU Kernel Manager (GKM) functionality directly into KServe's
existing CRDs and controllers, we enable users to pre-distribute trusted,
architecture-specific kernel caches across nodes, reducing model startup
times by 30-70% while ensuring cache integrity using OCI image signing
(cosign and Kyverno) and GPU compatibility validation. In future iterations,
this integration will provide automatic cache warming that precompiles and
captures kernel caches when new models run, further accelerating
model readiness and improving the overall KServe model startup experience
across heterogeneous GPU clusters.

---

## Background

KServe's `Local Model Cache` feature accelerates startup by pre-downloading and
caching model artifacts (weights, tokenizers, configuration files) onto
node-local storage using PersistentVolumes. When an InferenceService references
a cached model, KServe mounts the PVC directly, eliminating download time and
network bandwidth usage. This significantly improves startup performance for
model loading.

However, the `Local Model Cache` does not address the GPU kernel compilation
overhead that occurs after model weights are loaded. For LLM inference
workloads using vLLM, Triton, or PyTorch with torch.compile, the first model
load triggers JIT compilation of GPU kernels optimized for the
specific GPU architecture. This compilation can add significant delay to startup
time, during which the pod is not ready to serve requests. This overhead
occurs on every pod (re)start, even when model weights are already cached.

GPU Kernel Manager (GKM) is a Kubernetes-native project that manages
precompiled GPU kernel caches distributed as signed OCI images with GPU
compatibility metadata. Rather than deploying GKM as a separate control plane
with its own CRDs, agents, and CSI drivers, this proposal integrates GKM's
core capabilities: Kernel extraction, GPU compatibility validation
(MCV library), and trusted image verification (cosign + kyverno) into KServe's
existing Local Model Cache architecture. This provides a unified experience
where model weights and GPU kernel caches are managed through the same CRDs,
stored on the same PV/PVCs, and tracked by the same controllers.

### Goals

1. **Reduce model startup latency by 30-70%** by providing precompiled GPU
  kernel caches ready for immediate use
2. **Unify model and kernel cache management** under a single LocalModelCache
  CRD and control plane
3. **Ensure cache integrity and security** through OCI image signing (cosign)
  and signature verification (kyverno).
4. **Support heterogeneous GPU configurations** via automatic GPU detection and
  compatibility validation
5. **Reuse existing PV/PVC infrastructure** to store kernel caches alongside
  model weights on the same volumes
6. **Minimize deployment complexity** by eliminating the need for separate GKM
  operators, agents, or CSI drivers
7. **Enable future cache warming workflows** to automatically generate and
  distribute kernel caches when new models are cached

### Non-Goals

1. **Managing or distributing model weights or artifacts** - Already handled by
  existing Local Model Cache (no changes needed to model weight handling)
2. **Directly handling GPU scheduling or resource allocation** - Handled by
  Kubernetes device plugins and schedulers
3. **Modifying inference framework code** - Integration is through standard
  environment variables and mount paths that frameworks already support

---

## Proposal Design / Approach

["How" the feature is going to work, is designed, implemented, etc. This should
be written for an average contributor in the WG area.]

### Design

[Design details for the feature at the resource model level. Details such as;
How the feature should work, sequence diagrams, schema-level changes, failure
modes. For user facing features, this section shouldn't contain code.]

#### Architecture Overview

This proposal extends KServe's existing Local Model Cache architecture with GPU
kernel cache management capabilities by adding an optional `kernelCache` field
to the LocalModelCache CRD. Model weights and GPU kernel caches are stored on
the same PersistentVolume in separate subdirectories (`/mnt/models/models/`
and `/mnt/models/kernel-caches/`), managed by the same controllers, and mounted
via the same PVC.

```text
┌────────────────────────────────────────────────────────────────┐
│                    KServe Control Plane                        │
│                                                                │
│  LocalModelCache (Extended)                                    │
│  ┌──────────────────────────────────────────────────────┐      │
│  │  spec:                                               │      │
│  │    sourceModelUri: s3://models/llama-7b              │      │
│  │    modelSize: 13Gi                                   │      │
│  │    nodeGroups: [gpu-node-group]                      │      │
│  │                                                      │      │
│  │    kernelCache:                    # NEW FIELD       │      │
│  │      image: quay.io/.../kernels:v1                   │      │
│  └──────────────────────────────────────────────────────┘      │
│                                                                │
│  LocalModel Controller:                                        │
│  - Calculate total storage (modelSize + cacheSize)       # MOD │
│  - Create PV/PVC (existing, unchanged)                         │
│  - Propagate kernel cache info to LocalModelNode specs   # NEW │
│  - Aggregate kernel cache status                         # NEW │
│  - Verify kernel cache signatures                        # NEW │
└────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌────────────────────────────────────────────────────────────────┐
│                    Worker Nodes (DaemonSet)                    │
│                                                                │
│  LocalModelNode Agent (Enhanced):                              │
│  - Detect GPU hardware (nvidia-smi/rocm-smi)             # NEW │
│  - Download model weights (existing, unchanged)                │
│  - Download kernel cache OCI images                      # NEW │
│  - Validate GPU compatibility with MCV                   # NEW │
│  - Extract caches to /mnt/models/kernel-caches/          # NEW │
│  - Update status with GPU info and cache status          # NEW │
│                                                                │
│  PersistentVolume (Node-Local):                                │
│  /mnt/models/                                                  │
│  ├── models/                                   # Existing      │
│  │   └── llama-7b/                             # Model weights │
│  └── kernel-caches/                            # NEW           │
│      └── llama-7b/                             # GPU kernels   │
└────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌────────────────────────────────────────────────────────────────┐
│                    InferenceService Pod                        │
│                                                                │
│  Predictor Container (vLLM/Triton/PyTorch):                    │
│    - Model PVC mounted at /mnt/models                          │
│    - Reads weights from /mnt/models/models/llama-7b            │
│    - Reads kernels from /mnt/models/kernel-caches/llama-7b     │
│    - Env: VLLM_KERNEL_CACHE=/mnt/models/kernel-caches/llama-7b │
└────────────────────────────────────────────────────────────────┘
```

#### How the Feature Works

**1. User Creates LocalModelCache with Kernel Cache:**

Users extend their existing LocalModelCache resources with an optional
`kernelCache` field:

```yaml
apiVersion: serving.kserve.io/v1alpha1
kind: LocalModelCache
metadata:
  name: llama-7b
spec:
  # Existing fields (no changes)
  sourceModelUri: s3://models/llama-7b
  modelSize: 13Gi
  nodeGroups:
    - gpu-node-group

  # NEW: Optional kernel cache configuration
  kernelCache:
    image: quay.io/myorg/llama-7b-vllm-kernels:v1
```

**1.a Kyverno verifies image signature (when enabled)**

To ensure the integrity and authenticity of kernel cache images, we leverage
[Kyverno](https://kyverno.io/), a Kubernetes-native policy engine designed
for declarative security and governance. Kyverno integrates with
[Sigstore's Cosign](https://docs.sigstore.dev/cosign/overview/) to verify
container image signatures and attestations stored in OCI registries,
providing cryptographic assurance that kernel cache images have not been
tampered with and originate from trusted sources. By defining `verifyImages`
rules in Kyverno ClusterPolicies, we enforce that only kernel cache images
signed with authorized keys or certificates are permitted to be pulled and
cached on cluster nodes, automatically rejecting unsigned or invalidly-signed
images at admission time. This approach eliminates the need for manual
signature verification, prevents the deployment of potentially compromised
kernel caches, and provides a transparent, auditable record of image provenance
through integration with transparency logs and in-toto attestation frameworks.

**1.b Webhook translates image tag to digest**

The KServe webhook is responsible for resolving the kernel cache image tag to
an immutable digest. The resolution process depends on whether Kyverno-based
signature verification is enabled in the system configuration.

**Scenario 1: Kyverno Enabled (Recommended for Production)**

When Kyverno is enabled via the system-wide ConfigMap (see Configuration
section), the webhook leverages Kyverno's signature verification results:

After Kyverno successfully verifies the kernel cache image signature, it
adds a verification annotation to the LocalModelCache CR containing the
resolved image digest:

```yaml
metadata:
  annotations:
    kyverno.io/verify-images: '{"quay.io/gkm/cache-examples@sha256:bf6f7ea60274882031ad81434aa9c9ac0e4ff280cd1513db239dbbd705b6511c":"pass"}'
```

The KServe webhook parses this `kyverno.io/verify-images` annotation to check
the `pass` status and extract the verified image digest and creates a
standardized annotation:

```yaml
metadata:
  annotations:
    serving.kserve.io/resolvedDigest: sha256:bf6f7ea60274882031ad81434aa9c9ac0e4ff280cd1513db239dbbd705b6511c
```

**Scenario 2: Kyverno Disabled (Development/Testing)**

When Kyverno is disabled, the webhook must still resolve the image tag to a
digest to ensure immutability. The webhook performs direct OCI registry
resolution:

1. The webhook detects a LocalModelCache with `kernelCache.image` specified
2. The webhook queries the OCI registry's manifest API to resolve the tag to
   its current digest
3. The webhook adds the `serving.kserve.io/resolvedDigest` annotation with
   the resolved digest

Example registry resolution:

```yaml
metadata:
  annotations:
    serving.kserve.io/resolvedDigest: sha256:bf6f7ea60274882031ad81434aa9c9ac0e4ff280cd1513db239dbbd705b6511c
```

**Note:** Without Kyverno, there is no cryptographic verification of the image
signature. The resolved digest ensures immutability but not authenticity. This
mode is suitable for development environments with trusted registries but
**not recommended for production**.

**Benefits of the `serving.kserve.io/resolvedDigest` annotation:**
- **Immutability**: The exact verified image version is locked and cannot
  change
- **Consistency**: All controllers (LocalModel, LocalModelNode) reference
  the same verified digest
- **Auditability**: The digest provides a cryptographic link to the signed
  image (when Kyverno is used)
- **Safety**: Prevents tag mutation attacks where a tag (e.g., `v1`) could
  be repointed to a different image

**Webhook Processing Steps:**

With Kyverno enabled:
1. Watch for LocalModelCache resources with `kernelCache.image` specified
2. Wait for Kyverno to add the `kyverno.io/verify-images` annotation
   (indicating successful signature verification)
3. Parse the JSON annotation to extract the image digest
4. Add the `serving.kserve.io/resolvedDigest` annotation with the extracted
   digest
5. Propagate this digest to LocalModelNode specs for cache download operations
6. If Kyverno's verification fails, the resource is rejected and no digest
   annotation is added

Without Kyverno:
1. Watch for LocalModelCache resources with `kernelCache.image` specified
2. Query the OCI registry to resolve the image tag to its current manifest
   digest
3. Add the `serving.kserve.io/resolvedDigest` annotation with the resolved
   digest
4. Propagate this digest to LocalModelNode specs for cache download operations
5. If registry resolution fails (e.g., network error, image not found), update
   status with error

**2. LocalModel Controller Processes the Request:**

The LocalModel controller (existing component) is enhanced to:

- Calculate total storage required: `modelSize + kernelCache.cacheSize`
<!-->
Billy Comment: Do we know if the PV/PVC are RO/RW? In case the cache doesn't
exist or needs to be rebuilt, need it RW and in the calculation, need buffer
of extra memory in case JIT is larger. This is probably not the place for it,
but need it considered somewhere.
-->
- Validate against LocalModelNodeGroup storage limits
- Create PersistentVolumes and PersistentVolumeClaims (existing logic,
  unchanged)
- Update LocalModelNode specs for each node in the node group with kernel
  cache image reference
- Aggregate kernel cache status from LocalModelNodes
- Validate Cache image signatures.

**3. LocalModelNode Agent Downloads and Validates:**

The LocalModelNode agent (existing DaemonSet) is enhanced to:

a. **Detect GPU hardware** on startup using nvidia-smi or rocm-smi, populating
  GPUInfo in status (GPU type, driver version, compute capability)

b. **Download model weights** using existing storage-initializer Job
  (unchanged)

c. **Download kernel cache** by creating a Kubernetes Job with a new
  `kernel-cache-initializer` container that:

    - Pulls the kernel cache OCI image using go-containerregistry
    - Validates GPU compatibility using MCV (Model Cache Vault) library
    - Extracts the cache to `/mnt/models/kernel-caches/<modelName>/` on the PVC
    - Reports results (status, compatibility, signature verification) back to
      the agent

d. **Update status** with KernelCacheStatus including download status,
   compatibility result, and resolved image digest

**4. InferenceService Uses the Cache:**

When a user creates an InferenceService referencing the cached model:

```yaml
apiVersion: serving.kserve.io/v1beta1
kind: InferenceService
metadata:
  name: llama-7b-serve
spec:
  predictor:
    model:
      modelFormat:
        name: pytorch
      storageUri: s3://models/llama-7b  # Matches LocalModelCache TODO: replace with CSI driver
      runtime: vllm
      resources:
        limits:
          nvidia.com/gpu: 1
```

The KServe webhook (existing component) is enhanced to:

- Detect that a LocalModelCache with kernel cache is configured (existing
  LocalModelCache matching logic)
- Transform storageUri to `pvc://<pvc-name>/models/llama-7b` (existing
  behavior, unchanged)
- **NEW:** Translate the kernel cache image tag into a digest.

The pod starts with the PVC mounted (existing behavior) containing both model
weights and kernel cache. The inference framework (vLLM, Triton, PyTorch)
detects the environment variable and uses the precompiled kernels instead of
JIT compiling, reducing startup time by 30-70%.

#### Sequence Diagram

```sh
User       LocalModel      LocalModelNode    kernel-cache     InferenceService
           Controller      Agent             -initializer     Webhook          vLLM Pod
  │            │               │                   │               │               │
  │ Create     │               │                   │               │               │
  │ LMC with   │               │                   │               │               │
  │ kernelCache│               │                   │               │               │
  │───────────▶│               │                   │               │               │
  │            │               │                   │               │               │
  │            │ Calculate     │                   │               │               │
  │            │ total storage │                   │               │               │
  │            │ (model+cache) │                   │               │               │
  │            │               │                   │               │               │
  │            │ Validate      │                   │               │               │
  │            │ vs limits     │                   │               │               │
  │            │               │                   │               │               │
  │            │ Create PV/PVC │                   │               │               │
  │            │ (existing)    │                   │               │               │
  │            │               │                   │               │               │
  │            │ Update LMN    │                   │               │               │
  │            │ spec with     │                   │               │               │
  │            │ kernel cache  │                   │               │               │
  │            │               │                   │               │               │
  │            │ Resolve Kernel│                   │               │               │
  │            │ cache image   │                   │               │               │
  │            │ tag to digest │                   │               │               │
  │            │               │                   │               │               │
  │            │──────────────▶│                   │               │               │
  │            │               │                   │               │               │
  │            │               │ Detect GPU        │               │               │
  │            │               │ (nvidia-smi)      │               │               │
  │            │               │                   │               │               │
  │            │               │ Create Job        │               │               │
  │            │               │ (kernel-cache-    │               │               │
  │            │               │  initializer)     │               │               │
  │            │               │──────────────────▶│               │               │
  │            │               │                   │               │               │
  │            │               │                   │ Pull OCI      │               │
  │            │               │                   │ image         │               │
  │            │               │                   │               │               │
  │            │               │                   │ Validate GPU  │               │
  │            │               │                   │ compatibility │               │
  │            │               │                   │ (MCV)         │               │
  │            │               │                   │               │               │
  │            │               │                   │ Extract to    │               │
  │            │               │                   │ PVC:          │               │
  │            │               │                   │ /kernel-caches│               │
  │            │               │                   │               │               │
  │            │               │ Update status:    │               │               │
  │            │               │ Downloaded,       │               │               │
  │            │               │ Compatible: true  │               │               │
  │            │               │◀──────────────────│               │               │
  │            │               │                   │               │               │
  │            │ Aggregate     │                   │               │               │
  │            │ status        │                   │               │               │
  │            │◀──────────────│                   │               │               │
  │            │               │                   │               │               │
  │ Create     │               │                   │               │               │
  │ ISVC       │               │                   │               │               │
  │───────────────────────────────────────────────────────────────▶│               │
  │            │               │                   │               │               │
  │            │               │                   │               │ Match LMC     │
  │            │               │                   │               │               │
  │            │               │                   │               │ Transform     │
  │            │               │                   │               │ storageUri    │
  │            │               │                   │               │ to PVC        │
  │            │               │                   │               │               │
  │            │               │                   │               │ Inject env:   │
  │            │               │                   │               │ VLLM_KERNEL_  │
  │            │               │                   │               │ CACHE=...     │
  │            │               │                   │               │               │
  │            │               │                   │               │ Create Pod    │
  │            │               │                   │               │──────────────▶│
  │            │               │                   │               │               │
  │            │               │                   │               │               │ Mount PVC
  │            │               │                   │               │               │ Use cache
  │            │               │                   │               │               │ Skip JIT
  │            │               │                   │               │               │ Fast start
```

#### Schema-Level Changes

**1. LocalModelCacheSpec Extension:**

Add an optional `kernelCache` field to the existing LocalModelCacheSpec:

- `kernelCache` (object, optional): GPU kernel cache configuration
  - `image` (string, required): OCI image reference containing precompiled
    kernels (e.g., `quay.io/myorg/llama-7b-vllm-kernels:v1@sha256:abc123...`)

**2. LocalModelNodeSpec Extension:**

Extend the existing `LocalModelInfo` struct within LocalModelNodeSpec:

- Add `kernelCacheImage` (string, optional): Kernel cache OCI image reference
  (populated by LocalModel controller)
- Add `kernelCacheFramework` (string, optional): Framework identifier

**3. LocalModelNodeStatus Extension:**

Add new status fields to track kernel cache state:

- `kernelCacheStatus` (map[string]KernelCacheStatus, optional): Per-model
  kernel cache status
  - Key: model name
  - Value: KernelCacheStatus object containing:
    - `status` (enum): Download status (reuses existing ModelStatus enum:
      `ModelDownloadPending`, `ModelDownloading`, `ModelDownloaded`,
      `ModelDownloadError`)
    - `compatible` (bool): GPU compatibility validation result
    - `message` (string, optional): Compatibility message or error details
    - `resolvedDigest` (string, optional): Resolved image digest for
      immutability
    - `signatureVerified` (bool): Signature verification result
    - `extractedAt` (metav1.Time, optional): Timestamp of successful extraction

- `gpuInfo` (GPUInfo, optional): Detected GPU information on this node
  - `gpuType` (string): GPU type (e.g., `NVIDIA A100-SXM4-40GB`)
  - `driverVersion` (string): Driver version
  - `runtimeVersion` (string): CUDA/ROCm version
  - `count` (int): Number of GPUs
  - `deviceIDs` ([]string): GPU device IDs

**4. LocalModelCacheStatus Extension:**

Add aggregated kernel cache status fields:

- `kernelCacheNodeStatus` (map[string]KernelCacheNodeStatus, optional):
  Per-node kernel cache summary
  - Key: node name
  - Value: KernelCacheNodeStatus object containing:
    - `status` (NodeStatus): Overall status
    - `compatible` (bool): GPU compatibility result
    - `gpuType` (string): GPU type on node

- `kernelCacheCopies` (KernelCacheCopies, optional): Kernel cache availability
  summary
  - `available` (int): Nodes with compatible, downloaded cache
  - `downloading` (int): Nodes currently downloading
  - `incompatible` (int): Nodes with incompatible GPU
  - `failed` (int): Nodes with download/verification errors

#### Failure Modes

| Failure Scenario | Detection | Recovery | User Impact |
| ---------------- | --------- | -------- | ----------- |
| **Image signature verification fails (Kyverno enabled)** | Kyverno webhook rejects LocalModelCache creation; no `kyverno.io/verify-images` annotation added | LocalModelCache resource rejected at admission time | User must sign image with correct key/certificate or fix Kyverno ClusterPolicy configuration |
| **GPU incompatible with cache** | MCV validation detects GPU mismatch (e.g., expected A100, found V100) | Status shows `Compatible: false` with explanation; cache not used | Pods not scheduled on incompatible nodes (via node affinity); or pods start but skip cache |
| **Cache image not found (404)** | OCI pull fails with NotFound error | Job fails; status shows error; retry with backoff | Temporary delay until registry issue resolved or image reference corrected |
| **Insufficient storage** | LocalModel controller validation before PV/PVC creation | LocalModelCache admission blocked with error message | User must increase storage limit in LocalModelNodeGroup or reduce cache size |
| **GPU driver not installed** | nvidia-smi/rocm-smi command not found or fails | GPUInfo not populated; kernel cache download skipped with warning | Kernel cache features unavailable on non-GPU nodes (expected behavior) |
| **Network partition during download** | Job timeout (default 10 minutes) | Kubernetes Job retry mechanism; agent detects timeout and recreates Job | Temporary delay; eventual consistency when network restored |
| **Corrupted cache extraction** | MCV validation post-extraction detects invalid metadata or missing files | Status shows error; cache directory cleaned up; retry on next reconciliation | Cache unavailable; fallback to JIT compilation |
| **Registry resolution fails (Kyverno disabled)** | Webhook cannot resolve image tag to digest (network error, registry down) | LocalModelCache status shows error; retry with backoff | Temporary delay until registry is accessible; user can manually specify image with digest |

#### Storage Layout

**Unified PersistentVolume Directory Structure:**

```sh
/mnt/models/                           # Root mount point (existing)
├── models/                            # Existing: Model weights
│   ├── llama-7b/
│   │   ├── pytorch_model.bin
│   │   ├── config.json
│   │   └── tokenizer.json
│   ├── gpt2/
│   │   └── ...
│   └── mistral-7b/
│       └── ...
│
└── kernel-caches/                     # NEW: GPU kernel caches
    ├── llama-7b/
    │   ├── metadata.json              # MCV metadata with GPU compatibility info
    │   ├── gpu_info.json              # Build-time GPU information
    │   └── kernels/                   # Compiled kernel binaries
    │       ├── kernel_0.cubin         # CUDA binary
    │       ├── kernel_1.cubin
    │       └── cache.json             # Framework-specific cache metadata
    ├── gpt2/
    │   └── ...
    └── mistral-7b/
        └── ...
```

Both model weights and kernel caches reside on the same PersistentVolume,
simplifying storage management and lifecycle. The total storage required is the
sum of model size and kernel cache size, validated against the
LocalModelNodeGroup storage limit.

---

### Implementation

[Where is the code going to live? What directories are impacted / changed]

#### 1. CRD Type Definitions

Files to modify:

- `pkg/apis/serving/v1alpha1/local_model_cache_types.go`
  - Add `KernelCache *KernelCacheSpec` field to `LocalModelCacheSpec`
  - Add new types: `KernelCacheSpec`, `SignaturePolicy`

- `pkg/apis/serving/v1alpha1/local_model_node_types.go`
  - Add `KernelCacheImage string` and `KernelCacheFramework string` fields to
    `LocalModelInfo`

- `pkg/apis/serving/v1alpha1/local_model_node_status.go`
  - Add `KernelCacheStatus map[string]KernelCacheStatus` field to
    `LocalModelNodeStatus`
  - Add `GPUInfo *GPUInfo` field to `LocalModelNodeStatus`
  - Add new types: `KernelCacheStatus`, `GPUInfo`

- `pkg/apis/serving/v1alpha1/local_model_cache_status.go`
  - Add `KernelCacheNodeStatus map[string]KernelCacheNodeStatus` field to
    `LocalModelCacheStatus`
  - Add `KernelCacheCopies *KernelCacheCopies` field to `LocalModelCacheStatus`
  - Add new types: `KernelCacheNodeStatus`, `KernelCacheCopies`

#### 2. LocalModel Controller

File: `pkg/controller/v1alpha1/localmodel/controller.go`

Changes:

- Add storage size calculation including kernel cache size
- Validate total storage against LocalModelNodeGroup limits before creating
  PV/PVC
- Propagate kernel cache configuration from LocalModelCache to LocalModelNode
  specs
- Aggregate kernel cache status from LocalModelNodes and update
  LocalModelCacheStatus
- Resolve Kernel Cache image tag to digest.

New functions:

- `calculateTotalStorage(lmc *LocalModelCache) resource.Quantity`
- `propagateKernelCacheInfo(ctx context.Context, lmc *LocalModelCache) error`
- `aggregateKernelCacheStatus(ctx context.Context, lmc *LocalModelCache) error`

#### 3. LocalModelNode Controller

File: `pkg/controller/v1alpha1/localmodelnode/controller.go`

Changes:

- Add GPU detection on agent startup
- Create Kubernetes Jobs with kernel-cache-initializer container for models
  with kernel cache configured
- Monitor kernel cache Job status and update KernelCacheStatus
- Validate cache compatibility after extraction
- Clean up completed/failed Jobs

New files:

- `pkg/controller/v1alpha1/localmodelnode/gpu_detector.go` - GPU hardware
  detection using nvidia-smi/rocm-smi
- `pkg/controller/v1alpha1/localmodelnode/kernel_cache_job.go` - Job creation
  and management for kernel cache downloads
- `pkg/controller/v1alpha1/localmodelnode/compatibility_validator.go` - GPU
  compatibility validation using MCV

New functions:

- `detectGPU(ctx context.Context) (*GPUInfo, error)`
- `createKernelCacheJob(ctx context.Context, lmn *LocalModelNode, modelInfo LocalModelInfo) error`
- `checkKernelCacheStatus(ctx context.Context, lmn *LocalModelNode, modelName string) error`
- `validateCacheCompatibility(cachePath string, gpuInfo *GPUInfo) (bool, string, error)`

#### 4. kernel-cache-initializer Container

New repository/directory: `python/kserve/kernelcache/`

Purpose: Init container that pulls kernel cache OCI images, validates GPU
compatibility with MCV, and extracts caches to the PVC.

Main components:

- `python/kserve/kernelcache/initializer/main.go` - Entry point
- `python/kserve/kernelcache/initializer/downloader.go` - OCI image pull logic
  using go-containerregistry
- `python/kserve/kernelcache/initializer/signature.go` - Signature verification
  using cosign
- `python/kserve/kernelcache/initializer/gpu.go` - GPU detection and compatibility
  validation using MCV
- `python/kserve/kernelcache/Dockerfile` - Container image build

Dependencies:

- `github.com/google/go-containerregistry` - OCI image operations
- `github.com/sigstore/cosign/v2` - Signature verification
- `github.com/redhat-et/MCU/mcv` - GPU compatibility validation
- `github.com/NVIDIA/go-nvml` - GPU detection (optional)

Build:

- Multi-arch support: amd64, arm64
- Published to: `kserve/kernel-cache-initializer:v0.14.0`

#### 5. InferenceService Webhook

File: `pkg/webhook/admission/pod/storage_initializer_injector.go`

Changes:

- Detect if matched LocalModelCache has `kernelCache` configured
- Inject framework-specific environment variable into predictor container (e.g.,
  `VLLM_KERNEL_CACHE=/mnt/models/kernel-caches/<modelName>`)

New function:
- `injectKernelCacheEnvVar(pod *v1.Pod, lmc *v1alpha1.LocalModelCache, modelName string) error`

Framework environment variable mapping:

- vLLM: `VLLM_KERNEL_CACHE`
- Triton: `TRITON_KERNEL_CACHE_PATH`
- PyTorch: `TORCH_COMPILE_CACHE_DIR`

#### 6. Configuration

File: `config/configmap/inferenceservice.yaml`

New section:

```yaml
data:
  localmodel: |
    enabled: true
    # ... existing config ...

    # NEW: Kernel cache configuration
    kernelCache:
      enabled: true
      initializerImage: kserve/kernel-cache-initializer:v0.14.0
      defaultFramework: vllm

    # Signature verification policy (system-wide)
    signatureVerification:
      enabled: true  # Enable Kyverno-based signature verification (recommended for production)
      # When enabled=true, Kyverno ClusterPolicy must be deployed to verify image signatures
      # When enabled=false, webhook performs direct registry digest resolution without verification

      # Note: The Kyverno ClusterPolicy configuration defines the actual signature requirements
      # (public keys, issuers, subjects). This setting only controls whether KServe expects
      # Kyverno to be present and should wait for kyverno.io/verify-images annotations.
```

#### 7. CRD Manifests

Files to regenerate:

- `config/crd/full/serving.kserve.io_localmodelcaches.yaml`
- `config/crd/full/serving.kserve.io_localmodelnodes.yaml`

Using kubebuilder markers and `make manifests` to generate updated CRDs.

#### 8. Documentation

New files:

- `docs/admin/kernel-cache.md` - Admin guide for enabling and configuring the
  feature
- `docs/modelserving/v1beta1/kernel-cache/README.md` - User guide
- `docs/modelserving/v1beta1/kernel-cache/creating-caches.md` - Building kernel
  cache OCI images
- `docs/samples/v1beta1/kernel-cache/localmodelcache-vllm.yaml` - Example
  manifest

Updated files:

- `docs/modelserving/v1beta1/llm/vllm.md` - Add kernel cache section
- `docs/reference/api.md` - Document new CRD fields

---

### Prerequisites / Dependencies

[Are there any issues / tech that need to be in place for this to work?]

System Requirements:

1. Kubernetes:

   - Minimum version: 1.24+
   - Required features: PersistentVolumes, DaemonSets, Jobs,
     MutatingWebhookConfiguration

2. KServe:

   - Minimum version: 0.13+ (requires Local Model Cache feature)
   - Local Model Cache feature must be enabled in ConfigMap
   - LocalModelNodeGroup resources must be configured for GPU nodes

3. GPU Nodes:

   - GPU drivers installed: NVIDIA (CUDA 11.0+) or AMD (ROCm 5.0+)
   - GPU accessible via device files: `/dev/nvidia*` or `/dev/dri/renderD*`
   - `nvidia-smi` (NVIDIA) or `rocm-smi` (AMD) available in PATH on nodes

4. Container Registry:

   - OCI-compliant registry accessible from cluster (e.g., quay.io, Docker Hub,
     GCR, ECR)
   - Registry credentials configured if using private registries
   - Support for image manifest digests (SHA256) required

5. Storage:

   - Node-local storage backend for PersistentVolumes (hostPath for dev, local
     volume provisioner for production)
   - Sufficient disk space: `modelSize + kernelCacheSize + 20% buffer` per
     model per node
   - Storage class with `volumeBindingMode: WaitForFirstConsumer` recommended
     for optimal node affinity

Software Dependencies:

1. Runtime Dependencies (in kernel-cache-initializer container):

   - go-containerregistry: OCI image pull and extraction
   - cosign v2.x: Signature verification (keyless and key-based)
   - MCV (Model Cache Vault): GPU compatibility validation library
   - go-nvml (optional): NVIDIA GPU detection at runtime

2. Build Dependencies:

   - Go 1.21+
   - Docker or Podman
   - Make
   - kubectl
   - kubebuilder (for CRD generation)

3. External Services (optional):

   - Sigstore services (for keyless signature verification):
     - Rekor transparency log: https://rekor.sigstore.dev
     - Fulcio CA: https://fulcio.sigstore.dev
   - Only required if using keyless cosign verification; key-based verification
     can use local keys

Kernel Cache Image Requirements:

Kernel cache OCI images must:

1. Be signed with cosign (keyless OIDC-based or key-based)
2. Contain MCV metadata in `metadata.json` describing GPU requirements and
  compatibility
3. Follow directory structure:

   ```sh
   /
   ├── metadata.json      # MCV compatibility metadata (required)
   ├── gpu_info.json      # Build-time GPU info (optional)
   └── kernels/           # Compiled kernel binaries (required)
       ├── *.cubin        # CUDA binaries
       ├── *.ptx          # PTX intermediate representation
       └── cache.json     # Framework-specific metadata
   ```
4. Be accessible from cluster nodes via registry pull (public or with
  credentials)

Framework Requirements:

1. vLLM:

   - Version: 0.3.0+ (requires `VLLM_KERNEL_CACHE` environment variable support)
   - Must check environment variable for cache location before compilation

2. Triton:

   - Version: TBD (requires `TRITON_KERNEL_CACHE_PATH` support - verify with
     NVIDIA)

3. PyTorch:

   - Version: 2.0+ (for torch.compile support)
   - Requires `TORCH_COMPILE_CACHE_DIR` environment variable support

Known Limitations:

- Alpha release supports NVIDIA GPUs only (AMD ROCm support planned for Beta)
- Signature verification requires network access to Sigstore services (for
  keyless) or access to public keys
- Heterogeneous GPU clusters require multiple kernel cache images (one per GPU
  architecture)

---

### Integration Checklist

#### Phase 1: Core Integration (Alpha) - Q1 2026

##### CRD and API Changes:

- [ ] Extend `LocalModelCacheSpec` with `KernelCache *KernelCacheSpec` field
- [ ] Extend `LocalModelNodeSpec` and `LocalModelNodeStatus` with kernel cache
  fields
- [ ] Extend `LocalModelCacheStatus` with kernel cache aggregation fields
- [ ] Add CRD validation rules (kubebuilder markers)
- [ ] Generate updated CRD manifests
- [ ] Update API documentation

##### kernel-cache-initializer Container:

- [ ] Implement OCI image pull using go-containerregistry
- [ ] Implement cosign signature verification (keyless)
- [ ] Integrate MCV library for GPU compatibility validation
- [ ] Implement GPU detection wrapper (nvidia-smi)
- [ ] Implement cache extraction to output directory
- [ ] Add error handling and logging
- [ ] Write unit tests
- [ ] Build and publish container image (amd64)

##### LocalModel Controller:

- [ ] Add storage size calculation including kernel cache
- [ ] Validate total storage against LocalModelNodeGroup limits
- [ ] Propagate kernel cache info to LocalModelNode specs
- [ ] Aggregate kernel cache status from LocalModelNodes
- [ ] Update LocalModelCacheStatus with kernel cache fields
- [ ] Add unit tests

##### LocalModelNode Controller:

- [ ] Implement GPU detection on agent startup
- [ ] Create Jobs with kernel-cache-initializer for models with kernel caches
- [ ] Monitor Job status and update KernelCacheStatus
- [ ] Validate cache compatibility post-extraction
- [ ] Clean up completed/failed Jobs
- [ ] Add unit tests

##### InferenceService Webhook:

- [ ] Detect LocalModelCache with kernel cache configured
- [ ] Inject framework-specific environment variable (vLLM only for Alpha)
- [ ] Add unit tests

##### Configuration and Documentation:

- [ ] Add kernel cache configuration to inferenceservice ConfigMap
- [ ] Create admin guide (docs/admin/kernel-cache.md)
- [ ] Create user guide (docs/modelserving/v1beta1/kernel-cache/README.md)
- [ ] Create example manifests
- [ ] Update vLLM documentation

##### Testing:

- [ ] Unit tests for all new code (>80% coverage)
- [ ] Integration test: LocalModelCache with kernel cache → Job creation →
  status update
- [ ] E2E test: Full workflow with vLLM on GPU node
- [ ] Performance benchmark: Measure startup time improvement

#### Phase 2: Enhanced Integration (Beta) - Q2 2026

##### Multi-Framework Support:

- [ ] Add Triton Inference Server support
- [ ] Add PyTorch (torch.compile) support
- [ ] Framework auto-detection from InferenceService spec
- [ ] Add integration tests for each framework

##### GPU Platform Support:

- [ ] Add AMD ROCm GPU detection (rocm-smi)
- [ ] Test on AMD GPUs
- [ ] Support heterogeneous clusters (NVIDIA + AMD)

##### Enhanced Validation:

- [ ] Implement admission webhook validation for LocalModelCache
- [ ] Validate kernel cache image exists before accepting resource
- [ ] Validate signature policy configuration

##### Observability:

- [ ] Add Prometheus metrics (downloads, duration, verifications, compatibility
  checks)
- [ ] Add structured logging
- [ ] Add Kubernetes events to LocalModelCache and LocalModelNode
- [ ] Create example Grafana dashboard

##### Failure Handling:

- [ ] Implement retry logic with exponential backoff
- [ ] Add detailed status conditions for error reporting
- [ ] Add cache corruption detection and recovery

##### Testing and Validation:

- [ ] Integration tests for all frameworks
- [ ] Integration tests for failure modes
- [ ] Performance benchmarks for all frameworks (>30% improvement)
- [ ] Load testing (50+ concurrent InferenceServices)

#### Phase 3: Cache Warming (GA) - Q2 2026

##### Automatic Cache Generation:

- [ ] Implement cache warming controller
- [ ] Watch LocalModelCache create/update events
- [ ] Trigger cache generation Job when new model cached
- [ ] Package generated cache using MCV
- [ ] Build OCI image, sign with cosign, push to registry
- [ ] Update LocalModelCache spec with kernel cache image reference

##### Cache Management:

- [ ] Implement cache versioning
- [ ] Detect framework version changes and trigger re-warming
- [ ] Implement cache invalidation on model update
- [ ] Add TTL-based cache cleanup

##### Production Readiness:

- [ ] Security audit (external)
- [ ] Performance audit
- [ ] Comprehensive documentation
- [ ] Operator runbooks

---

## Operations

[How is this feature implemented or turned on by the user / operator?]

### For Cluster Administrators:

#### Step 1: Enable Local Model Cache (if not already enabled)

Edit the KServe ConfigMap to enable the Local Model Cache feature:

```bash
kubectl edit configmap inferenceservice-config -n kserve
```

Ensure the `localmodel` section exists:

```yaml
data:
  localmodel: |
    enabled: true
    jobNamespace: kserve-localmodel-jobs
```

#### Step 2: Configure Kernel Cache (optional)

Add kernel cache configuration to the same ConfigMap:

```yaml
data:
  localmodel: |
    enabled: true
    jobNamespace: kserve-localmodel-jobs

    # NEW: Kernel cache configuration
    kernelCache:
      enabled: true
      initializerImage: kserve/kernel-cache-initializer:v0.14.0

    # Signature verification (system-wide)
    signatureVerification:
      enabled: true  # Require Kyverno-based signature verification (recommended for production)
```

#### Step 2a: Deploy Kyverno ClusterPolicy (if signature verification enabled)

If `signatureVerification.enabled: true`, deploy a Kyverno ClusterPolicy to verify kernel cache image signatures:

```yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: verify-kernel-cache-images
spec:
  validationFailureAction: enforce
  background: false
  webhookTimeoutSeconds: 30
  failurePolicy: Fail
  rules:
    - name: verify-kernel-cache-signature
      match:
        any:
        - resources:
            kinds:
              - serving.kserve.io/v1alpha1/LocalModelCache
      verifyImages:
      - imageReferences:
        - "*"  # Match all kernel cache images
        attestors:
        - count: 1
          entries:
          - keyless:
              subject: "*@yourdomain.com"  # Adjust to your OIDC subject pattern
              issuer: "https://github.com/login/oauth"  # Or your OIDC provider
              # Alternatively, use public keys:
              # - keys:
              #     publicKeys: |-
              #       -----BEGIN PUBLIC KEY-----
              #       ...
              #       -----END PUBLIC KEY-----
```

**Note:** If `signatureVerification.enabled: false`, skip this step. The webhook will perform direct registry digest resolution without signature verification.

#### Step 3: Create LocalModelNodeGroup for GPU Nodes

Define a node group for GPU nodes with local storage:

```yaml
apiVersion: serving.kserve.io/v1alpha1
kind: LocalModelNodeGroup
metadata:
  name: gpu-node-group
spec:
  storageLimit: 100Gi  # Total storage per node (models + kernel caches)

  persistentVolumeSpec:
    capacity:
      storage: 100Gi
    accessModes:
      - ReadWriteOnce
    local:
      path: /mnt/models
    nodeAffinity:
      required:
        nodeSelectorTerms:
        - matchExpressions:
          - key: nvidia.com/gpu
            operator: Exists

  persistentVolumeClaimSpec:
    accessModes:
      - ReadWriteOnce
    resources:
      requests:
        storage: 100Gi
```

#### Step 4: Verify kernel-cache-initializer Image

Optionally pre-pull the kernel-cache-initializer image to all GPU nodes:

```bash
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: kernel-cache-init-prepull
  namespace: kserve
spec:
  selector:
    matchLabels:
      name: kernel-cache-init-prepull
  template:
    metadata:
      labels:
        name: kernel-cache-init-prepull
    spec:
      nodeSelector:
        nvidia.com/gpu: exists
      initContainers:
      - name: prepull
        image: kserve/kernel-cache-initializer:v0.14.0
        command: ["sh", "-c", "echo Image pulled"]
      containers:
      - name: pause
        image: gcr.io/google_containers/pause:3.1
EOF
```

### For ML Engineers/Users:

#### Step 1: Create LocalModelCache with Kernel Cache

Create a LocalModelCache resource that includes both model weights and kernel
cache configuration:

```yaml
apiVersion: serving.kserve.io/v1alpha1
kind: LocalModelCache
metadata:
  name: llama-7b
spec:
  # Model configuration (existing)
  sourceModelUri: s3://models/llama-7b
  modelSize: 13Gi
  nodeGroups:
    - gpu-node-group

  # Kernel cache configuration (new)
  kernelCache:
    image: quay.io/myorg/llama-7b-vllm-kernels:v1
```

#### Step 2: Verify Cache Status

Check the LocalModelCache status to ensure both model weights and kernel caches
are downloaded:

```bash
kubectl get localmodelcache llama-7b -o yaml
```

Look for `kernelCacheCopies` in the status:

```yaml
status:
  # Model weight status (existing)
  copies:
    available: 3
    total: 3
    failed: 0

  # Kernel cache status (new)
  kernelCacheCopies:
    available: 3
    downloading: 0
    incompatible: 0
    failed: 0
```

Check per-node status:
```bash
kubectl get localmodelnode node-1 -o jsonpath='{.status.kernelCacheStatus.llama-7b}' | jq
```

#### Step 3: Create InferenceService

Create an InferenceService as usual. The kernel cache will be automatically
detected and mounted:

```yaml
apiVersion: serving.kserve.io/v1beta1
kind: InferenceService
metadata:
  name: llama-7b-serve
spec:
  predictor:
    model:
      modelFormat:
        name: pytorch
      storageUri: s3://models/llama-7b  # Matches LocalModelCache sourceModelUri
      runtime: vllm
      resources:
        limits:
          nvidia.com/gpu: 1
```

#### Step 4: Verify Kernel Cache is Used

Check the pod environment variables to confirm the kernel cache path is set:

```bash
POD=$(kubectl get pod -l serving.kserve.io/inferenceservice=llama-7b-serve -o jsonpath='{.items[0].metadata.name}')
kubectl exec $POD -- env | grep KERNEL_CACHE
```

Expected output:

```bash
VLLM_KERNEL_CACHE=/mnt/models/kernel-caches/llama-7b
```

Verify the cache directory is mounted and contains kernels:
```bash
kubectl exec $POD -- ls -la /mnt/models/kernel-caches/llama-7b/
```

#### Feature Toggles:

1. **Global enable/disable** (in ConfigMap):

   ```yaml
   localmodel:
     kernelCache:
       enabled: false  # Disable kernel cache globally
   ```

2. **Per-model opt-in** (omit `kernelCache` field):

   ```yaml
   spec:
     sourceModelUri: s3://models/gpt2
     modelSize: 5Gi
     nodeGroups:
       - gpu-node-group
     # No kernelCache field = no kernel caching for this model
   ```

3. **Disable signature verification** (NOT recommended for production):
   ```yaml
   signatureVerification:
     enabled: false  # Disable Kyverno-based signature verification (development only)
   ```

---

## Observability

[Will this feature need instrumentation or measures that are exposed to specific personas? If so, which personas and optics are needed?]

### Metrics

### Kernel Cache Metrics (to be added to KServe):

1. `kserve_kernel_cache_downloads_total{status="success|failure"}` - Counter of kernel cache download attempts
2. `kserve_kernel_cache_download_duration_seconds{model="<name>"}` - Histogram of download duration
3. `kserve_kernel_cache_size_bytes{model="<name>", node="<node>"}` - Gauge of extracted cache size
4. `kserve_kernel_cache_signature_verifications_total{result="success|failure"}` - Counter of signature verification attempts
5. `kserve_kernel_cache_compatibility_checks_total{result="compatible|incompatible"}` - Counter of GPU compatibility checks

### InferenceService Startup Metrics (to be added):

6. `kserve_predictor_startup_duration_seconds{kernel_cache="enabled|disabled", framework="vllm|triton|pytorch"}` - Histogram of predictor startup time, labeled by kernel cache usage

### Example Prometheus Queries:

```promql
# Cache hit rate (caches successfully used)
sum(rate(kserve_kernel_cache_downloads_total{status="success"}[5m])) /
sum(rate(kserve_kernel_cache_downloads_total[5m]))

# Average startup time improvement
avg(kserve_predictor_startup_duration_seconds{kernel_cache="disabled"}) /
avg(kserve_predictor_startup_duration_seconds{kernel_cache="enabled"})

# Cache storage usage per node
sum by (node) (kserve_kernel_cache_size_bytes)
```

### Logging

#### LocalModel Controller Logs:

```sh
level=info msg="Processing LocalModelCache with kernel cache" localModelCache=llama-7b kernelCacheImage=quay.io/myorg/llama-7b-vllm-kernels:v1
level=info msg="Total storage required" localModelCache=llama-7b modelSize=13Gi cacheSize=500Mi totalSize=13.5Gi
level=info msg="Kernel cache propagated to LocalModelNode" localModelCache=llama-7b node=node-1
level=error msg="Storage limit exceeded" localModelCache=llama-7b required=13.5Gi limit=10Gi
```

#### LocalModelNode Controller Logs:

```sh
level=info msg="Detected GPU" node=node-1 gpuType="NVIDIA A100-SXM4-40GB" driverVersion="535.104.05" computeCapability="8.0"
level=info msg="Creating kernel cache download job" model=llama-7b image=quay.io/myorg/llama-7b-vllm-kernels:v1
level=info msg="Kernel cache job completed successfully" model=llama-7b duration=45s
level=warn msg="GPU incompatible with kernel cache" model=llama-7b expected="A100" actual="V100"
level=error msg="Kernel cache download failed" model=llama-7b error="signature verification failed"
```

#### kernel-cache-initializer Logs:

```sh
level=info msg="Starting kernel cache download" image=quay.io/myorg/llama-7b-vllm-kernels:v1
level=info msg="Verifying signature" method=keyless issuer="https://github.com/login/oauth"
level=info msg="Signature verified successfully" digest=sha256:abc123...
level=info msg="Validating GPU compatibility" gpuType="NVIDIA A100" requiredType="A100" requiredCapability="8.0"
level=info msg="GPU compatible" compatible=true
level=info msg="Extracting cache" output=/mnt/models/kernel-caches/llama-7b size=512MB
level=info msg="Kernel cache ready" duration=42s
```

### Kubernetes Events

#### LocalModelCache Events:

```bash
kubectl describe localmodelcache llama-7b

Events:
  Type    Reason                     Message
  ----    ------                     -------
  Normal  KernelCacheDownloading     Started downloading kernel cache on 3 nodes
  Normal  KernelCacheDownloaded      Kernel cache available on 3 nodes
  Warning KernelCacheIncompatible    Kernel cache incompatible with GPU on node-4 (expected A100, found V100)
  Warning SignatureVerificationFailed Signature verification failed for image quay.io/myorg/llama-7b-vllm-kernels:v1
```

#### LocalModelNode Events:

```bash
kubectl describe localmodelnode node-1

Events:
  Type    Reason                    Message
  ----    ------                    -------
  Normal  GPUDetected               Detected NVIDIA A100-SXM4-40GB, driver 535.104.05, compute capability 8.0
  Normal  KernelCacheJobCreated     Created job kernel-cache-node-1-llama-7b
  Normal  KernelCacheDownloaded     Kernel cache downloaded successfully (512MB in 42s)
  Normal  KernelCacheCompatible     Cache compatible with node GPU
```

### Personas and Monitoring Needs

| Persona | What to Monitor | Tools | Actions |
|---------|----------------|-------|---------|
| **Platform Operator** | - Kernel cache download failures<br>- Storage usage across nodes<br>- Job failures<br>- Signature verification failures | - Prometheus alerts<br>- `kubectl get localmodelcache -A`<br>- Job logs<br>- Grafana dashboards | - Investigate registry issues<br>- Increase storage limits<br>- Check GPU driver versions<br>- Rotate signing keys |
| **ML Engineer** | - Cache availability for their models<br>- Startup time improvements<br>- GPU compatibility issues | - LocalModelCache status<br>- InferenceService events<br>- Pod logs showing cache usage | - Verify cache image exists and is signed<br>- Check GPU requirements match cluster<br>- Update kernel cache image version |
| **SRE** | - System-wide performance metrics<br>- Startup time trends<br>- Security (unsigned images) | - Grafana dashboards<br>- Prometheus alerts<br>- Audit logs | - Optimize cache distribution<br>- Monitor signature verification<br>- Track cost savings from faster startup |

### Example Prometheus Alerts

```yaml
groups:
- name: kserve-kernel-cache
  rules:
  - alert: KernelCacheDownloadFailureHigh
    expr: |
      rate(kserve_kernel_cache_downloads_total{status="failure"}[10m]) > 0.2
    for: 15m
    labels:
      severity: warning
    annotations:
      summary: High kernel cache download failure rate
      description: "{{ $value | humanizePercentage }} of kernel cache downloads are failing"

  - alert: KernelCacheSignatureVerificationFailed
    expr: |
      increase(kserve_kernel_cache_signature_verifications_total{result="failure"}[5m]) > 0
    labels:
      severity: critical
    annotations:
      summary: Kernel cache signature verification failed
      description: Unsigned or improperly signed kernel cache image detected

  - alert: KernelCacheStorageLimitApproaching
    expr: |
      sum by (node) (kserve_kernel_cache_size_bytes) /
      on(node) group_left kube_node_status_capacity{resource="ephemeral_storage"} > 0.8
    for: 30m
    labels:
      severity: warning
    annotations:
      summary: Kernel cache storage limit approaching on {{ $labels.node }}
      description: "Node {{ $labels.node }} kernel cache storage at {{ $value | humanizePercentage }} capacity"
```

---

## Test Plan

[How is the feature tested for use? i.e unit testing, E2E, isolated or in conjunction with other components? what conformance tests need to be in place?]

### Unit Tests

Location: `pkg/apis/serving/v1alpha1/`, `pkg/controller/v1alpha1/`

Coverage Target: >80% for all new code

### Test Cases:

1. **CRD Validation:**
   - LocalModelCacheSpec with valid `kernelCache` field validates successfully
   - Invalid `kernelCache.image` format is rejected

2. **Storage Calculation:**
   - `calculateTotalStorage()` correctly sums `modelSize + kernelCache.cacheSize`
   - Storage validation rejects when total exceeds `LocalModelNodeGroup.spec.storageLimit`
   - Storage validation passes when total is within limits

3. **LocalModel Controller:**
   - `propagateKernelCacheInfo()` correctly sets `kernelCacheImage` in LocalModelNode specs
   - `aggregateKernelCacheStatus()` correctly aggregates status from multiple LocalModelNodes
   - Handles LocalModelCache without `kernelCache` field gracefully (no errors)
   - `KernelCacheCopies` counts are accurate (available, downloading, incompatible, failed)

4. **LocalModelNode Controller:**
   - GPU detection populates `GPUInfo` correctly from nvidia-smi output
   - `createKernelCacheJob()` generates Job with correct init container args
   - Status updates based on Job status (Pending → Downloading → Downloaded)
   - Handles Job failures with appropriate error messages in status
   - Compatibility validation sets `Compatible=false` for mismatched GPUs

5. **Webhook Injection:**
   - `injectKernelCacheEnvVar()` sets `VLLM_KERNEL_CACHE` for vLLM framework
   - Skips injection when `kernelCache` is nil
   - Uses correct framework when explicitly specified in `kernelCache.framework`

### Integration Tests

Location: `test/e2e/localmodel/`

Test Environment: KIND cluster with GPU simulation or real GPU nodes

#### Test Cases:

1. **Basic Kernel Cache Workflow:**
   - Create LocalModelNodeGroup
   - Create LocalModelCache with `kernelCache` field
   - Verify LocalModelNode resources are created
   - Verify kernel cache download Job is created with correct spec
   - Simulate Job success (mock kernel-cache-initializer completion)
   - Verify `KernelCacheStatus` updated to `Downloaded` with `Compatible=true`
   - Create InferenceService matching the LocalModelCache
   - Verify pod has environment variable set correctly

2. **GPU Compatibility Validation:**
   - Create LocalModelCache
   - Mock node with V100 GPU
   - Verify `KernelCacheStatus.Compatible=false`
   - Verify status message explains incompatibility ("expected A100, found V100")

3. **Signature Verification (with Kyverno enabled):**
   - Configure `signatureVerification.enabled=true` in ConfigMap
   - Deploy Kyverno ClusterPolicy with signature requirements
   - Create LocalModelCache with unsigned image (or invalid signature)
   - Verify LocalModelCache resource is rejected at admission time by Kyverno
   - Verify no `kyverno.io/verify-images` annotation is added

4. **Storage Limit Enforcement:**
   - Create LocalModelNodeGroup with `storageLimit=10Gi`
   - Create LocalModelCache with `modelSize=8Gi` and `kernelCache.cacheSize=5Gi`
   - Verify admission fails with storage limit exceeded error

5. **Job Failure and Retry:**
   - Simulate Job failure (image pull error)
   - Verify status updated to `ModelDownloadError`
   - Delete failed Job
   - Verify controller creates new Job (retry logic)

### End-to-End Tests

Location: `test/e2e/`

Test Environment: Real GPU cluster (A100 or V100 nodes)

#### Test Cases:

1. **Full vLLM Workflow:**

   - Deploy LocalModelNodeGroup on GPU nodes
   - Build real kernel cache image for vLLM + Llama-7B (or use pre-built)
   - Sign image with cosign (keyless)
   - Push to test registry
   - Create LocalModelCache with `kernelCache` pointing to signed image
   - Wait for cache download and verification on all nodes
   - Verify `kernelCacheCopies.available > 0`
   - Create InferenceService with vLLM runtime
   - Send inference request
   - Measure startup time from pod creation to first successful response
   - Delete InferenceService
   - Create new InferenceService (should reuse cache)
   - Verify second startup is faster
   - Expected: >30% startup time reduction

2. **Heterogeneous GPU Cluster:**

   - Cluster with A100 and V100 nodes
   - Create two LocalModelCaches (same model, different kernel caches for A100 and V100)
   - Verify correct cache is downloaded to each node type
   - Create InferenceServices with node selectors targeting each GPU type
   - Verify correct cache is used on each

3. **Cache Update Workflow:**

   - Create LocalModelCache with `kernelCache.image` v1
   - Wait for download
   - Update `kernelCache.image` to v2
   - Verify old cache is cleaned up
   - Verify new cache is downloaded
   - Verify InferenceService uses new cache (check resolved digest in pod)

### Performance Tests

#### Benchmarks:

1. **Startup Time Comparison:**

Measure time from pod creation to first successful inference request:

| Scenario | Without Cache | With Cache | Improvement |
|----------|--------------|------------|-------------|
| vLLM Llama-7B on A100 | ~90s | ~30s | 67% |
| vLLM Mistral-7B on A100 | ~85s | ~28s | 67% |
| Triton BERT-large on V100 | ~45s | ~18s | 60% |

**Target for GA:** >30% improvement

2. **Cache Download Time:**

Measure time for kernel-cache-initializer to complete (pull + verify + extract):

| Cache Size | Network | Expected Time |
|-----------|---------|---------------|
| 100MB | 1Gbps | <10s |
| 500MB | 1Gbps | <20s |
| 1GB | 1Gbps | <40s |

3. **Storage Overhead:**

| Model | Model Size | Cache Size | Overhead |
|-------|-----------|-----------|----------|
| Llama-7B | 13GB | 500MB | 3.8% |
| Llama-13B | 26GB | 800MB | 3.1% |

**Target:** <5% storage overhead

### Conformance Tests

#### Alpha Requirements:
- All unit tests pass with >80% coverage
- Integration test with vLLM passes on GPU cluster
- Performance benchmark shows >20% improvement

#### Beta Requirements:
- All unit and integration tests pass
- E2E tests for vLLM, Triton, PyTorch
- Performance >30% improvement across all frameworks
- Failure mode tests pass (signature fail, GPU mismatch, network errors)

#### GA Requirements:
- All tests pass with >99% reliability over 30-day period
- Performance validated in 3+ production environments
- Security audit completed
- Load testing (100+ concurrent InferenceServices)

---

## Documentation
[What personas will use this feature and which documented use-cases does this affect? Are there new use-cases that need to be written or existing ones edited?]

### Personas

1. **Platform Operators/Admins** - Deploy and configure KServe with kernel cache support
2. **ML Engineers** - Use kernel caches to accelerate their InferenceServices
3. **Framework Developers** - Integrate new frameworks with kernel caching

### New Use-Cases

#### Use-Case 1: Fast Model Deployment for Production LLM Serving

- **Persona:** ML Engineer deploying Llama-7B with vLLM for customer-facing chatbot
- **Problem:** Model takes 90 seconds to start, causing slow deployments and poor auto-scaling response
- **Solution:** Create LocalModelCache with kernel cache, reducing startup to 30 seconds
- **Impact:** 3x faster deployments, better auto-scaling, improved user experience

#### Use-Case 2: Multi-Model Serving with Heterogeneous GPUs

- **Persona:** Platform Operator managing cluster with A100, H100, and V100 nodes
- **Problem:** Different GPU types require different kernel compilations, causing failures
- **Solution:** Create multiple kernel cache images (one per GPU type)
- **Impact:** Predictable startup times across heterogeneous hardware

#### Use-Case 3: Secure Kernel Cache Distribution

- **Persona:** Security-conscious Platform Operator in regulated industry
- **Problem:** Need to ensure kernel caches are not tampered with or from untrusted sources
- **Solution:** Enable Kyverno-based signature verification system-wide (`signatureVerification.enabled=true`), deploy ClusterPolicy to reject unsigned images
- **Impact:** Secure supply chain for GPU artifacts with cryptographic verification

### Documentation Deliverables

#### Admin Guides:

1. **`docs/admin/kernel-cache-setup.md`** - Installation and Configuration
   - Prerequisites and system requirements
   - Enabling kernel cache feature in KServe ConfigMap
   - Configuring LocalModelNodeGroups for GPU nodes
   - Deploying kernel-cache-initializer
   - Security best practices (cosign setup, RBAC)

2. **`docs/admin/kernel-cache-monitoring.md`** - Observability and Troubleshooting
   - Monitoring kernel cache status with kubectl
   - Prometheus metrics and alerts
   - Common issues and solutions
   - Performance tuning

#### User Guides:

1. **`docs/modelserving/v1beta1/kernel-cache/README.md`** - User Guide
   - Overview: What are GPU kernel caches and why they matter (30-70% faster startup)
   - Quick Start: Create LocalModelCache with kernel cache in 5 minutes
   - Configuration reference: All `kernelCache` fields explained
   - Verifying cache usage: How to check if cache is mounted and used
   - Troubleshooting common issues

2. **`docs/modelserving/v1beta1/kernel-cache/creating-caches.md`** - Building Kernel Cache Images
   - Step-by-step guide to generating kernel caches
   - Packaging caches with MCV
   - Building OCI images
   - Signing with cosign (keyless and key-based)
   - Pushing to registry

3. **Framework-Specific Guides:**
   - `docs/modelserving/v1beta1/kernel-cache/vllm.md` - vLLM integration
   - `docs/modelserving/v1beta1/kernel-cache/triton.md` - Triton integration (Beta)
   - `docs/modelserving/v1beta1/kernel-cache/pytorch.md` - PyTorch integration (Beta)

#### API Reference:

1. **Update `docs/reference/api.md`:**
   - Document new `LocalModelCacheSpec.kernelCache` field
   - Document new status fields (`kernelCacheNodeStatus`, `kernelCacheCopies`)
   - Document new types (`KernelCacheSpec`, `SignaturePolicy`,
     `KernelCacheStatus`, `GPUInfo`)

#### Sample Manifests:

1. **`docs/samples/v1beta1/kernel-cache/`**
   - `localmodelcache-vllm.yaml` - LocalModelCache with vLLM kernel cache
   - `localmodelcache-triton.yaml` - LocalModelCache with Triton kernel cache
   - `inferenceservice-vllm.yaml` - InferenceService using cached model + kernels
   - `localmodelnodegroup-gpu.yaml` - LocalModelNodeGroup for GPU nodes

#### Updated Existing Documentation:

1. **`docs/modelserving/v1beta1/llm/vllm.md`:**
   - Add new section: "Accelerating Startup with GPU Kernel Cache"
   - Include before/after startup time comparison
   - Link to kernel cache user guide

2. **`docs/modelserving/v1beta1/triton/README.md`:**
   - Add kernel cache section (Beta release)

3. **`docs/admin/localmodel.md`:**
   - Update with kernel cache capabilities
   - Update storage planning section to include kernel cache sizes

---

## Exit Criteria

[What are the requirements to exit each stage]

### Alpha

[exit criteria]

#### Functionality

- [ ] LocalModelCache CRD extended with `kernelCache` field (backward compatible)
- [ ] LocalModelNode controller handles kernel cache downloads using Jobs
- [ ] kernel-cache-initializer container built, tested, and published
- [ ] GPU detection working on NVIDIA GPUs (nvidia-smi)
- [ ] Signature verification with cosign (keyless OIDC via Sigstore)
- [ ] GPU compatibility validation using MCV library
- [ ] InferenceService webhook injects environment variable for kernel cache path
- [ ] Single framework support: vLLM

#### Testing

- [ ] Unit tests: >80% coverage for all new code
- [ ] Integration test: Full workflow from LocalModelCache creation to status update
- [ ] E2E test: vLLM on real GPU node with signed kernel cache image
- [ ] Performance benchmark: >20% startup time improvement (measured on A100 with Llama-7B)

#### Documentation

- [ ] Admin setup guide published
- [ ] User guide with vLLM example published
- [ ] API reference updated with new CRD fields
- [ ] Example manifests available

#### Quality

- [ ] Code review completed by at least 2 maintainers
- [ ] No critical or high-severity bugs
- [ ] Known limitations clearly documented

#### Deployment

- [ ] Alpha tested in at least 1 production-like environment (GPU cluster with signed images)
- [ ] Feedback collected from alpha users (at least 2 external users)

#### Acceptable Limitations for Alpha

- Manual kernel cache image creation required (no cache warming)
- Single framework support (vLLM only)
- Basic error handling (no automatic retry beyond Kubernetes Job retry)
- NVIDIA GPUs only (no AMD ROCm support)
- Keyless signature verification only (no support for custom keys)

#### Target Release:** KServe v0.14.0 (Q2 2026)

---

### Beta

[exit criteria]

#### Functionality:

- [ ] Multi-framework support: vLLM, Triton Inference Server, PyTorch (torch.compile)
- [ ] Framework auto-detection from InferenceService spec when `framework` not explicitly set
- [ ] Enhanced GPU detection: NVIDIA (nvidia-smi) and AMD (rocm-smi)
- [ ] Signature verification: both keyless (Sigstore) and key-based (custom public keys)
- [ ] GPU compatibility validation working for heterogeneous clusters (A100, V100, H100)
- [ ] Automatic retry logic for failed downloads (exponential backoff, max 3 retries)
- [ ] Support for multiple kernel cache versions per model (via image tags/digests)

#### Testing:

- [ ] All unit tests pass
- [ ] Integration tests for all supported frameworks (vLLM, Triton, PyTorch)
- [ ] Integration tests for all failure modes (signature fail, GPU mismatch, network partition, storage full)
- [ ] E2E tests on heterogeneous GPU cluster (NVIDIA A100 + V100 nodes)
- [ ] Performance benchmarks: >30% startup improvement across all frameworks
- [ ] Load testing: 50+ concurrent InferenceServices with kernel caches

#### Observability:

- [ ] Prometheus metrics implemented and tested
- [ ] Structured logging with configurable log levels
- [ ] Kubernetes events for user visibility (LocalModelCache and LocalModelNode)
- [ ] Example Grafana dashboard published

#### Documentation:

- [ ] Complete user guides for all frameworks (vLLM, Triton, PyTorch)
- [ ] Troubleshooting guide with common issues and solutions
- [ ] Security best practices guide (cosign key management, RBAC)
- [ ] Performance tuning guide

#### Quality:

- [ ] No critical bugs
- [ ] High-severity bugs resolved or documented with workarounds
- [ ] Security review completed (internal)
- [ ] All failure modes handled gracefully with clear error messages

#### Deployment:

- [ ] Beta tested in at least 3 production environments (different organizations)
- [ ] Positive feedback from beta users (survey with >80% satisfaction)
- [ ] Performance validated in production workloads (at least 100 InferenceServices deployed)

#### Acceptable Limitations for Beta:

- Manual kernel cache creation still required (cache warming not yet available)
- Cache versioning requires manual image tag management
- No automatic cache invalidation (user must update image manually)

#### Target Release: KServe v0.15.0 (Q3 2026)

---

### GA

[exit criteria]

#### Functionality:

- [ ] All Beta features stable and production-ready
- [ ] Automatic cache warming controller implemented and tested
  - Watches LocalModelCache create/update events
  - Triggers cache generation Job when new model cached
  - Packages cache using MCV, builds OCI image, signs with cosign, pushes to registry
  - Updates LocalModelCache spec with generated `kernelCache.image`
- [ ] Cache lifecycle management
  - Cache invalidation on model or framework version updates
  - TTL-based cache cleanup for unused caches
  - Cache versioning (automatic or manual)
- [ ] Advanced signature policies
  - Support for multiple signers (any-of, all-of)
  - Transparency log verification (Rekor)
  - Custom trust roots

#### Testing:

- [ ] All unit, integration, and E2E tests passing consistently (>99% success rate over 30 days)
- [ ] Performance benchmarks validated across 5+ production deployments
- [ ] Load testing: 200+ concurrent InferenceServices
- [ ] Chaos testing: network failures, node failures, registry failures, driver failures
- [ ] Long-running stability test: 30+ days continuous operation
- [ ] Upgrade testing: smooth upgrade path from Beta

#### Observability:

- [ ] SLO defined and validated: "95% of kernel cache downloads succeed within 5 minutes"
- [ ] SLI dashboards implemented and tested
- [ ] Alerting rules validated in production
- [ ] Runbooks published for common operational scenarios

#### Documentation:

- [ ] Complete documentation for all features (cache warming, lifecycle management)
- [ ] Operator runbooks for production operations
- [ ] Upgrade guides from Alpha and Beta
- [ ] FAQ with solutions to common issues
- [ ] Video tutorials (optional but recommended)

#### Quality:

- [ ] Zero critical bugs
- [ ] Zero high-severity bugs
- [ ] Security audit completed by external security firm
- [ ] Performance audit completed
- [ ] Accessibility review (docs, APIs)
- [ ] License compliance verified

#### Deployment:

- [ ] Production deployments in 5+ organizations across different industries
- [ ] Case studies published demonstrating business impact (cost savings, improved UX)
- [ ] Community feedback incorporated (from KServe Slack, GitHub issues, community meetings)
- [ ] Upgrade path tested from Beta in at least 3 production environments

#### Ecosystem Integration:

- [ ] Integration with service mesh (Istio) tested
- [ ] Integration with GitOps (ArgoCD, Flux) tested
- [ ] Integration with monitoring (Prometheus, Grafana) validated
- [ ] CLI tools for kernel cache management (optional but recommended)

#### Support:

- [ ] SLA defined for critical issues (<24 hours response)
- [ ] Support documentation published
- [ ] Community support channels active (Slack #kserve-dev, GitHub Discussions)
- [ ] Training materials available for operators and users

#### Performance Targets:

- [ ] >30% startup time improvement validated across 10+ different models
- [ ] <5% storage overhead validated
- [ ] <10 minutes cache download time for 95th percentile (2GB caches)

#### Target Release: KServe v1.0.0 or v0.16.0 (Q4 2026)

---

## Alternatives Considered

[What other approaches to solving this problem were considered? What rationale
was used to select the specific design over other methods?]

### Alternative 1: Separate GKM Control Plane with CSI Driver

#### Description

Deploy GKM as a standalone system alongside KServe with its own CRDs
(`GKMCache`, `GKMCacheNode`), controllers (Operator), DaemonSet agent, and CSI
driver for volume mounting.

#### Approach:

- User creates both `LocalModelCache` (for model weights) and `GKMCache` (for
  kernel caches)
- GKM stores caches on hostPath at `/var/lib/gkm/caches/`
- GKM CSI driver (`csi.gkm.io`) mounts caches into pods via CSI ephemeral
  volumes
- InferenceService references `GKMCache` via annotation (e.g.,
  `serving.kserve.io/gkm-cache: "my-cache"`)
- Separate status tracking, separate storage, separate lifecycle

#### Pros

- Clear separation of concerns (GKM is independent)
- GKM can evolve independently of KServe
- Existing GKM codebase reused as-is with minimal changes
- No modifications to KServe CRDs (annotation-based integration)

#### Cons

- **Two separate control planes** to deploy, manage, and monitor (increased
  operational complexity)
- **Duplicate storage infrastructure** (PVC for models + hostPath for kernel
  caches)
- **User creates two resources** instead of one (LocalModelCache + GKMCache)
- **Additional infrastructure required**: CSI driver DaemonSet, separate
  webhooks for admission control
- **Fragmented lifecycle management**: kernel cache not automatically deleted
  when model is removed
- **Storage quota management split**: separate limits for models (in
  LocalModelNodeGroup) and kernel caches (in GKM)
- **Higher resource overhead**: additional DaemonSet, CSI driver pods, and
  operator pods
- **More complex troubleshooting**: two systems to monitor, two sets of logs,
  two failure domains

#### Rationale for Rejection:

While this approach offers clean separation, it adds significant operational
complexity for minimal benefit. Since kernel caches have a 1:1 relationship
with models in most use cases (each model has one set of kernels per GPU type),
managing them as separate resources provides little value. The unified approach
provides identical end-user benefits (30-70% faster startup) while requiring
half the infrastructure. The operational overhead of deploying and maintaining
two separate control planes, especially in enterprise environments with strict
change control, outweighs the architectural purity of separation.

---

### Alternative 2: In-Pod JIT Compilation with PVC Persistence

#### Description

Let inference frameworks compile kernels on first pod run, then persist the
compiled cache to a shared PVC for reuse by subsequent pods on the same node.

#### Approach

- Mount a shared PVC (per node or per model) at kernel cache location (e.g.,
  `/root/.cache/vllm`)
- First pod on a node compiles kernels during startup and writes to PVC
- Subsequent pods on the same node read pre-compiled kernels from PVC
- No pre-built cache images needed, no signature verification needed

#### Pros

- **Automatic cache generation** - no manual kernel cache image creation
  required
- **Always matches exact framework version** - cache generated by same
  framework version that uses it (no version mismatch)
- **Simpler deployment** - no GKM components, no kernel-cache-initializer,
  no signature verification
- **No OCI image distribution** - avoids registry requirements and image pull
  overhead

#### Cons

- **First pod always slow** - completely defeats the purpose for cold-start
  scenarios (first pod on each node takes 90s)
- **No GPU compatibility validation** - cache compiled on A100 will fail if
  pod moves to V100 node
- **Cache corruption risk** - no integrity verification; corrupted cache causes
  all subsequent pods to fail
- **Difficult to share across namespaces** - PVC is namespace-scoped, requires
  cross-namespace mounting (security risk)
- **Concurrency issues** - multiple pods starting simultaneously can corrupt
  cache (race conditions on file writes)
- **No security** - malicious pod can poison cache for all other pods using
  same PVC
- **PVC management complexity** - need one PVC per model per node, or shared
  PVC with complex locking
- **No pre-warming possible** - can't prepare caches before deploying
  InferenceServices

#### Rationale for Rejection

This approach fundamentally fails to solve the cold-start problem, which is the
primary goal of kernel caching. The first pod on each node still experiences
the full 30-120 second compilation delay. In auto-scaling scenarios where new
nodes are added frequently, or in disaster recovery where all pods restart
simultaneously, this approach provides no benefit. Additionally, the lack of
integrity verification and GPU compatibility checks makes it unsuitable for
production multi-tenant environments where security and reliability are
critical. This might work for single-tenant development clusters, but not
for the production LLM serving use cases KServe targets.

---

### Alternative 3: Embed Kernels in Container Images (Layer Caching)

#### Description

Include precompiled kernels as a layer in the inference framework container
image itself, distributed via standard container registries.

#### Approach

- Build custom vLLM/Triton/PyTorch container image with kernels pre-compiled
  into `/root/.cache/<framework>`
- Kernel cache included as layer in container image (e.g.
  , `vllm:v0.3.0-llama7b-a100`)
- No separate download needed - kernels pulled with container image
- Container runtime caches image layers automatically

#### Pros

- **Simplest approach** - no additional infrastructure beyond standard container
  distribution
- **Atomic versioning** - framework version + kernels always match (both in same
  image)
- **Standard container distribution** - uses existing registry infrastructure and
  image pull mechanisms
- **Image layer caching** - container runtimes cache layers, avoiding re-download
  on same node

#### Cons

- **Image bloat** - adding 500MB-2GB of kernels to every framework image
- **GPU-specific images required** - need separate image for A100, V100, H100,
  etc. (different compiled kernels)
- **Model-specific images required** - kernels for Llama-7B don't work for
  Mistral-7B (different model architectures)
- **Combinatorial explosion** - framework × model × GPU type = hundreds of
  images to build and maintain
  - Example: 10 models × 3 GPU types × 2 framework versions = 60 different
    images
- **Slow updates** - changing kernel cache requires rebuilding entire framework
  image (including framework code)
- **No sharing across models** - each model needs its own complete
  framework+kernel image
- **Wastes registry storage** - multiple copies of framework code for each
  model/GPU combination
- **Security updates painful** - framework CVE fix requires rebuilding all 60
  images

#### Example of Explosion

```text
vllm:v0.3.0-llama7b-a100     (vLLM + Llama-7B kernels for A100)
vllm:v0.3.0-llama7b-v100     (vLLM + Llama-7B kernels for V100)
vllm:v0.3.0-llama7b-h100     (vLLM + Llama-7B kernels for H100)
vllm:v0.3.0-mistral7b-a100   (vLLM + Mistral-7B kernels for A100)
vllm:v0.3.0-mistral7b-v100   ...
vllm:v0.3.0-mistral7b-h100
... (54 more images for 8 remaining models)

vllm:v0.3.1-llama7b-a100     (Now rebuild all 60 for new framework version)
...
```

#### Rationale for Rejection:

This approach leads to an unmaintainable proliferation of container images.
Managing hundreds of images is operationally infeasible, especially when
framework security updates require rebuilding all combinations. The unified
approach separates model weights, kernel caches, and framework code into three
independent artifacts, allowing each to be updated without affecting the
others. When vLLM releases a security patch, only the base vLLM image needs
updating, not 60 model-specific variants.

---

### Alternative 4: ConfigMap/Secret-Based Cache Distribution

#### Description

Package small kernel caches as Kubernetes ConfigMaps or Secrets and mount them
into pods using standard Kubernetes volume mechanisms.

#### Approach

- Admin compiles kernels and converts binary to base64 encoding
- Store base64-encoded cache in ConfigMap or Secret (in data field)
- Mount ConfigMap/Secret into pod via standard volume mount
- Framework uses cache from mounted directory (after decoding if needed)

#### Pros

- **Native Kubernetes primitives** - no custom controllers, no CRDs, no webhooks
- **Very simple to implement** - just ConfigMap/Secret creation and volume
  mounting
- **No additional infrastructure** - works with any Kubernetes cluster
- **Easy versioning** - ConfigMap/Secret revisions tracked automatically by
  Kubernetes
- **Namespace isolation** - ConfigMaps/Secrets respect namespace boundaries

#### Cons

- **Size limits** - ConfigMap/Secret effectively limited to ~1MB, but kernel
  caches are often 500MB-2GB
  - Kubernetes etcd has 1MB value size limit; larger objects severely degrade
    etcd performance
- **etcd storage pressure** - storing 500MB+ binary data in etcd is
  anti-pattern, causes cluster instability
- **No signature verification** - ConfigMaps/Secrets have no built-in integrity
  checking mechanism
- **No GPU compatibility validation** - no way to validate cache matches node
  GPU
- **Base64 encoding overhead** - 33% size increase for binary data when base64
  encoded
- **Update propagation delays** - ConfigMap/Secret updates can take minutes to
  propagate to pods
- **No distribution optimization** - every node pulls full data from etcd (no
  layer caching like OCI images)

#### Example of Size Problem

```text
Kernel cache size: 500MB (typical for Llama-7B on vLLM)
Base64 encoded: 667MB
etcd recommended max object size: 1MB
Result: 667× over recommended limit, will cause etcd performance degradation
```

#### Rationale for Rejection

ConfigMaps and Secrets are designed for small configuration data (API keys,
config files), not multi-gigabyte binary artifacts. Storing 500MB+ kernel
caches in etcd would cause severe cluster performance issues, slow API server
responses, and potentially make the cluster unstable. The Kubernetes
documentation explicitly warns against large ConfigMaps/Secrets. Even if
technically possible (by splitting into multiple ConfigMaps), it would be an
abuse of the API and harm cluster health. OCI images stored in registries are
the correct mechanism for distributing large binary artifacts in Kubernetes
ecosystems.

---

### Alternative 5: Distributed Cache (Redis/Memcached)

#### Description

Store compiled kernels in a distributed in-memory cache system (Redis or
Memcached), shared across all nodes in the cluster.

#### Approach

- Deploy Redis or Memcached cluster in Kubernetes
- On first kernel compilation, store kernels in cache with key = hash(model,
  framework, GPU architecture)
- Subsequent pods check cache before compiling (cache lookup by key)
- Fall back to JIT compilation on cache miss
- Cache persisted to disk (Redis with AOF/RDB) to survive restarts

#### Pros

- **Fast access** - in-memory storage provides microsecond latency
- **Distributed** - automatically shared across all nodes in cluster
- **Dynamic** - no pre-building needed, caches populate on-demand
- **Flexible** - supports multiple versions, easy cache invalidation

#### Cons

- **Additional infrastructure** - requires deploying and managing
  Redis/Memcached cluster (HA, persistence, backups)
- **Memory cost** - storing multi-GB kernel caches in RAM is extremely
  expensive
  - Example: 10 models × 500MB = 5GB RAM just for cache (multiply by
    replication factor for HA)
- **Network overhead** - downloading 500MB over network slower than reading
  from local disk
  - 1Gbps network: 500MB download = 4+ seconds
  - Local NVMe disk: 500MB read = <1 second
- **Size limits** - Redis default max value size is 512MB; requires
  reconfiguration for larger caches
- **Complexity** - need to manage cache eviction policies, cluster sizing,
  persistence configuration
- **No integrity verification** - cache can be corrupted in memory or during
  network transfer
- **Persistence overhead** - Redis RDB/AOF adds latency to writes and requires
  disk space anyway
- **Cost inefficiency** - paying for RAM to cache data that changes
  infrequently

#### Cost Comparison

For 10 models × 500MB each = 5GB cache:

```text
Redis in-memory (3x replication for HA): 15GB RAM
- AWS r6g.large (16GB RAM): ~$0.10/hour × 3 nodes = $220/month

Local disk PV (no replication, cached on each node):
- AWS gp3 EBS: 5GB × $0.08/GB-month = $0.40/month per node
- Even at 10 nodes: $4/month
```

#### Rationale for Rejection

Distributed caches are optimized for small, frequently-changing data with high
read rates (e.g., session data, API responses). Kernel caches are large
(500MB-2GB), infrequently-changing (model release cycle), and read once per
pod start. Storing them in expensive RAM when cheap local disk suffices is
economically unjustifiable. Additionally, network transfer of 500MB is slower
than local disk read, negating the "fast access" benefit. The PV/PVC approach
provides faster access (local disk), lower cost (disk vs. RAM), and simpler
operations (no cache cluster to manage).

---

### Alternative 6: Sidecar Container for Cache Management

#### Description

Run a sidecar container alongside the predictor container that downloads,
validates, and manages kernel caches, sharing storage via emptyDir or PVC.

#### Approach

- KServe webhook injects kernel cache sidecar container into InferenceService
  pods
- Sidecar downloads kernel cache OCI image, verifies signature, extracts to
  shared volume
- Shared volume (emptyDir or PVC) mounted in both sidecar and predictor
  containers
- Predictor container waits for sidecar to signal cache is ready (via file
  lock or init container pattern)

#### Pros

- **More flexible than init container** - sidecar can handle dynamic cache
  updates during pod lifetime
- **Can implement custom logic** - sidecar can retry downloads, monitor cache
  staleness, etc.
- **Doesn't require CSI driver** - uses standard volume types
- **Easier to debug** - sidecar container logs accessible via kubectl logs

#### Cons

- **Resource overhead** - extra container per pod consuming CPU and memory
  - Example: 100 pods × 100MB sidecar memory = 10GB wasted memory
    cluster-wide
- **Lifecycle complexity** - when should sidecar stop? Keep running? Exit
  after download?
- **Shared volume security concerns** - emptyDir shared between containers can
  be exploited
- **No reuse across pods** - each pod downloads cache independently (even on
  same node)
  - Node with 10 vLLM pods: downloads same 500MB cache 10 times = 5GB network
    traffic, 5GB storage
- **Slower than node-local caching** - every pod startup waits for download
  (30-60 seconds)
- **Harder to implement compatibility validation** - sidecar needs GPU access
  to validate, but sidecar doesn't need GPU for operation (complicates
  resource requests)
- **Cache not available for next pod** - if pod is deleted, cache is lost (no
  persistence with emptyDir)

#### Performance Comparison

```text
Unified approach (node-local PVC):
- First pod on node: downloads once (500MB, ~30s)
- Next 9 pods on node: instant (cache already on disk)
- Total: 30s + 0s × 9 = 30s, 500MB network

Sidecar approach (per-pod download):
- Each pod: downloads independently (500MB, ~30s)
- Total: 30s × 10 = 300s, 5000MB network
```

#### Rationale for Rejection

The sidecar pattern is designed for cross-cutting concerns that need to run
throughout the pod lifetime (e.g., logging agents, service mesh proxies).
Kernel cache download is a one-time startup operation, better suited for init
containers or DaemonSet agents. The per-pod download overhead means this
approach is 10× slower and uses 10× more network bandwidth than node-local
caching. In large clusters with hundreds of pods, this would cause significant
network congestion and slower startup times. The DaemonSet agent + PVC
approach (unified proposal) downloads once per node and shares across all pods
on that node, providing optimal performance and resource usage.

---

## Selected Approach: Unified Control Plane Integration

### Rationale

The unified control plane approach (extending KServe's LocalModelCache CRD to
include kernel caches) provides the best balance across all evaluation criteria:

1. User Experience:

- **Single resource** - Users create one `LocalModelCache` with optional
 `kernelCache` field, not two separate resources
- **Intuitive mental model** - Kernel cache is a property of a cached model,
  not a separate entity
- **Backward compatible** - Existing LocalModelCache resources work unchanged;
  `kernelCache` is optional
- **No need to learn GKM** - Users only interact with KServe APIs

2. Operational Simplicity:

- **One control plane** instead of two (GKM + KServe)
- **Reuses existing PV/PVC infrastructure** - no new storage backend, no CSI
  driver
- **Same controllers** - extends existing LocalModel and LocalModelNode
  controllers
- **Unified RBAC** - one set of permissions, not two
- **Single monitoring stack** - one set of metrics, logs, and alerts

3. Storage Efficiency:

- **Single PVC** holds both weights and kernels (unified quota management)
- **Unified storage accounting** - total = modelSize + cacheSize, validated
  against one limit
- **Automatic cleanup** - kernel cache deleted when LocalModelCache is deleted
  (same lifecycle)

4. Performance:

- **Same startup improvement** as standalone GKM (30-70% faster)
- **Lower resource overhead** - no separate DaemonSet, no CSI driver pods
- **Faster mounting** - PVC subpath mount slightly faster than CSI driver mount

5. Security:

- **Imports GKM's security model** - cosign signature verification via library
- **Leverages existing PVC security** - RBAC, Pod Security Standards apply
- **No new network attack surface** - no CSI driver gRPC endpoints to secure

6. Maintainability:

- **Single codebase** - KServe repository only (no GKM dependency)
- **Leverages proven patterns** - PV/PVC management, Job-based downloads already
  battle-tested
- **Easier testing** - no multi-system integration tests needed
- **Familiar to contributors** - extends existing code, not new system to learn

7. Cost:

- **Lower compute cost** - fewer pods (no GKM DaemonSet, no CSI driver)
- **Lower storage cost** - single PVC, not hostPath + PVC
- **Lower operational cost** - one system to maintain, monitor, and upgrade

Trade-offs Accepted:

- **Tighter coupling** - GKM functionality built into KServe instead of
  separate (acceptable: 1:1 relationship between models and kernel caches)
- **KServe-specific** - kernel caching not available to non-KServe workloads
  (acceptable: KServe is the target use case)
- **Less independent evolution** - GKM and KServe evolve together (acceptable:
  kernel caching is core to KServe's LLM serving mission)

This approach delivers production-quality GPU kernel caching with minimal
deployment complexity, making it accessible to the widest range of KServe users
while meeting all functional and performance requirements.

<!-- markdownlint-enable MD033 MD046 MD013 MD034 MD031 MD026 MD060 MD032-->
<!-- markdownlint-enable MD029 MD033 MD046 MD013 MD024 MD022 MD036-->
