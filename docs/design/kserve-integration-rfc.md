# RFC: GPU Kernel Manager (GKM) Integration with KServe

<!-- markdownlint-disable MD033 MD046 MD013 MD034 MD031 MD026 MD060 MD032 -->
<!-- markdownlint-disable MD029 MD033 MD046 MD013 MD024 MD022-->

## <span style="color:red; font-size:2em;">Status: Draft</span>

## Objective

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
core capabilities: Kernel extraction, GPU compatibility validation
(MCV library), and trusted image verification (cosign + kyverno) into KServe's
existing Local Model Cache architecture. This provides a unified experience
where model weights and GPU kernel caches are managed through the same CRDs,
stored on the same PV/PVCs, and tracked by the same controllers.

## Requirements

For GKM to function properly in KServe, the following requirements must be met:

* A GPU Kernel Cache must be packaged in an OCI Image and pushed to a registry
  accessible by the Kubernetes Cluster.
  * The OCI Image must be manually created and pushed using
    [MCV](https://github.com/redhat-et/MCU/tree/main/mcv) today. In future
    releases, the plan is to auto-detect when GPU Kernel Cache is built or
    rebuilt and automatically create and push an OCI Image.
* URL of the OCI Image must be placed in the KServe LocalModelCache CRD.
  * If OCI Image is not provided in a KServe LocalModelCache CRD, code will
    function as it does today and JIT compilation will occur at startup. Code
    will still function, it will just take longer to startup.
* When the PVC that contains the GPU Kernel Cache is mounted in a pod, it needs
  to be mounted in a directory in the pod such that if parameters change in the
  pod and a new JIT compilation is required, which is plausible, then the new
  JIT output should not be in the PVC memory space.

## Design Ideas

This proposal extends KServe's existing Local Model Cache architecture with GPU
kernel cache management capabilities by adding an optional `kernelCache` field
to the LocalModelCache CRD. The `kernelCache` field is small and contains a URL
to an OCI Image.

### Image Signature Verification

To ensure the integrity and authenticity of kernel cache images, we leverage
[Kyverno](https://kyverno.io/), a Kubernetes-native policy engine that
integrates with [Sigstore's Cosign](https://docs.sigstore.dev/cosign/overview/)
to verify container image signatures. By defining `verifyImages` rules in
Kyverno ClusterPolicies, we enforce that only kernel cache images signed with
authorized keys or certificates are permitted to be pulled and cached on
cluster nodes, automatically rejecting unsigned or invalidly-signed images at
admission time.

When Kyverno is enabled (recommended for production), it verifies the image
signature and adds a verification annotation to the LocalModelCache CR. The
KServe webhook then parses this annotation to extract the verified image digest
and creates a standardized `serving.kserve.io/resolvedDigest` annotation. This
ensures immutability and provides a cryptographic link to the signed image.

When Kyverno is disabled (development/testing only), the webhook performs
direct OCI registry resolution to convert image tags to digests. This ensures
immutability but not authenticity.

### Storage and Mounting

Model weights and GPU kernel caches are stored on the same PersistentVolume in
separate subdirectories (`/mnt/models/models/` and
`/mnt/models/kernel-caches/`), managed by the same controllers, and mounted
via the same PVC. The workload pod (i.e. vLLM pod) starts with the PVC mounted
(existing behavior) containing both model weights and kernel cache. The
inference framework (vLLM, Triton, PyTorch) detects the precompiled kernels and
uses them instead of JIT compiling, reducing startup time by 30-70%.

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
