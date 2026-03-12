# Getting Started Guide

This guide provides the list of prerequisites to building, instructions on
building GKM and description of how to deploy GKM.

## Prerequisites

- go version v1.25.0+
- podman version 5.3.1+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### Automated Installation (RHEL 10 / CentOS Stream 10)

For RHEL 10 or CentOS Stream 10 systems, you can install all dependencies (including go, podman, kubectl, and build packages) using:

```sh
make install-deps
```

This will:
- Install system development packages (gpgme-devel, libdrm-devel, hwloc-devel)
- Install btrfs development headers
- Install or upgrade Go to v1.25.0+ if needed
- Install or upgrade Podman to v5.3.1+ if needed
- Install or upgrade kubectl to v1.11.3+ if needed

### Manual Installation

The following packages are required to build:

**For Fedora/RHEL/CentOS:**

```sh
sudo dnf install -y gpgme-devel libdrm-devel libbtrfs btrfs-progs \
     btrfs-progs-devel hwloc hwloc-devel
```

> **Note for RHEL 10**: Some packages may not be available in standard repositories.
> Use `make install-deps` or see [hack/install_deps.sh](../hack/install_deps.sh) for the installation script
> that sources packages from CentOS Stream 10 and Fedora repositories.

**For Debian/Ubuntu:**

```sh
sudo apt-get install -y libgpgme-dev libbtrfs-dev btrfs-progs libgpgme11-dev \
     libseccomp-dev
```

## Building GKM

To compile GKM, simply run:

```console
cd $USER_SRC_DIR/GKM/
make build
```

Alternatively, to build GKM in container images that can be deployed in a
Kubernetes cluster, simply run:

```console
cd $USER_SRC_DIR/GKM/
make build-images
```

> **Note:**
> Run `make help` for more information on all potential `make` targets.

To build a MCV binary, run (see [MCV](../mcv/README.md) for details on MCV):

```console
cd $USER_SRC_DIR/GKM/mcv/
make build
```

## To Deploy a Cluster With a Simulated GPU

To simulate a GPU in `kind`, GKM is leveraging scripts in
[kind-gpu-sim](https://github.com/maryamtahhan/kind-gpu-sim).
These scripts require `podman` and some environment variables defined.
If `kind` is not being used, then `docker` can be used to build images.
`Makefile` will use `podman` if found, and fallback to `docker` if not found
(but `make run-on-kind` will fail).

To create a `kind` cluster with a simulated GPU and latest GKM running:

```sh
export KIND_EXPERIMENTAL_PROVIDER=podman
export DOCKER_HOST=unix:///run/user/$UID/podman/podman.sock
make run-on-kind
```

Check the GKM installed pods:

```sh
$ kubectl get pods -n gkm-system
NAME                            READY   STATUS    RESTARTS   AGE
gkm-agent-85lqg                 1/1     Running   0          5m7s
gkm-agent-kzx6j                 1/1     Running   0          5m7s
gkm-operator-7dc756c84b-2w74z   3/3     Running   0          5m7s
```

To delete a `kind` cluster with a simulated GPU:

```sh
make destroy-kind
```

## Install Test Pod Using GKM

There are example yamls that creates `GKMCache` and `ClusterGKMCache` custom
resource (CR) instances, each of which points to an OCI Image with GPU Kernel
Cache.
See [./examples/](https://github.com/redhat-et/GKM/tree/main/examples).
Sample:

```yaml
apiVersion: gkm.io/v1alpha1
kind: GKMCache
metadata:
  name: vector-add-cache-rocm-v2
  namespace: gkm-test-ns-scoped-1
  labels:
    gkm.io/signature-format: cosign-v2
spec:
  image: quay.io/gkm/cache-examples:vector-add-cache-rocm-v2
  storageClassName: standard
```

The example yaml also includes a test pod that references the `PVC` that is
create via GKM, which is just the same name as the `GKMCache` CR instance.
Example:

```yaml
kind: Pod
apiVersion: v1
metadata:
  name: gkm-test-pod-1
  namespace: gkm-test-ns-scoped-1
spec:
  tolerations:
    - key: gpu
      operator: Equal
      effect: NoSchedule
      value: "true"
  containers:
  - name: test
    :
    volumeMounts:
    - name: kernel-volume
      mountPath: "/cache"
  volumes:
      volumes:
        - name: kernel-volume
          persistentVolumeClaim:
            claimName: vector-add-cache-rocm-v2
```

Pod Spec Highlights:

- The `volumes:` named `kernel-volume` references a PVC via
  `persistentVolumeClaim:` and references the GKM Cache CR via
  `claimName: vector-add-cache-rocm-v2`.
- The `volumeMounts:` named `kernel-volume` maps the GPU Kernel Cache to the
  directory `/cache` within the pod.

Because of the Node Selector, the test pod will be launched on node
`kind-gpu-sim-worker`. Determine the GKM Agent instant running on this node:

<!-- markdownlint-disable  MD013 -->
<!-- Temporarily disable MD013 - Line length to keep the block formatting  -->
```sh
$ kubectl get pods -n gkm-system -o wide
NAME                            READY   STATUS    RESTARTS   AGE    IP           NODE
gkm-agent-85lqg                 1/1     Running   0          5m7s   10.244.2.4   kind-gpu-sim-worker    <-- HERE
gkm-agent-kzx6j                 1/1     Running   0          5m7s   10.244.1.5   kind-gpu-sim-worker2
gkm-operator-7dc756c84b-2w74z   3/3     Running   0          5m7s   10.244.0.5   kind-gpu-sim-control-plane
```
<!-- markdownlint-enable  MD013 -->

Now the example yaml can be applied:

```sh
make deploy-examples
```

The test pods `gkm-test-pod-*` should be running and the cache should be volume
mounted in the pods:

<!-- markdownlint-disable  MD013 -->
<!-- Temporarily disable MD013 - Line length to keep the block formatting  -->
```sh
$ kubectl get pods -A
NAMESPACE              NAME                            READY   STATUS    RESTARTS   AGE
:
gkm-system             gkm-agent-85lqg                 1/1     Running   0          41s
gkm-system             gkm-agent-kzx6j                 1/1     Running   0          41s
gkm-system             gkm-operator-7dc756c84b-2w74z   3/3     Running   0          41s
gkm-test-cl-scoped     gkm-test-pod-1                  1/1     Running   0          19s
gkm-test-cl-scoped     gkm-test-pod-2                  1/1     Running   0          19s
gkm-test-cl-scoped     gkm-test-pod-3                  1/1     Running   0          19s
gkm-test-ns-scoped-1   gkm-test-pod-1                  1/1     Running   0          22s
gkm-test-ns-scoped-1   gkm-test-pod-2                  1/1     Running   0          22s
gkm-test-ns-scoped-1   gkm-test-pod-3                  1/1     Running   0          22s
gkm-test-ns-scoped-2   gkm-test-pod-1                  1/1     Running   0          21s
gkm-test-ns-scoped-2   gkm-test-pod-2                  1/1     Running   0          21s
gkm-test-ns-scoped-2   gkm-test-pod-3                  1/1     Running   0          21s
:

$ kubectl exec -it -n gkm-test-ns-scoped-1 gkm-test-pod-1  -- sh
sh-5.2# ls /cache
CETLGDE7YAKGU4FRJ26IM6S47TFSIUU7KWBWDR3H2K3QRNRABUCA  MCELTMXFCSPAMZYLZ3C3WPPYYVTVR4QOYNE52X3X6FIH7Z6N6X5A
CHN6BLIJ7AJJRKY2IETERW2O7JXTFBUD3PH2WE3USNVKZEKXG64Q  c4d45c651d6ac181a78d8d2f3ead424b8b8f07dd23dc3de0a99f425d8a633fc6
```
<!-- markdownlint-enable  MD013 -->

## Build and Run Private GKM Build

By default, `Makefile` defaults to `quay.io/gkm/*` for pushing and pulling.
For building private images and testing, set the environment variable
`QUAY_USER` to override image repository.

> **Note:** Make sure not to check-in `kustomization.yaml` files with overridden
> quay.io user account.

Start by building and pushing the GKM images, then start `kind` cluster:

```sh
export QUAY_USER=<UserName>
make build-images
make push-images
make run-on-kind
```

### Building Without GPU Support

For environments without GPU hardware (e.g., KIND clusters with simulated GPUs),
you can build the agent image without ROCm packages to reduce image size and
build time. This is useful for development and testing scenarios.

To build the agent image without GPU support:

```sh
make build-image-agent NO_GPU_BUILD=true
```

Or to build all images (only the agent will skip ROCm installation):

```sh
make build-images NO_GPU_BUILD=true
```

When built with `NO_GPU_BUILD=true`:

- ROCm packages are not installed in the agent container
- The `NO_GPU` environment variable is set to `true` in the container
- The agent will run in no-GPU mode as indicated in the logs

This is the recommended approach for KIND deployments since they use simulated
GPUs and don't require actual ROCm libraries.

## Deployment Options

Steps to deploy GKM have been automated in the
[Makefile](https://github.com/redhat-et/GKM/blob/main/Makefile).
There are some complexities in deployment because GKM requires cert-manager
running and has dependencies on GPUs in the system.
For running in a KIND cluster, the GPU is being simulated from GKM perspective,
not workload perspective.
All of this has been automated, but running the wrong commands can cause
undefined behavior.
Below are the recommended steps for deploying and undeploying GKM in different
environments.

### Managing GKM in KIND, No Existing Cluster

Below are set of commands to manage the lifecycle of a KIND Cluster with the GKM
Operator when no KIND Cluster exists:

> **Note:** When building images for KIND deployments, it's recommended to use
> `NO_GPU_BUILD=true` to skip ROCm installation since KIND uses simulated GPUs.
> For example: `make build-images NO_GPU_BUILD=true` before running
> `make run-on-kind`.

- `make run-on-kind`: This command creates a KIND Cluster with GKM and
  cert-manager installed and simulated GPUs.
  - `make undeploy-on-kind` or `make undeploy-on-kind-force`: Optionally,
    these commands can be used to unwind the GKM deployment on a KIND Cluster.
    It leaves cert-manager installed.
    GKM is still a work in progress and stranded resources are currently not
    cleaned up properly by GKM, but the Makefile can be used to cleanup.
    It is recommended that these pods are stopped before GKM is removed.
    If there are workload pods running that have extracted GPU Kernel Cache
    mounted:
    - `make undeploy-on-kind`: Workload pods will remain running.
    - `make undeploy-on-kind-force`: Workload pods are removed by Makefile
      (Recommended).
  - `make redeploy-on-kind`: Optionally, this command can be used to redeploy
    the GKM deployment on a KIND Cluster.
    It assumes that cert-manager is already installed.
- `make destroy-kind`: This command stops the KIND Cluster completely.

### Managing GKM in Existing KIND Cluster

Below are set of commands to manage the lifecycle of a KIND Cluster with the GKM
Operator when a KIND Cluster already exists.
This assumes that the GPUs are properly being simulated in the existing KIND
Cluster.
See [kind-gpu-sim](https://github.com/maryamtahhan/kind-gpu-sim) for reference.

> **Note:** When building images for KIND deployments, it's recommended to use
> `NO_GPU_BUILD=true` to skip ROCm installation since KIND uses simulated GPUs.
> For example: `make build-images NO_GPU_BUILD=true` before running
> `make deploy-on-kind`.

- `make deploy-on-kind`: This command deploys GKM and cert-manager in the
  existing KIND Cluster.
  - `make undeploy-on-kind` or `make undeploy-on-kind-force`:: Optionally,
    these commands can be used to unwind the GKM deployment on a KIND Cluster.
    It leaves cert-manager installed.
    GKM is still a work in progress and stranded resources are currently not
    cleaned up properly by GKM, but the Makefile can be used to cleanup.
    It is recommended that these pods are stopped before GKM is removed.
    If there are workload pods running that have extracted GPU Kernel Cache
    mounted:
    - `make undeploy-on-kind`: Workload pods will remain running.
    - `make undeploy-on-kind-force`: Workload pods are removed by Makefile
      (Recommended).
  - `make redeploy-on-kind`: Optionally, this command can be used to redeploy
    the GKM deployment on a KIND Cluster.
    It assumes that cert-manager is already installed.
- `make undeploy-on-kind`: Same as above, this command can be used to unwind the
  GKM deployment on a KIND Cluster, leaving cert-manager installed.
- `make undeploy-cert-manager`: The Makefile doesn't keep track of whether
  cert-manager was installed before `make deploy-on-kind` was called or not, so
  the user needs to manually remove cert-manager with this command if needed.

### Managing GKM in Existing Cluster With Hardware

Below are set of commands to manage the lifecycle of a KIND Cluster with the GKM
Operator when a Cluster already exists.

- `make deploy`: This command deploys GKM and cert-manager in the existing
  Cluster.
  If cert-manager is already installed on the cluster, use `make redeploy`
  instead.
  - `make undeploy` or `make undeploy-force`: Optionally, these commands can be
    used to unwind the GKM deployment on a Cluster.
    It leaves cert-manager installed.
    GKM is still a work in progress and stranded resources are currently not
    cleaned up properly by GKM, but the Makefile can be used to cleanup.
    It is recommended that these pods are stopped before GKM is removed.
    If there are workload pods running that have extracted GPU Kernel Cache
    mounted:
    - `make undeploy`: Workload pods will remain running.
    - `make undeploy-force`: Workload pods are removed by Makefile (Recommended).
  - `make redeploy`: Optionally, this command can be used to redeploy the GKM
    deployment on a Cluster.
    It assumes that cert-manager is already installed.
- `make undeploy`: Same as above, this command can be used to unwind the GKM
  deployment on a KIND Cluster, leaving cert-manager installed.
- `make undeploy-cert-manager`: The Makefile doesn't keep track of whether
  cert-manager was installed before `make deploy` was called or not, so the user
  needs to manually remove cert-manager with this command if needed.
