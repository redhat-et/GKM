# GPU Kernel Manager Operator, Agent and CSI Plugin Design

## Introduction

The GPU Kernel Manager (GKM) Operator, Agent and CSI Plugin manage GPU kernel
cache distribution and usage within a Kubernetes environment.
The Operator validates and inspects GPU kernel images, while the Agent
handles Kernel cache extraction from OCI Images, and the CSI Plugin handles
volume mounting the kernel images in pods.
The architecture follows a controller-runtime approach, leveraging Kubernetes
Custom Resources (CRs) and a CSI plugin to manage kernel images efficiently.

## Motivation

The primary motivation of the GPU Kernel Manager Operator, Agent and CSI
Plugin is to reduce the startup time of large language models (LLMs) that use
GPU Kernels.
By providing a pre-tuned kernel cache as a directory in a container that can
be consumed by Triton or vLLM at runtime, we aim to optimize model loading,
performance and reduce latency in containerized workloads.

Triton Cache Vault (TCV) is a utility developed under
[TKDK](https://github.com/redhat-et/TKDK).
TCV packages Triton Kernel Caches and vLLM caches into OCI-compliant container
images.
Applications can leverage TCV to package up their caches into container images
and use [cosign](https://github.com/sigstore/cosign) to sign the images.
GKM will then use Kubernetes to distribute the cache images to workloads on
various nodes in a given cluster and use TCV to extract the caches from the
images. Using GKM, TCV, cosign and Kubernetes to manage and distribute kernel
images to containerized workloads ensures their validity before usage in
containers and is crucial for performance, optimization and security.

The GKM Operator focuses on:

- Verifying kernel cache image signatures using cosign.
- Aggregating node-level status of each kernel cache.
- Supporting both cluster and namespace-scoped CRDs to improve security and
  flexibility

The GKM Agent focuses on:

- Detecting GPU hardware and driver versions on each node.
- Pulling and extracting the kernel cache container images.
- Validating cache compatibility against the node's hardware (per GPU).
- Reporting status to the control plane via node-specific CRs.
- Tracking the status of all kernel caches for all GPUs on the node (via TCM).

The CSI Driver focuses on:

- Mounting validated kernel caches into pods.
- Does not directly access the Kubernetes API.

This clear separation of concerns ensures that the CSI plugin does not perform
image validation, while the operator remains focused on image inspection and
verification.

## Goals

- Decouple image validation from kernel cache mounting.
- Provide efficient GPU-kernel compatibility tracking.
- Enable accurate kernel usage reporting via Agent.
- Avoid CSI access to Kubernetes API.

## Architecture

### Components

```bash
                 ┌────────────────────────────────────────────┐
                 │ Control Plane                              │
                 │                                            │
                 │     ┌────────────────────────────────┐     │
                 │     │ GKMCache (CR)                  │     │
                 │     │ - ociImage                     │     │
                 │     │ - Load Status Summary          │     │
                 │     └───────────────┬────────────────┘     │
                 │                     │                      │
                 │                     ▼                      │
                 │  ┌──────────────────────────────────────┐  │
                 │  │ Operator/controller (Deployment)     │  │
                 │  │ - Runs on control plane              │  │
                 │  │ - Registers CSI Driver               │  │
                 │  │ - Launches GKM Agent                 │  │
                 │  │ - Validate image Signature           │  │
                 │  │ - Tracks overall status across       │  │
                 │  │   all nodes in GKMCacheNode          │  │
                 │  └──────────────────┬───────────────────┘  │
                 │                     │                      │
                 └─────────────────────┼──────────────────────┘
                                       │
                 ┌─────────────────────┴──────────────────────┐
                 │ Worker Node                                │
                 │                                            │
                 │     ┌────────────────────────────────┐     │
                 │     │ GKMCacheNode (CR)              │     │
                 │     │ - GPU Info (per-GPU)           │     │
                 │     │ - Node Load Status (per-GPU)   │     │
                 │     └───────────────┬────────────────┘     │
                 │                     │                      │
                 │                     ▼                      │
                 │  ┌──────────────────────────────────────┐  │
                 │  │ GKM Agent (DaemonSet)                │  │
                 │  │ - Detects GPUs and drivers           │  │
                 │  │ - Validates cache compatibility      │  │
                 │  │ - Create/Update node-specific CR     │  │
                 │  │ - Collects usage from CSI driver     │  │
                 │  └──────────────────┬───────────────────┘  │
                 │                     │                      │
                 │                     ▼                      │
                 │  ┌──────────────────────────────────────┐  │
                 │  │ CSI Driver (DaemonSet)               │  │
                 │  │ - Loads kernel cache into volume     │  │
                 │  │   if extracted by Agent              │  │
                 │  │ - Updates kernel cache usage file    │  │
                 │  └──────────────────────────────────────┘  │
                 └────────────────────────────────────────────┘
```

#### Control Plane Components

- **GKM Operator/Controller (Control Plane):** Validates kernel cache images,
  inspects metadata, and updates CR status. Manages both cluster and
  namespace-scoped CRDs.
  Runs as a long-lived controller on the control plane.

#### Worker Node Components

- **GKM Agent (Node-local Daemon):** Discovers GPU hardware and driver versions,
  verifies kernel cache compatibility, updates node-specific CRs, and reports
  status to the control plane.
  Runs as a DaemonSet on each worker node.

- **GKM CSI Driver (Node-local Daemon):** Mounts the validated kernel cache onto
  the pod's volume if marked as `Ready` and `Compatible` on the node.
  Runs as a DaemonSet on each worker node.

### Custom Resource Definitions (CRDs)

GKM will support the following CRDs:

- **GKMCache CRD (namespaced):**
  Declares that workloads in a specific namespace intend to use a GPU kernel
  cache resource defined by an OCI image. This is a lightweight reference to
  a kernel cache image. The actual validation, extraction, and usage tracking
  are handled by the GKM Operator, Agent and CSI driver. This CRD supports
  multi-tenancy by scoping kernel cache declarations to specific namespaces.

- **ClusterGKMCache CRD:**
  Same as GKMCache, but used when the kernel resource is intended for
  workloads across the entire cluster. Suitable for shared or system-wide
  kernel caches.

- **GKMCacheNode CRD (namespaced):**
  A GKMCacheNode resource is created by the Agent to reflect
  compatibility and readiness of kernel caches for each GPU on the node.
  A GKMCacheNode instance is created for each node for each GKMCache instance.

- **ClusterGKMCacheNode CRD:**
  Same as GKMCacheNode, but used when the corresponding kernel
  cache is defined using a ClusterGKMCache resource
  A ClusterGKMCacheNode instance is created for each node for each
  ClusterGKMCache instance.

To increase security, the GKM Operator supports a namespace-scoped
version of the GKMCache CRD.
Namespace-scoped CRDs improve security and flexibility by allowing
administrators to limit kernel cache usage to designated namespaces.
This is particularly useful in multi-tenant Kubernetes clusters where
different applications may require distinct Kernel configurations.
This enables the restriction of kernel cache loading and mounting
to specific namespaces, thereby enhancing isolation between workloads.

Advantages:

- Improved security through namespace isolation.
- Clear separation of kernel cache resources between tenants.
- Simplified CRD structure by merging cache and metadata.

#### GKMCache and ClusterGKMCache CRD

The GKMCache and ClusterGKMCache CRDs serve as declarations
of interest in a specific GPU kernel cache, represented by an OCI image.
These resources inform the GKM system that workloads in the cluster may require
access to the specified kernel cache.

Users or application operators populate the image field, which points to a valid
OCI image containing the precompiled kernel cache.
Once specified, the operator resolves the image to its digest (e.g.,
sha256:...). This digest acts as the authoritative identifier throughout the
system for validation, compatibility checks, cache extraction, and mounting.

This image is pulled by the GKM Agent as needed, and validated against the GPUs
installed on given nodes.
The actual management of image signatures, pull secrets, and validation policies
is handled globally via GKM configuration (e.g., ConfigMap), not per resource.

GPU compatibility is assessed dynamically by the GKM Agent on a per-node,
per-GPU basis. The CRD itself does not include any GPU-specific configuration.

Example of GKMCache CRD:

```yaml
apiVersion: gkm.io/v1alpha1
kind: GKMCache
metadata:
  name: cache-vllm-llama2
  namespace: ml-apps
spec:
  image: quay.io/example/cache-vllm-llama2:latest
status:
  resolvedDigest: sha256:abc123deadbeef456789...
  conditions:
    - type: Verified
      status: "True"
      reason: CosignSuccess
      message: "Image signature verified and digest resolved."
      lastTransitionTime: "2025-06-03T14:52:00Z"
    - type: Error
      status: "True"
      reason: NodeFailuresPresent
      message: "One or more nodes reported errors. See failedNodeConditions."
      lastTransitionTime: "2025-06-12T13:50:00Z"
  totalNodes: 10
  readyNodes: 8
  failedNodes: 2
  failedNodeConditions:
    ArchitectureMismatch:
      - node-a100x16
      - node-a100x8
  lastUpdated: "2025-06-12T14:00:00Z"
```

#### GKMCacheNode and ClusterGKMCacheNode CRDs

GKMCacheNode and ClusterGKMCacheNode CR instances are created by the GKM Agent,
not the user.
Each node reports status per kernel cache via one CR per node.
This consolidates status for all relevant caches on that node.
The CR includes labels and annotations to support efficient filtering and
introspection.

If the corresponding GKMCache is namespace-scoped, the GKMCacheNode CR
will live in the same namespace.
If the corresponding ClusterGKMCache is cluster-scoped, a cluster-scoped
ClusterGKMCacheNode CR will be used instead.

While nodes themselves are not namespaced, the namespace of the GKMCacheNode CR
follows the scope of the kernel cache resource it reports on. This allows the
operator to correctly associate status objects with their source cache
definitions.

This structure enables more efficient status tracking in environments with
heterogeneous GPU configurations and supports CSI plugin queries via the GKM
Agent.

Summary of data reflected in the CRD:

Labels:

- `gkm.node=<node-name>`: Helps filter status CRs by node

Annotations:

- gkm.io/lastUpdated: ISO8601 timestamp of the last time this CR was
  updated by the Agent.
- gkm.io/currentCaches: (Optional) Summary of cache states on the node,
  potentially used for indexing/debugging.

Spec Fields:

- nodeName: The name of the Kubernetes node this CR represents.

Status Fields:

- gpus: A list describing each physical GPU on the node. Each entry
  includes:

  - ids: GPU indices (e.g., [0, 1, 2, 3])
  - gpuType: GPU model (e.g., nvidia-a100)
  - driverVersion: Installed driver version

- caches: A map of kernel cache identifiers (e.g., cache-vllm-llama2)
  to their status. Each cache entry includes:

  - digest: Resolved OCI digest of the cache image.
  - compatibleGPUs: List of GPU sets where the cache is compatible.
  - incompatibleGPUs: List of GPU sets where the cache is incompatible,
    with structured reason and message fields.
  - lastUpdated: Last timestamp this entry was refreshed.

This consolidated per-node, per-GPU view supports scalable monitoring and
allows the CSI driver to consult the Agent instead of accessing the Kubernetes
API directly.

Example of GKMCacheNode CRD:

```yaml
apiVersion: gkm.io/v1alpha1
kind: GKMCacheNode
metadata:
  name: node-a100x8
  namespace: ml-apps
  labels:
    gkm.node: node-a100x8
spec:
  nodeName: node-a100x8
status:
  gpus:
    - gpuType: nvidia-a100
      driverVersion: 535.43.02
      ids: [0, 1, 2, 3, 4, 5, 6, 7]
  caches:
    cache-vllm-llama2:
      digest: sha256:abc123...
      compatibleGPUs:
        - ids: [0, 1, 2, 3, 4, 5, 6, 7]
      incompatibleGPUs: []
      lastUpdated: "2025-06-03T15:12:00Z"
    cache-vllm-mixtral:
      digest: sha256:def456...
      compatibleGPUs:
        - ids: [0, 1, 2, 3, 4, 5, 6, 7]
      incompatibleGPUs: []
      lastUpdated: "2025-06-03T15:13:00Z"
    cache-vllm-gpt4:
      digest: sha256:789xyz...
      compatibleGPUs: []
      incompatibleGPUs:
        - ids: [0, 1, 2, 3, 4, 5, 6, 7]
          reason: "Architecture Mismatch"
          message: "Kernel built for Hopper architecture (SM 8.9)"
      lastUpdated: "2025-06-03T15:14:00Z"
```

## Interaction

Below is a rough flow when using GKM:

- User or application creates a GKMCache CR specifying the kernel image.
- Webhook verifies that the image is signed and not tampered with.
- GKM Agent on each node:
  - Creates a GKMCacheNode CR for its Node.
  - Extracts the kernel cache from the OCI Image referenced in the GKMCache CR
    to local host in known directory.
  - Collects GPU information, verifies kernel cache compatibility, and updates
    the status in the GKMCacheNode CR.
- User or application creates a Pod referencing GKMCache CR in the Volume
  Mounts section..
- Kubelet call CSI Driver when pod is schedule on Node.
- CSI Driver on each node:
  - Searches host in known directory for subdirectory with GKMCache CR name and
    namespace parent directories.
  - If found, mounts the directory in Pod.
  - Updates pod usage data in files on host.
- Operator monitors that state of each GKMCacheNode CR and updates
  the status of the GKMCache CR.

An example of the flow is shown below:

```sh
               +------------------------+
               | User creates GPU       |
               | Kernel Cache (CR)      |
               +----------+-------------+
                           |
                           v
               +-----------+------------+
               | Webhook verifies       |
               | image signature        |
               +-----------+------------+
                           |
            +--------------+----------------+
            |                               |
   +--------v--------+            +---------v---------+
   | Signature valid |            | Signature invalid |
   +--------+--------+            +---------+---------+
            |                               |
            v                               v
+-----------+-----------+        +----------+----------+
| Mark CR as "Verified" |        | CR is not created   |
+-----------+-----------+        +----------+----------+
            |
            v
+-----------+-----------+
| Agent runs preflight  |
| checks using image    |+---------------------------+
| metadata              |                            |
+-----------+-----------+                            |
            |                                        |
            v                                        v
+-----------+------------+               +-----------+-----------+
| Preflight check passes |               | Preflight check fails |
+-----------+------------+               +-----------------------+
            |                                        |
            v                                        v
+-----------+-----------+                +-----------+-----------+
| Extract kernel cache  |                | Mark CR as "Failed"   |
| from image to host    |                | with error details    |
|                       |                +-----------------------+
| Mark CR as "Ready"    |
| and "Compatible"      |
+-----------+-----------+
            |
            v
+-----------+-----------+
| Pod requests volume   |
| from CSI driver via   |
| CR name and namespace |
+-----------+-----------+
            |
            v
+-----------+-----------+
| CSI Driver searches   |
| host for CR directory |+---------------------------+
+-----------------------+                            |
            |                                        |
            v                                        v
+-----------+-----------+               +------------+-------------+
| Found, CSI Driver     |               | Not Found, CSI Driver    |
| mounts cache in pod   |               | returns error to request |
| and updates usage     |               +--------------------------+
+-----------------------+
```

### Kernel Cache Extraction and CSI Mounting Behavior

The GKM Operator will make sure the OCI Image was properly signed using cosign
and the image provided is valid and has not been tampered with.
This will be done with a mutating webhook, so if the image is invalid, the
GKMCache (or ClusterGKMCache) CR will not be allowed to be created.

Once it is created, the GKM Agent will download the OCI Image and extract the
kernel cache from the image into a temporary directory on host.
From here, the GKM Agent will verify the kernel cache is compatible with the
GPU and associated drivers installed on the host.
The compatibility state will be written to the GKMCacheNode CR.
If incompatible, the kernel cache directory will be deleted.
If compatible, the kernel cache will be move to the default directory:

```console
/var/lib/gkm/caches/<namespace>/<cr-name>/
```

If the CR is cluster scoped (ClusterGKMCache), the `<namespace>` will be a
fixed string like `cluster-scoped`.
Note that this directory is not removed on server reboots so the extract cache
is preserved.
It will be up to the GKM Agent to remove stale data on power-up.

When a pod needing the kernel cache is scheduled (see
[Example Pod Spec Volume Request](#example-pod-spec-volume-request)
below for example yaml), Kubelet will call the registered CSI Driver with the
`volumeAttributes` from the pod spec, which includes the name of the CR and its
namespace. The CSI driver will look in the known directory for a kernel cache
directory with that name and namespace.
If it exists, it will mount it in the pod.

By default, the CSI driver mounts this cache directory as read-only into the
requesting pod to maintain kernel integrity and enable safe sharing between
pods. Applications requiring write access must opt-in by explicitly setting the
`readOnly: false` flag in the volumeAttributes section of the pod spec.

#### Example Pod Spec Volume Request

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: example-pod
  namespace: ml-apps
spec:
  containers:
    - name: app-container
      image: example/image:latest
      volumeMounts:
        - mountPath: /models/cache
          name: kernel-cache
  volumes:
    - name: kernel-cache
      csi:
        driver: csi.gkm.io
        volumeAttributes:
          cacheName: kernel-x
          cacheNamespace: ml-apps
          readOnly: "false"
```

### Communication Between GKM Agent and CSI Driver

In the initial implementation, the CSI Driver will store data in files
on host in place of using a local database.
It is not expected that there is much data that needs to be stored.
After the initial PoC, if additional data needs to be stored, or data
contention issues arise, a proper database will be leveraged.

The CSI Driver needs a `VolumeId` mapping to the CR name and namespace.
The `VolumeId` is provided by Kubelet in the initial `NodePublishVolume()`
call, which requests the volume to be mounted.
This call also includes the `volumeAttributes`, which contain the CR name
and namespace.
Subsequent Kubelet calls (`NodeGetVolumeStats()` and `NodeUnpublishVolume()`)
only include the `VolumeId`, so the CSI driver needs the mapping to the CR name
and namespace.

To help the user with introspection and debugging, the GKM Agent will provide
usage data in the GKMCacheNode CR.
This data is only known by the CSI Driver, so the CSI Driver will also store
usage data in the files.
The GKM Agent will have Read-only access to the files, which it will
periodically read.

When a kernel cache is mounted in a pod, CSI Driver will record pod data in a
file in a mirror directory for GKM Agent to pull usage data from.
Note that this directory is removed on server reboots so data is cleaned up.

```console
/run/gkm/usage/<namespace>/<cr-name>/usage.json
```

Example content of usage.json:

<!-- markdownlint-disable  MD013 -->
<!-- Temporarily disable MD013 - Line length to keep the block formatting  -->
```console
{
  "pod-foo": [
    {
      "podName": "pod-foo",
      "podNamespace": "ml-apps",
      "volumeId": "csi-a1427cb0aa8fc7c1429eaa38c2f4b371d823172e1d74af6c089e26bf9ca39e8c",
      "volumeSize": 80608,
      "targetPath": "/var/lib/kubelet/pods/f009a3d0-0a2e-46d8-bc0a-4a3cc139338d/volumes/kubernetes.io~csi/kernel-volume/mount"
      "startTime": "2025-07-08T12:40:00Z",
    }
  ],
  "pod-bar": [...]
}
```
<!-- markdownlint-enable  MD013 -->

## Design Considerations

### Separate Resources for Kernel Metadata and Cache

Instead of managing separate resources for kernel metadata and cache, the GKM
operator will use a unified GKMCache resource. This avoids redundancy
since the kernel cache and its metadata are tightly coupled in Triton-lang.
This single resource will hold both cache and metadata information, simplifying
management and reducing potential conflicts.

### Pros and Cons of Using a NodeStatus CRD

A couple of Operators use the NodeStatus pattern of creating a Node specific
CRD to track the status of a higher level CRD for a given Kubernetes Node.
In particular,
[bpfman Operator](https://operatorhub.io/operator/bpfman-operator)
[Security Profiles Operator](https://operatorhub.io/operator/security-profiles-operator)
and Ingress Node Firewall Operator.
Below are some Pros and Cons for using this pattern.

#### Pros

One of the reasons for using this pattern is that for a given CRD, work has to
be done on every node (or a large subset of nodes) and because of potential
hardware differences between nodes, the action may succeed on some nodes and
fail on others. For large clusters with 100+ nodes, tracking success/failure,
error message and small of amount of metadata for 100+ nodes in the status of
one CRD get messy and hard for the user to consume.
In addition, 100+ agents writing their status to a given CRD instance may not
scale well.

By keeping an overall status in the higher level CRD, with `Success` if all
nodes succeeded and `Failure` if one or more nodes had a failure, and a list of
nodes with failures, more detailed errors as well additional node metadata can
be kept in Node specific CRD.

#### Cons

One of the major drawbacks to using this pattern is that it is not very
Kubernetes like.
The user creates the higher level CRD, but then has to get any failure details
from the Node specific CRD.

To address the issue of scale,
[Server Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/)
may be the solution.
This needs to be investigated.

### State Management

For the initial implementation, the CSI Driver will store some pod metadata in
a file on the host for the GKM Agent to poll.

To ensure resilience and consistent state management, future enhancements may
include utilizing a lightweight embedded database (such as Sled/SQLite/BoltDB)
to maintain the current state of the kernel cache images. This allows the
operator to recover seamlessly from failures or restarts without losing track
of kernel cache image validation and metadata status.

The database will be used to store:

- Kernel image metadata
- Validation status and signature checks
- Last known good state

This database will be synchronized with the Kubernetes API state to ensure
consistency between the operator's in-memory data and the persistent storage.

## Open Questions

- Should validation be enforced strictly, or allow fallback for unverified
  images?
  - Global configuration knob, `allow-unsigned-images` and `verify-enabled`?
- How to handle image updates during runtime?
- Does GKM have to manage access to GPU? Can 20 different pods all load their
  kernels simultaneously? Use:
  [extended-resource-node](https://kubernetes.io/docs/tasks/administer-cluster/extended-resource-node/)

## Alternatives Considered

- Running the controller as a short-lived process (daemonless). While this
  approach would reduce resource consumption when idle, it poses a challenge
  in responding promptly to kernel cache image validation and updates.
  Additionally, frequent start-stop cycles can increase latency during critical
  operations.

- Keeping all CRDs cluster-scoped. While simpler to manage and deploy, this
  approach lacks namespace isolation, making it harder to enforce security
  boundaries between different workloads.

## Future Work

- **Add metrics for kernel cache usage:**
  Introduce Kernel metrics from GKM, including per-pod and per-node cache
  hit/miss ratios, extraction times, and compatibility failures, and Kernel
  usage.

- **Improve signature validation with additional cosign policy support:**
  Add support for configurable cosign policies such as keyless verification,
  transparency logs, and support for multiple signers or trusted keys to
  enhance supply chain security.

- **Introduce Just-In-Time (JIT) Kernel Cache Mode:**
  To avoid the overhead and complexity of precompiling and distributing
  kernel images for every possible GPU and driver combination, GKM will support
  a **JIT Kernel Cache Mode**. In this mode:

  - When a pod requiring a kernel cache is scheduled, and no prebuilt cache
    exists for the specific hardware (GPU model, driver version), GKM will
    initiate an **on-cluster compilation** of the necessary kernel.
  - This compilation can be triggered by the pod itself (when the model runs).
  - Once the cache is compiled and tuned for the specific hardware:
    - The GKM Agent will **sign** the generated cache contents and **package**
      the cache using TCV into a compliant OCI container image. It will also
      **sign** the container image.
    - The image will be **pushed automatically** to a configurable container
      registry (e.g., Quay, Harbor, or any OCI-compliant registry).
    - The GKM Operator will **label** and **register** this image in a
      corresponding GKMCache or ClusterGKMCache CR, including compatibility
      metadata.
    - Subsequent workloads on similar hardware can reuse the newly created
      kernel image, avoiding recompilation.

  **Benefits:**

  - **One-time cost per hardware model**: Kernel cache compilation is performed
    once per unique GPU/driver configuration in the cluster.
  - **Reduced image sprawl**: Only kernel caches actually needed by running
    workloads are stored and distributed.
  - **Faster time-to-first-run**: Pods with compatible hardware benefit from
    prebuilt images automatically in subsequent launches.
  - **Scalable optimization**: New nodes or GPUs introduced into the cluster
    will generate their own caches once, then use them persistently.

  **Default Mode Behavior:**

  This JIT Kernel Cache Mode is expected to be the **default operating mode**,
  with an option to disable it for air-gapped or security-sensitive
  environments where all images must be pre-validated and controlled
  externally.

  **Configuration Considerations:**

  - The push location (target registry) will be configurable via GKM ConfigMap
    or environment variable.
  - Signing policies for JIT-generated images can be enforced using internal
    cosign keys or keyless signing workflows.
  - An optional retention policy can be configured to garbage-collect unused
    JIT kernel caches after a specified TTL or number of image versions.
