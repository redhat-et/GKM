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
- Supporting both cluster and namespace-scoped CRDs to improve security and
  flexibility

The TKM Agent focuses on:

- Detecting GPU hardware and driver versions on each node.
- Validating cache compatibility against the node's hardware.
- Reporting status to the control plane via node-specific CRs.

The CSI Plugin focuses on:

- Facilitating seamless cache extraction and mounting via a CSI plugin

This clear separation of concerns ensures that the CSI plugin does not perform
image validation, while the operator remains focused on image inspection and
verification.

## Goals

- Decouple kernel image validation from mounting
- Integrate with CargoHold for cache image inspection and validation
- Provide both cluster and namespace-scoped CRDs
- Maintain the controller as a long-running daemon for consistent state
  management and responsiveness

## Architecture

### Components

```bash
                 ┌───────────────────────────────────────┐
                 │ Control Plane                         │
                 │                                       │
                 │ ┌───────────────────────────────────┐ │
                 │ │ TritonKernelCache (CR)            │ │
                 │ │ - ociImage                        │ │
                 │ │ - Load Status Summary             │ │
                 │ └─────────────────┬─────────────────┘ │
                 │                   │                   │
                 │                   ▼                   │
                 │ ┌───────────────────────────────────┐ │
                 │ │ Operator/controller (Deployment)  │ │
                 │ │ - Runs on control plane           │ │
                 │ │ - Registers CSI Driver            │ │
                 │ │ - Launches TKM Agent              │ │
                 │ │ - Tracks overall status across    │ │
                 │ │   all nodes in TritonKernelCache  │ │
                 │ └─────────────────┬─────────────────┘ │
                 │                   │                   │
                 └───────────────────┼───────────────────┘
                                     │
                 ┌───────────────────┴───────────────────┐
                 │ Worker Node                           │
                 │                                       │
                 │ ┌───────────────────────────────────┐ │
                 │ │ TritonKernelCacheNodeStatus (CR)  │ │
                 │ │ - GPU Info                        │ │
                 │ │ - Node Load Status                │ │
                 │ └─────────────────┬─────────────────┘ │
                 │                   │                   │
                 │                   ▼                   │
                 │ ┌───────────────────────────────────┐ │
                 │ │ TKM Agent (DaemonSet)             │ │
                 │ │ - Detects GPU Info                │ │
                 │ │ - Validate Image Signature        │ │
                 │ │ - Validates cache compatibility   │ │
                 │ │ - Create/Update node-specific CR  │ │
                 │ └─────────────────┬─────────────────┘ │
                 │                   │                   │
                 │                   ▼                   │
                 │ ┌───────────────────────────────────┐ │
                 │ │ CSI Driver (DaemonSet)            │ │
                 │ │ - Watches pod volumes             │ │
                 │ │ - Loads kernel cache into volume  │ │
                 │ │   if "Ready"                      │ │
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

- **TritonKernelCache CRD:** Represents the desired state of a kernel cache.
  Through this CRD, the user can specify the OCI Image, which is the container
  image containing the kernel binary.
  This is a namespace scoped CRD.

  > *[OI] Possible Naming Options (prefer a shorter name):
  > TritonKernelCache/TKMCache/TKMImage/TKMCacheImage/TKMKernelImage*
- **TritonKernelCacheCluster CRD:** Represents the desired state of a kernel
  cache.
  Same as TritonKernelCache CRD, but a cluster scoped CRD.
- **TritonKernelCacheNodeStatus CRD:** Represents the actual state of a kernel
  cache for a given Kubernetes Node.
  The user does not create or modify this object, but is used by TKM to reflect
  the status a kernel cache for a given Kubernetes Node.
  One instance of this CRD is created for each node for each `TritonKernelCache`
  instance.
  This is a namespace scoped CRD.
- **TritonKernelCacheNodeStatusCluster CRD:** Represents the actual state of a
  kernel.
  Same as TritonKernelCacheNodeStatus CRD, but a cluster scoped CRD.
  One instance of this CRD is created for each node for each
  `TritonKernelCacheCluster` instance.

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

#### TritonKernelCache and TritonKernelCacheCluster CRD

The TritonKernelCache and TritonKernelCacheCluster CRDs allow the user
to specify details about the OCI Image that contains the Triton kernel.
The data provided allows the image to be downloaded and the Triton kernel
extracted from the image.

> *[OI] Are there any GPU Type specific fields?*

Example of TritonKernelCache CRD:

```yaml
apiVersion: tkm.io/v1alpha1
kind: TritonKernelCache
metadata:
  name: kernel-y
  namespace: ml-apps
spec:
  ociImage:
    pullPolicy: IfNotPresent
    image: quay.io/example/kernel-y:latest
    pullSecret:
  validateSignature: false
status:
  conditions:
  - lastTransitionTime: "2025-05-08T21:06:07Z"
    message: 'TritonKernelCache Reconciliation failed on the following TritonKernelCacheNodeStatus
      objects: [kernel-y-node1]'
    reason: Error
    status: "True"
    type: Error
```

> *[OI] I don't think we need `validateSignature` here? We will want
> a TKM configuration option to allow unsigned images and to disable
> COSign. bpfman had a ConfigMap which contained a configuration file.*

#### TritonKernelCacheNodeStatus and TritonKernelCacheNodeStatusCluster CRD

TritonKernelCacheNodeStatus and TritonKernelCacheNodeStatusCluster CRD instances
are created by the TKM Agent, not the user.
There is an instance for each Kubernetes Node for each TritonKernelCache or
TritonKernelCacheCluster instance.
The purpose is to provide the status of a given Triton kernel for each node
as well detected GPU info from the node.

Summary of data reflected in CRD:

- **nodeName:** The name of the node.
- **kernelCacheRef:** Reference to the Triton kernel cache.
- **gpuType:** The type of GPU present.
- **driverVersion:** Version of the GPU driver.

> *[OI] Do need or can we have a used by?*

Example of TritonKernelCacheNodeStatus CRD:

```yaml
apiVersion: tkm.io/v1alpha1
kind: TritonKernelCacheNodeStatus
metadata:
  name: kernel-x-node1
  namespace: ml-apps
status:
  conditions:
    - type: Ready
      status: "True"
    - type: Compatible
      status: "True"
  driverVersion: 470.57.02
  kernelCacheRef: kernel-x
  gpuType: nvidia
  nodeName: node1
```

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
