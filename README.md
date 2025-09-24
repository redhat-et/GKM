# GPU-kernel-manager-operator

<!-- markdownlint-disable  MD033 -->
<img src="docs/images/gkm-logo.png" alt="gkm" width="20%" height="auto">
<!-- markdownlint-enable  MD033 -->

## Description

GPU Kernel Manager is a software stack that aims to deploy, manage and
monitor GPU Kernels in a Kubernetes cluster.
It will use the utilities developed in
[MCU](https://github.com/redhat-et/MCU) to accomplish these goals.

## Getting Started

### Prerequisites

- go version v1.22.0+
- podman version 5.3.1+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy a Cluster With a Simulated GPU

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
gkm-csi-node-w6flc              2/2     Running   0          5m7s
gkm-csi-node-xc2sb              2/2     Running   0          5m7s
gkm-operator-7dc756c84b-2w74z   3/3     Running   0          5m7s
```

To delete a `kind` cluster with a simulated GPU:

```sh
make destroy-kind
```

### Install Test Pod Using GKM

There are example yamls that creates `GKMCache` and `ClusterGKMCache` custom
resource (CR) instances, each of which points to an OCI Image with GPU Kernel
Cache.
See [./examples/](https://github.com/redhat-et/GKM/tree/main/examples).
Sample:

```yaml
apiVersion: gkm.io/v1alpha1
kind: GKMCache
metadata:
  name: vector-add-cache-rocm-1
spec:
  image: quay.io/gkm/cache-examples:vector-add-cache-rocm
```

The example yaml also includes a test pod that references the `GKMCache` CR
instance. Example:

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
  nodeSelector:
    gkm-test-node: "true"
  containers:
  - name: alpine
    :
    volumeMounts:
    - name: kernel-volume
      mountPath: "/cache"
  volumes:
  - name: kernel-volume
    csi:
      driver: csi.gkm.io
      volumeAttributes:
        csi.gkm.io/GKMCache: vector-add-cache-rocm-1
        csi.gkm.io/namespace: gkm-test-ns-scoped-1
```

Pod Spec Highlights:

- The `volumes:` named `kernel-volume` references the GKM CSI driver via
  `driver: csi.gkm.io` and references the GKM Cache CR via
  `csi.gkm.io/GKMCache: vector-add-cache-rocm`.
- The `volumeMounts:` named `kernel-volume` maps the GPU Kernel Cache to the
  directory `/cache` within the pod.
- There is a Node Selector `gkm-test-node: "true"`.
  The `make run-on-kind` command adds this label to node `kind-gpu-sim-worker`.
  This is help monitor logs while applying the pod.

Because of the Node Selector, the test pod will be launched on node
`kind-gpu-sim-worker`. Determine the CSI Plugin instant running on this node:

<!-- markdownlint-disable  MD013 -->
<!-- Temporarily disable MD013 - Line length to keep the block formatting  -->
```sh
$ kubectl get pods -n gkm-system -o wide
NAME                            READY   STATUS    RESTARTS   AGE    IP           NODE
gkm-agent-85lqg                 1/1     Running   0          5m7s   10.244.2.4   kind-gpu-sim-worker    <-- HERE
gkm-agent-kzx6j                 1/1     Running   0          5m7s   10.244.1.5   kind-gpu-sim-worker2
gkm-csi-node-w6flc              2/2     Running   0          5m7s   10.89.0.55   kind-gpu-sim-worker
gkm-csi-node-xc2sb              2/2     Running   0          5m7s   10.89.0.54   kind-gpu-sim-worker2
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
gkm-system             gkm-csi-node-w6flc              2/2     Running   0          41s
gkm-system             gkm-csi-node-xc2sb              2/2     Running   0          41s
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

### Build and Run Private GKM Build

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

### Deployment Options

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

#### Managing GKM in KIND, No Existing Cluster

Below are set of commands to manage the lifecycle of a KIND Cluster with the GKM
Operator when no KIND Cluster exists:

- `make run-on-kind`: This command creates a KIND Cluster with GKM and
  cert-manager installed and simulated GPUs.
  - `make undeploy-on-kind`: Optionally, this command can be used to unwind
    the GKM deployment on a KIND Cluster.
    It leaves cert-manager installed.
    If there are workload pods running that have extracted GPU Kernel Cache
    mounted, those pods will remain running.
    GKM is still a work in progress and stranded resources are currently not
    cleaned up properly.
    It is recommended that these pods are stopped before GKM is removed.
  - `make redeploy-on-kind`: Optionally, this command can be used to redeploy
    the GKM deployment on a KIND Cluster.
    It assumes that cert-manager is already installed.
- `make destroy-kind`: This command stops the KIND Cluster completely.

#### Managing GKM in Existing KIND Cluster

Below are set of commands to manage the lifecycle of a KIND Cluster with the GKM
Operator when a KIND Cluster already exists.
This assumes that the GPUs are properly being simulated in the existing KIND
Cluster.
See [kind-gpu-sim](https://github.com/maryamtahhan/kind-gpu-sim) for reference.

- `make deploy-on-kind`: This command deploys GKM and cert-manager in the
  existing KIND Cluster.
  - `make undeploy-on-kind`: Optionally, this command can be used to unwind
    the GKM deployment on a KIND Cluster.
    It leaves cert-manager installed.
    If there are workload pods running that have extracted GPU Kernel Cache
    mounted, those pods will remain running.
    GKM is still a work in progress and stranded resources are currently not
    cleaned up properly.
    It is recommended that these pods are stopped before GKM is removed.
  - `make redeploy-on-kind`: Optionally, this command can be used to redeploy
    the GKM deployment on a KIND Cluster.
    It assumes that cert-manager is already installed.
- `make undeploy-on-kind`: Same as above, this command can be used to unwind the
  GKM deployment on a KIND Cluster, leaving cert-manager installed.
- `make undeploy-cert-manager`: The Makefile doesn't keep track of whether
  cert-manager was installed before `make deploy-on-kind` was called or not, so
  the user needs to manually remove cert-manager with this command if needed.

#### Managing GKM in Existing Cluster With Hardware

Below are set of commands to manage the lifecycle of a KIND Cluster with the GKM
Operator when a Cluster already exists.

- `make deploy`: This command deploys GKM and cert-manager in the existing
  Cluster.
  If cert-manager is already installed on the cluster, use `make redeploy`
  instead.
  - `make undeploy`: Optionally, this command can be used to unwind the GKM
    deployment on a Cluster.
    It leaves cert-manager installed.
    If there are workload pods running that have extracted GPU Kernel Cache
    mounted, those pods will remain running.
    GKM is still a work in progress and stranded resources are currently not
    cleaned up properly.
    It is recommended that these pods are stopped before GKM is removed.
  - `make redeploy`: Optionally, this command can be used to redeploy the GKM
    deployment on a Cluster.
    It assumes that cert-manager is already installed.
- `make undeploy`: Same as above, this command can be used to unwind the GKM
  deployment on a KIND Cluster, leaving cert-manager installed.
- `make undeploy-cert-manager`: The Makefile doesn't keep track of whether
  cert-manager was installed before `make deploy` was called or not, so the user
  needs to manually remove cert-manager with this command if needed.

## Project Distribution

Following are the steps to build the installer and distribute this project to
users.

1. Build the installer for the image built and published in the registry:

    ```sh
    make build-installer IMG=quay.io/gkm/operator:latest
    ```

    > **Note:**  The makefile target mentioned above generates an 'install.yaml'
    > file in the dist directory. This file contains all the resources built
    > with Kustomize, which are necessary to install this project without
    > its dependencies.

1. Using the installer
    <!-- markdownlint-disable  MD033 -->
    Users can just run kubectl apply -f <URL for YAML BUNDLE> to install the
    project, i.e.:

    <!-- markdownlint-disable  MD013 -->
    <!-- Temporarily disable MD013 - Line length to keep the block formatting  -->
    ```sh
    kubectl apply -f https://raw.githubusercontent.com/<org>/GPU-kernel-manager-operator/<tag or branch>/dist/install.yaml
    ```
    <!-- markdownlint-enable  MD013 -->
    <!-- markdownlint-enable  MD033 -->

## Contributing

// TODO(user): Add detailed information on how you would like others to
contribute to this project

> **Note:** Run `make help` for more information on all potential `make`
> targets.

More information can be found via the
[Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)
