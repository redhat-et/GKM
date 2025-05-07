# Triton Kernel Manager Operator and CSI Plugin Design

## Introduction

The Triton Kernel Manager (TKM) Operator and CSI Plugin manage Triton kernel
distribution and usage within a Kubernetes environment. The operator validates
and inspects Triton kernel images, while the CSI plugin handles Triton Kernel
cache extraction and volume mounting. The architecture follows a
controller-runtime approach, leveraging Kubernetes Custom Resources (CRs) and
a CSI plugin to manage kernel images efficiently.

## Motivation

The primary motivation of the Triton Kernel Manager Operator and CSI Plugin is
to reduce the startup time of large language models (LLMs) that use Triton
Kernels. By providing a pre-tuned kernel cache as a directory that can be
consumed by Triton-lang at runtime, we aim to optimize model loading
performance and reduce latency. Additionally, managing kernel images and
ensuring their validity before usage in containers is crucial for performance
optimisation and security.

The TKM operator focuses on:

- Validating Triton kernel cache image signatures.
- Supporting both cluster and namespace-scoped CRDs to improve security and
  flexibility

The TKM Agent focuses on:

- Detecting GPU hardware and driver versions on each node.
- Validating cache compatibility against the node's hardware.
- Reporting status to the control plane via node-specific CRs.

The CSI plugin focuses on:

- Facilitating seamless cache extraction and mounting via a CSI plugin

This clear separation of concerns ensures that the CSI plugin does not perform
image validation, while the operator remains focused on image inspection and
verification.

## Goals

- Decouple kernel image validation from mounting
- Integrate with cargohold for cache image inspection and validation
- Provide both cluster and namespace-scoped CRDs
- Maintain the controller as a long-running daemon for consistent state
  management and responsiveness

## Architecture

### Components

```bash
                     ┌───────────────────────────────────┐
                     │ Control Plane                     │
                     │                                   │
                     │ ┌───────────────────────────────┐ │
                     │ │ TritonKernelCache (CR)        │ │
                     │ │ - ociImage                    │ │
                     │ │ - validateSignature           │ │
                     │ └──────────────┬────────────────┘ │
                     │                │                  │
                     │                ▼                  │
                     │      ┌─────────────────────┐      │
                     │      │ Operator/controller │      │
                     │      │ Runs on control     │      │
                     │      │ plane, validates,   │      │
                     │      │ Triton Cache image  │      │
                     │      │ Signature.          │      │
                     │      └─────────┬───────────┘      │
                     │                │                  │
                     └────────────────┼──────────────────┘
                                      │
                     ┌────────────────┴────────────────────┐
                     │ Worker Node                         │
                     │                                     │
                     │ ┌────────────────────────────────┐  │
                     │ │ TKM Agent (DaemonSet)          │  │
                     │ │ - Detects GPU info             │  │
                     │ │ - Validates cache compatibility│  │
                     │ │ - Updates node-specific CR     │  │
                     │ └──────────────┬─────────────────┘  │
                     │                │                    │
                     │                ▼                    │
                     │ ┌────────────────────────────┐      │
                     │ │ CSI Driver (DaemonSet)     │      │
                     │ │ - Watches pod volumes      │      │
                     │ │ - Loads kernel cache into  │      │
                     │ │   volume if "Ready"        │      │
                     │ └────────────────────────────┘      │
                     └─────────────────────────────────────┘
```

#### Control Plane Components

Operator/Controller (Control Plane): Validates Triton kernel images, inspects
metadata, and updates CR status. Manages both cluster and namespace-scoped
CRDs. Runs as a long-lived controller on the control plane.

#### Worker Node Components

- TKM Agent (Node-local Daemon): Discovers GPU hardware and driver versions,
  verifies kernel cache compatibility, updates node-specific CRs, and reports
  status to the control plane. Runs as a DaemonSet on each worker node.

- CSI Driver (Node-local Daemon): Mounts the validated kernel cache onto the
  pod's volume if marked as `Ready` and `Compatible` on the node. Runs as a
  DaemonSet on each worker node.

### CRDs

#### Namespaced CRDs

To increase security, the Triton Kernel Manager (TKM) Operator supports
namespace-scoped CRDs. This enables the restriction of Triton kernel
cache loading and mounting to specific namespaces, thereby enhancing
isolation between workloads.

##### Motivation

Namespace-scoped CRDs improve security and flexibility by allowing
administrators to limit Triron kernel usage to designated namespaces.
This is particularly useful in multi-tenant Kubernetes clusters where
different applications may require distinct Triton Kernel configurations.

##### Advantages:

- Improved security through namespace isolation.
- Clear separation of kernel cache resources between tenants.
- Simplified CRD structure by merging cache and metadata.

#### TritonKernelCache CRD (Namespace Scoped)

Represents the desired state of a kernel cache.

- ociImage: The container image containing the kernel binary.
- validateSignature: Boolean to enforce signature checks.

For example:

```yaml
apiVersion: tkm.io/v1alpha1
kind: TritonKernelCache
metadata:
  name: kernel-y
  namespace: ml-apps
spec:
  ociImage: quay.io/example/kernel-y:latest
  validateSignature: false
```

#### TritonKernelCacheCluster CRD (Cluster-Scoped)

Same as TritonKernelCache but applies at the cluster level.

For example:

```yaml
apiVersion: tkm.io/v1alpha1
kind: TritonKernelCacheCluster
metadata:
  name: kernel-x
spec:
  ociImage: quay.io/example/kernel-x:latest
  validateSignature: true
```

#### TritonKernelCacheNodeStatus CRD (Node-Specific)

Represents the per-node status of a Triton kernel cache.

- nodeName: The name of the node.
- kernelCacheRef: Reference to the Triton kernel cache.
- gpuType: The type of GPU present.
- driverVersion: Version of the GPU driver.

For example:

```yaml
apiVersion: tkm.io/v1alpha1
kind: TritonKernelCacheNodeStatus
metadata:
  name: kernel-x-node1
  namespace: tkm-system
spec:
  nodeName: node1
  kernelCacheRef: kernel-x
  gpuType: nvidia
  driverVersion: 470.57.02
status:
  conditions:
    - type: Ready
      status: "True"
    - type: Compatible
      status: "True"
```

## Interaction

- User creates a TritonKernelCache CR specifying the kernel image.
- Operator validates the image and updates the status.
- TKM Agent on each node:
  - Collects GPU information and verifies kernel cache compatibility.
  - Updates TritonKernelCacheNodeStatus CR.
- CSI plugin checks the TritonKernelCacheNodeStatus CR for the node and mounts the kernel cache as a volume if marked 'Ready' and 'Compatible'.

An example of the flow is shown below:

```sh
                +------------------------+
                | User creates Triton    |
                | Kernel Cache (CR)      |
                +----------+-------------+
                           |
                           v
               +-----------+------------+
               | Controller verifies    |
               | image signature        |
               +-----------+------------+
                           |
            +--------------+----------------+
            |                                 |
   +--------v--------+               +---------v---------+
   | Signature valid |               | Signature invalid |
   +--------+--------+               +---------+---------+
            |                                  |
            v                                  v
+-----------+-----------+             +---------+---------+
| Mark CR as "Verified" |             | Mark CR as "Failed"|
+-----------+-----------+             +---------+----------+
            |
            v
+-----------+-----------+
| Agent reads "Verified"|
| status from CR and    |
| runs preflight checks |+---------------------------+
| using image metadata  |                            |
+-----------+-----------+                            |
            |                                        |
            v                                        v
+-----------+------------+               +-----------+-----------+
| Preflight check passes |               | Preflight check fails |
+-----------+------------+               +-----------------------+
            |                                        |
            v                                        v
+-----------+-----------+                +-----------+---------+
| Mark CR as "Ready" and|                | Mark CR as "Failed" |
| "Compatible"          |                | with error details  |
+-----------+-----------+                +---------------------+
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

### State Management

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
- How to handle image updates during runtime?

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
