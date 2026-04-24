# Getting Started Guide

This guide provides the list of prerequisites to building, instructions on
building GKM and description of how to deploy GKM.

## Prerequisites

- go version v1.25.0+
- podman version 5.3.1+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

The following packages are also required to build:

```sh
sudo dnf install -y gpgme-devel libdrm-devel libbtrfs btrfs-progs \
     btrfs-progs-devel hwloc hwloc-devel
```

OR

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
gkm-agent-7hvdw                 1/1     Running   0          3m28s
gkm-agent-jk2l9                 1/1     Running   0          3m28s
gkm-operator-6f4b9df6f6-p648s   1/1     Running   0          3m28s
```

To delete a `kind` cluster with a simulated GPU:

```sh
make destroy-kind
```

## Install Test Pod Using GKM on KIND Cluster

There are example yaml files that create `GKMCache` and `ClusterGKMCache`
custom resource (CR) instances, each of which points to an OCI Image with GPU
Kernel Cache.
([Examples Directory](./Examples.md)) explains in detail the layout of the
[./examples/](https://github.com/redhat-et/GKM/tree/main/examples) files and how
to properly deploy them in different environments.

Example (`cat examples/base/scope/namespace/gkmcache.yaml`):

```yaml
apiVersion: gkm.io/v1alpha1
kind: GKMCache
metadata:
  name: gkm-test-obj
  namespace: gkm-test-ns-1
spec:
  image: quay.io/gkm/cache-examples:vector-add-cache-rocm-v2
  accessModes:
    - ReadWriteOnce
```

The example yaml also includes several test pod that references the `PVC` that
is create via GKM, which is just the same name as the `GKMCache` CR instance.

Example (`cat examples/base/access/rox/pod-1.yaml`):

```yaml
kind: Pod
apiVersion: v1
metadata:
  name: gkm-test-pod-1
  namespace: gkm-test-ns-1
spec:
  securityContext:
    fsGroup: 1000
  containers:
    - name: test
      image: quay.io/fedora/fedora-minimal
      imagePullPolicy: IfNotPresent
      command: [sleep, 365d]
      securityContext:
        allowPrivilegeEscalation: false
        runAsNonRoot: true
        runAsUser: 1000
      volumeMounts:
        - name: kernel-volume
          mountPath: /cache
  volumes:
    - name: kernel-volume
      persistentVolumeClaim:
        claimName: gkm-test-obj
```

Pod Spec Highlights:

- The `volumes:` named `kernel-volume` references a PVC via
  `persistentVolumeClaim:` and references the GKM Cache CR via
  `claimName: gkm-test-obj`.
- The `volumeMounts:` named `kernel-volume` maps the GPU Kernel Cache to the
  directory `/cache` within the pod.

Now the example yamls can be applied:

```sh
make deploy-examples-kind
```

The test pods `gkm-test-ns-*` should be running and the cache should be volume
mounted in the pods.
Note: The `Completed` pods are Kubernetes Jobs that GKM created to download and
extract the OCI Image into a PVC.

<!-- markdownlint-disable  MD013 -->
<!-- Temporarily disable MD013 - Line length to keep the block formatting  -->
```sh
$ kubectl get pods -A
NAMESPACE                             NAME                                                     READY   STATUS      RESTARTS   AGE
cert-manager                          cert-manager-7d75c44448-dsz84                            1/1     Running     0          5m44s
cert-manager                          cert-manager-cainjector-798687f777-phhld                 1/1     Running     0          5m43s
cert-manager                          cert-manager-webhook-6b7cdfdf8b-5lr5q                    1/1     Running     0          5m43s
gkm-system                            gkm-agent-7hvdw                                          1/1     Running     0          5m31s
gkm-system                            gkm-agent-jk2l9                                          1/1     Running     0          5m31s
gkm-system                            gkm-operator-6f4b9df6f6-p648s                            1/1     Running     0          5m31s
gkm-test-ns-1-rox-cluster-rocm-v2     gkm-test-obj-rox-cluster-rocm-v29x4md-lvntz              0/1     Completed   0          96s
gkm-test-ns-1-rox-cluster-rocm-v2     gkm-test-pod-1-rox-cluster-rocm-v2                       1/1     Running     0          98s
gkm-test-ns-1-rox-cluster-rocm-v2     gkm-test-pod-2-rox-cluster-rocm-v2                       1/1     Running     0          98s
gkm-test-ns-1-rox-namespace-rocm-v3   gkm-test-obj-rox-namespace-rocm-v37pbpx-szc2q            0/1     Completed   0          100s
gkm-test-ns-1-rox-namespace-rocm-v3   gkm-test-pod-1-rox-namespace-rocm-v3                     1/1     Running     0          100s
gkm-test-ns-1-rox-namespace-rocm-v3   gkm-test-pod-2-rox-namespace-rocm-v3                     1/1     Running     0          100s
gkm-test-ns-1-rox-namespace-rocm-v3   gkm-test-pod-3-rox-namespace-rocm-v3                     1/1     Running     0          100s
gkm-test-ns-1-rwo-cluster-rocm-v3     gkm-test-ds-1-rwo-cluster-rocm-v3-gtk5x                  1/1     Running     0          101s
gkm-test-ns-1-rwo-cluster-rocm-v3     gkm-test-ds-1-rwo-cluster-rocm-v3-j8lmk                  1/1     Running     0          101s
gkm-test-ns-1-rwo-cluster-rocm-v3     gkm-test-ds-2-rwo-cluster-rocm-v3-bf8w9                  1/1     Running     0          102s
gkm-test-ns-1-rwo-cluster-rocm-v3     gkm-test-obj-rwo-cluster-rocm-v3-286ba108chcrl-dnwzb     0/1     Completed   0          102s
gkm-test-ns-1-rwo-cluster-rocm-v3     gkm-test-obj-rwo-cluster-rocm-v3-c6f37497qjl5g-l9cbl     0/1     Completed   0          102s
gkm-test-ns-1-rwo-namespace-rocm-v2   gkm-test-ds-1-rwo-namespace-rocm-v2-bnn8r                1/1     Running     0          104s
gkm-test-ns-1-rwo-namespace-rocm-v2   gkm-test-ds-1-rwo-namespace-rocm-v2-srd2f                1/1     Running     0          104s
gkm-test-ns-1-rwo-namespace-rocm-v2   gkm-test-ds-2-rwo-namespace-rocm-v2-dm7jb                1/1     Running     0          104s
gkm-test-ns-1-rwo-namespace-rocm-v2   gkm-test-ds-3-rwo-namespace-rocm-v2-r54df                1/1     Running     0          104s
gkm-test-ns-1-rwo-namespace-rocm-v2   gkm-test-obj-rwo-namespace-rocm-v2-7529441aghdw5-pdblk   0/1     Completed   0          104s
gkm-test-ns-1-rwo-namespace-rocm-v2   gkm-test-obj-rwo-namespace-rocm-v2-b6c984234brgh-tk6q7   0/1     Completed   0          104s
gkm-test-ns-2-rox-cluster-rocm-v2     gkm-test-obj-rox-cluster-rocm-v2wdgn4-rsrm6              0/1     Completed   0          96s
gkm-test-ns-2-rox-cluster-rocm-v2     gkm-test-pod-3-rox-cluster-rocm-v2                       1/1     Running     0          98s
gkm-test-ns-2-rwo-cluster-rocm-v3     gkm-test-ds-3-rwo-cluster-rocm-v3-7j82t                  1/1     Running     0          101s
gkm-test-ns-2-rwo-cluster-rocm-v3     gkm-test-obj-rwo-cluster-rocm-v3-8724a7b7vjg8h-plxfk     0/1     Completed   0          101s
gkm-test-ns-2-rwo-cluster-rocm-v3     gkm-test-obj-rwo-cluster-rocm-v3-9dde6ea5lhtwj-hxh5h     0/1     Completed   0          102s
:
kyverno                               kyverno-admission-controller-578c64df84-gm9x9            1/1     Running     0          4m50s
kyverno                               kyverno-background-controller-66cb87dd88-p852k           1/1     Running     0          5m18s
kyverno                               kyverno-cleanup-controller-65b4494b5f-6rjlx              1/1     Running     0          5m18s
kyverno                               kyverno-reports-controller-db4986dc-2dq6w                1/1     Running     0          5m18s
:

$ kubectl exec -it -n gkm-test-ns-1-rox-cluster-rocm-v2 gkm-test-pod-1-rox-cluster-rocm-v2 -c test -- sh
sh-5.3$ ls /cache
CETLGDE7YAKGU4FRJ26IM6S47TFSIUU7KWBWDR3H2K3QRNRABUCA  MCELTMXFCSPAMZYLZ3C3WPPYYVTVR4QOYNE52X3X6FIH7Z6N6X5A
CHN6BLIJ7AJJRKY2IETERW2O7JXTFBUD3PH2WE3USNVKZEKXG64Q  c4d45c651d6ac181a78d8d2f3ead424b8b8f07dd23dc3de0a99f425d8a633fc6
```
<!-- markdownlint-enable  MD013 -->

To remove, the example yamls:

```sh
make undeploy-examples-kind
```

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
