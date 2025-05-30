# Triton Kernel Manager Operator and CSI Plugin Design

## Introduction

The Triton Kernel Manager (TKM) Operator and CSI Plugin manage Triton kernel
distribution and usage within a Kubernetes environment. The operator validates
and inspects Triton kernel images, while the CSI plugin handles Triton Kernel
cache extraction and volume mounting. The architecture follows a
controller-runtime approach, leveraging Kubernetes Custom Resources (CRs) and
a CSI plugin to manage kernel images efficiently.

## Motivation

The primary motivation of the Triton Kernel Manager Operator, Agent and CSI
Plugin is to reduce the startup time of large language models (LLMs) that use
Triton Kernels. By providing a pre-tuned kernel cache as a directory that can
be consumed by Triton-lang at runtime, we aim to optimize model loading
performance and reduce latency. Additionally, managing kernel images and
ensuring their validity before usage in containers is crucial for performance
optimization and security.

The TKM Operator focuses on:

- Validating Triton kernel cache image signatures.
- Aggregating node-level status of each kernel cache.
- Supporting both cluster and namespace-scoped CRDs to improve security and
  flexibility

The TKM Agent focuses on:

- Detecting GPU hardware and driver versions on each node.
- Validating cache compatibility against the node's hardware (per GPU).
- Reporting status to the control plane via node-specific CRs.
- Tracking the status of all kernel caches for all GPUs on the node (via TCM).
- Communicates with the CSI plugin via GRPC.

The CSI Plugin focuses on:

- Facilitating seamless cache extraction and mounting via a CSI plugin
- Validating cache compatibility by consulting the TKM Agent via gRPC before
  extraction.
- Does not directly access the Kubernetes API.

This clear separation of concerns ensures that the CSI plugin does not perform
image validation, while the operator remains focused on image inspection and
verification.

## Goals

- Decouple kernel image validation from mounting
- Integrate with TCV for cache image inspection and validation
- Provide efficient per-GPU kernel compatibility tracking.
- Provide both cluster and namespace-scoped CRDs
- Maintain the controller as a long-running daemon for consistent state
  management and responsiveness

## Architecture

### Components

```bash
                 ┌─────────────────────────────────────────────────┐
                 │ Control Plane                                   │
                 │                                                 │
                 │ ┌───────────────────────────────────┐           │
                 │ │ TritonKernelCache (CR)            │           │
                 │ │ - ociImage                        │           │
                 │ │ - Load Status Summary             │           │
                 │ └─────────────────┬─────────────────┘           │
                 │                   │                             │
                 │                   ▼                             │
                 │ ┌─────────────────────────────────────────────┐ │
                 │ │ Operator/controller (Deployment)            │ │
                 │ │ - Runs on control plane                     │ │
                 │ │ - Registers CSI Driver                      │ │
                 │ │ - Launches TKM Agent                        │ │
                 │ │ - Tracks overall status across              │ │
                 │ │   all nodes in TritonKernelCacheNodeStatus  │ │
                 │ └─────────────────┬───────────────────────────┘ │
                 │                   │                             │
                 └───────────────────┼─────────────────────────────┘
                                     │
                 ┌───────────────────┴───────────────────┐
                 │ Worker Node                           │
                 │                                       │
                 │ ┌───────────────────────────────────┐ │
                 │ │ TritonKernelCacheNodeStatus (CR)  │ │
                 │ │ - GPU Info (per-GPU)              │ │
                 │ │ - Node Load Status (per-GPU)      │ │
                 │ └─────────────────┬─────────────────┘ │
                 │                   │                   │
                 │                   ▼                   │
                 │ ┌───────────────────────────────────┐ │
                 │ │ TKM Agent (DaemonSet)             │ │
                 │ │ - Detects GPUs and drivers        │ │
                 │ │ - Validate Image Signature        │ │
                 │ │ - Validates cache compatibility   │ │
                 │ │ - Create/Update node-specific CR  │ │
                 │ │ - Serves gRPC to CSI plugin       │ │
                 │ └─────────────────┬─────────────────┘ │
                 │                   │                   │
                 │                   ▼                   │
                 │ ┌───────────────────────────────────┐ │
                 │ │ CSI Driver (DaemonSet)            │ │
                 │ │ - Watches pod volumes             │ │
                 │ │ - Requests gRPC validation        │ │
                 │ │ - Loads kernel cache into volume  │ │
                 │ │   if approved by Agent            │ │
                 │ └───────────────────────────────────┘ │
                 └───────────────────────────────────────┘
```

#### Control Plane Components

- TKM Operator/Controller (Control Plane): Validates Triton kernel images,
  inspects metadata, and updates CR status. Manages both cluster and
  namespace-scoped CRDs.
  Runs as a long-lived controller on the control plane.

#### Worker Node Components

- TKM Agent (Node-local Daemon): Discovers GPU hardware and driver versions,
  verifies kernel cache compatibility, updates node-specific CRs, and reports
  status to the control plane. Runs as a DaemonSet on each worker node.

- TKM CSI Driver (Node-local Daemon): Mounts the validated kernel cache onto
  the pod's volume if marked as `Ready` and `Compatible` on the node.
  Runs as a DaemonSet on each worker node.

### Custom Resource Definitions (CRDs)

TKM will support the following CRDs:

- **TritonKernelCache CRD (namespaced):**
  Declares that workloads in a specific namespace intend to use a Triton GPU
  kernel resource defined by an OCI image. This is a lightweight reference to
  a kernel cache image — the actual validation, extraction, and usage tracking
  are handled by the TKM Agent and CSI driver. This CRD supports multi-tenancy
  by scoping kernel cache declarations to specific namespaces.

  > *[OI] Possible Naming Options (prefer a shorter name):
  > TritonKernelCache/TKMCache/TKMImage/TKMCacheImage/TKMKernelImage*
- **TritonKernelCacheCluster CRD:**
  Same as TritonKernelCache, but used when the kernel resource is intended for
  workloads across the entire cluster. Suitable for shared or system-wide kernel
  caches.
- **TritonKernelCacheNodeStatus CRD (namespaced):**
  A TritonKernelCacheNodeStatus resource is created by the Agent to reflect
  compatibility and readiness of kernel caches for each GPU on the node.
- **TritonKernelCacheNodeStatusCluster CRD:**
  Same as TritonKernelCacheNodeStatus, but used when the corresponding kernel
  cache is defined using a TritonKernelCacheCluster resource

To increase security, the TKM Operator supports a namespace-scoped
version of the TritonKernelCache CRD.
Namespace-scoped CRDs improve security and flexibility by allowing
administrators to limit Triton kernel usage to designated namespaces.
This is particularly useful in multi-tenant Kubernetes clusters where
different applications may require distinct Triton Kernel configurations.
This enables the restriction of Triton kernel cache loading and mounting
to specific namespaces, thereby enhancing isolation between workloads.

Advantages:

- Improved security through namespace isolation.
- Clear separation of kernel cache resources between tenants.
- Simplified CRD structure by merging cache and metadata.

> *[OI] Does Namespace Scoped CRD make sense? We cannot isolate the actual
> GPU to a namespace.*
> The namespaced CRDs act as a declaration of dependency: This pod needs
> access to a kernel cache that is compatible with the GPU it will be scheduled on.
> So Namespaced CRDs ensure that a pod can only request a kernel declared in its own
> namespace.

#### TritonKernelCache and TritonKernelCacheCluster CRD

The TritonKernelCache and TritonKernelCacheCluster CRDs serve as declarations
of interest in a specific Triton GPU kernel cache, represented by an OCI image.
These resources inform the TKM system that workloads in the cluster may require
access to the specified kernel cache.

Users provide the cacheImage field, which points to a valid OCI image containing
the precompiled Triton kernels. This image is pulled and validated by the TKM
Agent as needed. The actual management of image signatures, pull secrets, and
validation policies is handled globally via TKM configuration (e.g., ConfigMap),
not per resource.

Once specified, the image is internally resolved to its digest (e.g., sha256:...).
This digest acts as the authoritative identifier throughout the system for validation,
compatibility checks, cache extraction, and mounting.

GPU compatibility is assessed dynamically by the TKM Agent on a per-node, per-GPU basis.
The CRD itself does not include any GPU-specific configuration.

> *[OI] Are there any GPU Type specific fields?*
> I don't think so - as these are in teh image iteself?

Example of TritonKernelCache CRD:

```yaml
apiVersion: tkm.io/v1alpha1
kind: TritonKernelCache
metadata:
  name: kernel-y
  namespace: ml-apps
spec:
  cacheImage: quay.io/example/kernel-y:latest
status:
  conditions:
    - lastTransitionTime: "2025-05-08T21:06:07Z"
      message: 'TritonKernelCache reconciliation failed on the following nodes: [node1]'
      reason: Error
      status: "True"
      type: Error
```

#### TritonKernelCacheNodeStatus and TritonKernelCacheNodeStatusCluster CRD

TritonKernelCacheNodeStatus and TritonKernelCacheNodeStatusCluster CRD instances
are created by the TKM Agent, not the user.
Each node will have one such CR to represent its status, and it will contain
information for all kernel caches relevant to that node, organized per GPU.

If the corresponding TritonKernelCache is namespace-scoped, the NodeStatus CR
should live in the same namespace.If the corresponding TritonKernelCache is c
luster-scoped, a cluster-scoped NodeStatus CR (TritonKernelCacheNodeStatusCluster)
will be used instead.

While nodes themselves are not namespaced, the namespace of the NodeStatus CR
follows the scope of the kernel cache resource it reports on. This allows the
operator to correctly associate status objects with their source cache definitions.

This structure enables more efficient status tracking in environments with
heterogeneous GPU configurations and supports CSI plugin queries via the TKM Agent.

Summary of data reflected in the CRD:

- **nodeName**: The name of the Kubernetes node.
- **gpus**: List of physical GPUs on the node with:
  - **id**: GPU index (e.g., 0–7).
  - **gpuType**: (e.g., nvidia-a100).
  - **driverVersion**: The GPU driver version.
- **caches**: Map of kernel cache identifiers to:
  - **compatibleGPUs**: List of GPU groups that support the cache.
  - **incompatibleGPUs**: List of GPU groups that do not support the cache, with reason/message.
- **lastUpdated**: Timestamp of the last compatibility update.


This consolidated per-node, per-GPU view supports scalable monitoring and allows the CSI
driver to consult the Agent instead of accessing the Kubernetes API directly.

> *[OI] Do need or can we have a used by?*

Example of TritonKernelCacheNodeStatus CRD:

```yaml
status:
  gpus:
    - id: 0
      gpuType: nvidia-a100
      driverVersion: 535.43.02
    - id: 1
      gpuType: nvidia-a100
      driverVersion: 535.43.02
    - id: 2
      gpuType: nvidia-h100
      driverVersion: 550.00.01
    - id: 3
      gpuType: nvidia-v100
      driverVersion: 470.57.02

  caches:
    kernel-x:
      compatibleGPUs:
        - ids: [0, 1]
          gpuType: nvidia-a100
          driverVersion: 535.43.02
      incompatibleGPUs:
        - ids: [3]
          gpuType: nvidia-v100
          driverVersion: 470.57.02
          reason: "Missing SM 7.0"
          message: "Kernel built only for Ampere+"
      lastUpdated: "2025-06-03T14:00:00Z"
```

#### CSI Cache Extraction and Mounting Behavior

The CSI plugin, after validating kernel cache readiness via the Agent
(see next section), extracts the kernel cache to a structured directory
on the node's filesystem. The proposed directory layout is:

```console
/var/run/tkm/caches/<namespace>/<pod-name>/<cache-id>/
```

Where <cache-id> is derived from the image digest internally resolved from the
OCI image provided in the pod spec (e.g., `sha256:abc123`) as resolved from
the OCI image.

This structure ensures:

-  Isolation across namespaces and pods
-  Simplified monitoring via Triton Cache Manager (TCM)
-  Easier cache cleanup and usage tracking by the Agent

By default, the CSI driver mounts this cache directory as read-only into the
requesting pod to maintain kernel integrity and enable safe sharing between pods.

Applications requiring write access must opt-in by explicitly setting the `readOnly:`
`false` flag in the volumeAttributes section of the pod spec. The CSI driver receives
the OCI image reference (e.g., `quay.io/example/kernel-x:latest`) in the pod spec, and
internally resolves it to the digest to identify and validate the correct cache. The
digest is not exposed in the pod spec to keep the user interface clean and intuitive.

Additionally, the TKM Agent internally tracks which pods are actively using each kernel
cache. This internal usage map supports future cleanup, accurate monitoring, and TCM
integration without exposing this detail in the Kubernetes CRDs.

Example Pod Spec with Writable Cache Mount

##### Example Pod Spec with Writable Cache Mount

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
        driver: csi.tkm.io
        volumeAttributes:
          cacheImage: quay.io/example/kernel-x:latest
          readOnly: "false"
```

#### gRPC Communication Between CSI Driver and TKM Agent

The CSI driver communicates with the TKM Agent using a local gRPC interface.
This interaction is central to validating and tracking kernel cache usage
for pods, and effectively plugs the Agent into the pod lifecycle.

CSI Lifecycle Hooks via gRPC

Mount Request: When the CSI driver mounts a volume for a pod, it makes a
gRPC call to the TKM Agent to validate the requested kernel cache
(by resolved digest) for the node and GPU configuration. If the Agent
confirms the cache is Ready and Compatible, the CSI proceeds with extraction
and mounting.

Unmount Request: Upon pod deletion or volume unmount, the CSI driver notifies
the Agent, allowing it to update internal reference counts or perform cleanup
operations.

This bidirectional communication ensures the TKM Agent maintains an accurate,
real-time view of which pods are actively using which kernel caches. It also
opens the door to richer cache lifecycle tracking, future eviction logic,
and better observability — all without the CSI needing to access Kubernetes
APIs.

This mechanism effectively ties the TKM Agent into pod scheduling and runtime
behavior, via its tight integration with CSI.

##### Pros and Cons of Using a NodeStatus CRD

> [OI] API Review of bpfman CRDs flagged NodeStatus CRD as an issue.

A couple of Operators use the NodeStatus pattern of creating a Node specific CRD
to track the status of a higher level CRD for a given Kubernetes Node.
In particular,
[bpfman Operator](https://operatorhub.io/operator/bpfman-operator)
[Security Profiles Operator](https://operatorhub.io/operator/security-profiles-operator)
and Ingress Node Firewall Operator.
Below are some Pros and Cons for using this pattern.

###### Pros

One of the reasons for using this pattern is that for a given CRD, work has to be
done on every node (or a large subset of nodes) and because of potential hardware
differences between nodes, the action may succeed on some nodes and fail on others.
For large clusters with 100+ nodes, tracking success/failure, error message and
small of amount of metadata for 100+ nodes in the status of one CRD get messy and
hard for the user to consume.
In addition, 100+ agents writing their status to a given CRD instance may not
scale well.

By keeping an overall status in the higher level CRD, with `Success` if all nodes
succeeded and `Failure` if one or more nodes had a failure, and a list of nodes
with failures, more detailed errors as well additional node metadata can be kept
in Node specific CRD.

###### Cons

One of the major drawbacks to using this pattern is that it is not very Kubernetes
like.
The user creates the higher level CRD, but then has to get any failure details from
the Node specific CRD.

To address the issue of scale,
[Server Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/)
may be the solution.
This needs to be investigated.

## Interaction

Below is a rough flow when using TKM:

- User creates a TritonKernelCache CR specifying the kernel image.
- TKM Agent on each node:
  - Creates a TritonKernelCacheNodeStatus CR for its Node.
  - Validates the image and updates the status in the TritonKernelCacheNodeStatus CR.
  - Collects GPU information and verifies kernel cache compatibility.
  - Updates TritonKernelCacheNodeStatus CR.
- CSI plugin checks the TritonKernelCacheNodeStatus CR for the node and mounts the
  kernel cache as a volume if marked 'Ready' and 'Compatible'.
- Operator monitors that state of each TritonKernelCacheNodeStatus CR and updates
  the status of the TritonKernelCache CR.

An example of the flow is shown below:

```sh
               +------------------------+
               | User creates Triton    |
               | Kernel Cache (CR)      |
               +----------+-------------+
                           |
                           v
              +------------+-------------+
              | Each Node Agent verifies |
              | image signature          |
              +------------+-------------+
                           |
            +--------------+----------------+
            |                               |
   +--------v--------+            +---------v---------+
   | Signature valid |            | Signature invalid |
   +--------+--------+            +---------+---------+
            |                               |
            v                               v
+-----------+-----------+        +----------+----------+
| Mark CR as "Verified" |        | Mark CR as "Failed" |
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
| Mark CR as "Ready"    |                | Mark CR as "Failed"   |
| and "Compatible"      |                | with error details    |
+-----------+-----------+                +-----------------------+
            |
            v
+-----------+-----------+
| Pod requests volume   |
| from CSI driver with  |
| cache from image      |
+-----------+-----------+
            |
            v
+-----------+-----------+
| CSI Driver validates  |
| cache and mounts      |
| volume                |
+-----------------------+
```

## Example pod volume request

```yaml
volumes:
  - name: kernel-volume
    csi:
      driver: csi.tkm.io
      volumeAttributes:
        kernel-name: kernel-x
```

## State Management

To ensure resilience and consistent state management, the operator will utilize
a lightweight embedded database (such as Sled/SQLite/BoltDB) to maintain the
current state of the Triton kernel images. This allows the operator to recover
seamlessly from failures or restarts without losing track of Triton kernel
image validation and metadata status.

The database will be used to store:

- Kernel image metadata
- Validation status and signature checks
- Last known good state

This database will be synchronized with the Kubernetes API state to ensure
consistency between the operator's in-memory data and the persistent storage.

### Design Considerations

Instead of managing separate resources for kernel metadata and cache, the TKM
operator will use a unified TritonKernelCache resource. This avoids redundancy
since the kernel cache and its metadata are tightly coupled in Triton-lang.
This single resource will hold both cache and metadata information, simplifying
management and reducing potential conflicts.

## Open Questions

- Should validation be enforced strictly, or allow fallback for unverified
  images?
    - Global configuration knob, `allow-unsigned-images` and `verify-enabled`?
- How to handle image updates during runtime?
- Does TKM have to manage access to GPU? Can 20 different pods all load their
  Triton kernels simultaneously? Use:
  [extended-resource-node](https://kubernetes.io/docs/tasks/administer-cluster/extended-resource-node/)

## Alternatives Considered

- Running the controller as a short-lived process (daemonless). While this
  approach would reduce resource consumption when idle, it poses a challenge
  in responding promptly to Triton kernel image validation and updates.
  Additionally, frequent start-stop cycles can increase latency during critical
  operations.

- Keeping all CRDs cluster-scoped. While simpler to manage and deploy, this
  approach lacks namespace isolation, making it harder to enforce security
  boundaries between different workloads.

## Future Work

- Add metrics for the Triton Kernel usage.
- Improve signature validation with additional cosign policy support.
