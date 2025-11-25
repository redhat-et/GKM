# RFC: GPU Kernel Manager (GKM) Integration with KServe

<!-- markdownlint-disable MD033 MD046 MD013 MD034 MD031 MD026 MD060 MD032 -->
<!-- markdownlint-disable MD029 MD033 MD046 MD013 MD024 MD022-->

## <span style="color:red; font-size:2em;">Status: Draft</span>

## Objective

### Problem Statement

When serving Large Language Models (LLMs), frameworks like vLLM, PyTorch and
Triton translate high-level Python code into optimized GPU kernels. These
kernels are compiled into CUDA PTX or ROCm HASCO assembly before being
executed by the GPU driver (for example, via torch.compile). This just-in-time
(JIT) compilation occurs each time a model is loaded and can significantly
delay model startup by 30-120 seconds. KServe's existing Local Model Cache
accelerates model weight downloads but does not cache the GPU kernel binaries
generated after model load, leaving a significant startup performance gap for
GPU workloads.

### Feature/Capability

This proposal extends KServe's Local Model Cache to manage GPU kernel caches
alongside model weights using a unified control plane architecture. By
integrating GPU Kernel Manager (GKM) functionality directly into KServe's
existing CRDs and controllers, we enable users to pre-distribute validated,
architecture-specific kernel caches across nodes, reducing model warm-up
times by 30-70% while ensuring cache integrity through OCI image signing
(cosign) and GPU compatibility validation. In future iterations, this
integration will provide automatic cache warming that precompiles and
captures kernel caches when new models are cached, further accelerating
model readiness and improving the overall KServe model startup experience
across heterogeneous GPU clusters.

### Goals

1. **Reduce model startup latency by 30-70%** by providing precompiled GPU
  kernel caches ready for immediate use
2. **Unify model and kernel cache management** under a single LocalModelCache
  CRD and control plane
3. **Ensure cache integrity and security** through OCI image signing (cosign)
  and signature verification
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
2. **Replacing runtime-level kernel compilation** - Frameworks will fall back
  to JIT compilation if kernel cache is unavailable or incompatible (graceful
  degradation, no breaking changes)
3. **Directly handling GPU scheduling or resource allocation** - Handled by
  Kubernetes device plugins and schedulers
4. **Modifying inference framework code** - Integration is through standard
  environment variables and mount paths that frameworks already support
5. **Supporting non-GPU workloads** - Kernel caching only applies to
  GPU-accelerated inference; CPU-only workloads are unaffected

## Background

KServe's Local Model Cache feature accelerates startup by pre-downloading and
caching model artifacts (weights, tokenizers, configuration files) onto
node-local storage using PersistentVolumes. When an InferenceService references
a cached model, KServe mounts the PVC directly, eliminating download time and
network bandwidth usage. This significantly improves startup performance for
model loading.

However, the Local Model Cache does not address the GPU kernel compilation
overhead that occurs after model weights are loaded. For LLM inference
workloads using vLLM, Triton, or PyTorch with torch.compile, the first model
load triggers extensive JIT compilation of GPU kernels optimized for the
specific GPU architecture. This compilation can add 30-120 seconds to startup
time, during which the pod is not ready to serve requests. This overhead
occurs on every pod restart, even when model weights are already cached.

GPU Kernel Manager (GKM) is a Kubernetes-native project that manages
precompiled GPU kernel caches distributed as signed OCI images with GPU
compatibility metadata. Rather than deploying GKM as a separate control plane
with its own CRDs, agents, and CSI drivers, this proposal integrates GKM's
core capabilities—signature verification (cosign), GPU compatibility validation
(MCV library), and OCI image management—directly into KServe's existing Local
Model Cache architecture. This provides a unified experience where model
weights and GPU kernel caches are managed through the same CRDs, stored on the
same PV/PVCs, and tracked by the same controllers.

## Requirements

**(2-5 paragraphs) What are the constraints for the problem you’re trying to
solve? What are the use cases? Estimate important relevant details: bandwidth,
request rates, access patterns, data sizes, number of nodes / pods, growth
patterns, etc.Keep this succinct and focused.**

TBD:

* PVC needs to be RW and large enough to allow JIT compilation if parameters
  change.

## Design Ideas

This proposal extends KServe's existing Local Model Cache architecture with GPU
kernel cache management capabilities by adding an optional `kernelCache` field
to the LocalModelCache CRD. Model weights and GPU kernel caches are stored on
the same PersistentVolume in separate subdirectories (`/mnt/models/models/`
and `/mnt/models/kernel-caches/`), managed by the same controllers, and mounted
via the same PVC.

The workload pod (i.e. vLLM pod) starts with the PVC mounted (existing behavior)
containing both model weights and kernel cache. The inference framework (vLLM,
Triton, PyTorch) detects the environment variable and uses the precompiled
kernels instead of JIT compiling, reducing startup time by 30-70%.

If the optional `kernelCache` field in the LocalModelCache CRD is not provided,
or a change is detected which would normal require an additional JIT
compilation, then the inference framework works as before and performs the JIT
compilation.

## Alternatives Considered

### Alternative 1: Separate GKM Control Plane with CSI Driver

Deploy GKM as a standalone system alongside KServe with its own CRDs
(`GKMCache`, `GKMCacheNode`), controllers (Operator), DaemonSet agent, and CSI
driver for volume mounting. User creates both `LocalModelCache` (for model
weights) and `GKMCache` (for kernel caches).

While this approach offers clean separation, it adds significant operational
complexity for minimal benefit. Since kernel caches have a 1:1 relationship
with models in most use cases (each model has one set of kernels per GPU type),
managing them as separate resources provides little value. The unified approach
provides identical end-user benefits (30-70% faster startup) while requiring
half the infrastructure. The operational overhead of deploying and maintaining
two separate control planes, especially in enterprise environments with strict
change control, outweighs the architectural purity of separation.
