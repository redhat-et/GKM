# GPU-kernel-manager-operator

<!-- markdownlint-disable  MD033 -->
<img src="docs/images/gkm-logo.png" alt="gkm" width="20%" height="auto">
<!-- markdownlint-enable  MD033 -->

## Description

GPU Kernel Manager is a software stack that aims to deploy, manage and
monitor GPU Kernels in a Kubernetes cluster.
It will use the utilities developed in
[TKDK](https://github.com/redhat-et/TKDK) to accomplish these goals.

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
NAME                                     READY   STATUS    RESTARTS   AGE
gkm-agent-7ggr2                          1/1     Running   0          74m
gkm-agent-mc9h6                          1/1     Running   0          74m
gkm-controller-manager-c7b6f4f87-9zgns   3/3     Running   0          74m
gkm-csi-node-nd6qn                       2/2     Running   0          74m
gkm-csi-node-tkkc8                       2/2     Running   0          74m
gkm-test-pod                             1/1     Running   0          64m
```

To delete a `kind` cluster with a simulated GPU:

```sh
make destroy-kind
```

### Install Test Pod Using GKM

There is an example yaml that creates a `GKMCache` custom resource (CR)
instance which points an OCI Image with GPU Kernel Cache. Example:

```yaml
apiVersion: gkm.io/v1alpha1
kind: GKMCache
metadata:
  name: flash-attention-rocm
spec:
  image: quay.io/mtahhan/flash-attention-rocm:latest
```

The example yaml also includes a test pod that references the `GKMCache` CR
instance. Example:

```yaml
kind: Pod
apiVersion: v1
metadata:
  name: gkm-test-pod
  namespace: gkm-system
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
        csi.gkm.io/GKMCache: flash-attention-rocm
```

Pod Spec Highlights:

- The `volumes:` named `kernel-volume` references the GKM CSI driver via
  `driver: csi.gkm.io` and references the GKM Cache CR via
  `csi.gkm.io/GKMCache: flash-attention-rocm`.
- The `volumeMounts:` named `kernel-volume` maps the GPU Kernel Cache to the
  directory `/cache` within the pod.
- There is a Node Selector `gkm-test-node: "true"`.
  The `make run-on-kind` command adds this label to node `kind-gpu-sim-worker`.
  This is help monitor logs while applying the pod.

> **NOTE:** GKM is still a work in progress and the Agent and Operator are
> deployed but aren't coded up to reconcile the CRDs.
> To test, the OCI Image needs to be manually extracted to the node.
> This is a temporary step.

Because of the Node Selector, the test pod will be launched on node
`kind-gpu-sim-worker`. Determine the CSI Plugin instant running on this node:

<!-- markdownlint-disable  MD013 -->
<!-- Temporarily disable MD013 - Line length to keep the block formatting  -->
```sh
$ kubectl get pods -n gkm-system -o wide
NAME                                     READY   STATUS    RESTARTS   AGE    IP           NODE
gkm-agent-7ggr2                          1/1     Running   0          102m   10.244.1.6   kind-gpu-sim-worker
gkm-agent-mc9h6                          1/1     Running   0          102m   10.244.2.3   kind-gpu-sim-worker2
gkm-controller-manager-c7b6f4f87-9zgns   3/3     Running   0          102m   10.244.0.5   kind-gpu-sim-control-plane
gkm-csi-node-nd6qn                       2/2     Running   0          102m   10.89.0.67   kind-gpu-sim-worker2
gkm-csi-node-tkkc8                       2/2     Running   0          102m   10.89.0.66   kind-gpu-sim-worker  <-- HERE
```
<!-- markdownlint-enable  MD013 -->

Exec into the CSI Plugin on node `kind-gpu-sim-worker` and use the GKM Agent Stub.
This calls `TCV` to extracts the OCI Image into a directory on the Node.

<!-- markdownlint-disable  MD013 -->
<!-- Temporarily disable MD013 - Line length to keep the block formatting  -->
```sh
$ kubectl exec -it -n gkm-system -c gkm-csi-node-plugin gkm-csi-node-tkkc8 -- sh
sh-5.2#
sh-5.2# gkm-agent-stub -load -image quay.io/mtahhan/flash-attention-rocm:latest -crdName flash-attention-rocm
2025/07/15 18:25:03 Response from gRPC server's LoadKernelImage function: Load Image Request Succeeded
sh-5.2#
sh-5.2# ls /run/gkm/caches/cluster-scoped/flash-attention-rocm/
c4d45c651d6ac181a78d8d2f3ead424b8b8f07dd23dc3de0a99f425d8a633fc6  c880dcbe2ffa9f4c96a3c5ce87fbf0b61a04ee4c46f96ee728d2d1efb65133f6  e0a7f37fbe7bb678faad9ffe683ba5d53d92645aefa5b62195bc2683b9971485
```
<!-- markdownlint-enable  MD013 -->

Now the example yaml can be applied:

```sh
kubectl apply -f examples/flash-attention-rocm.yaml
```

The `gkm-test-pod` should be running and the cache should be volume mounted in
the pod:

<!-- markdownlint-disable  MD013 -->
<!-- Temporarily disable MD013 - Line length to keep the block formatting  -->
```sh
$ kubectl get pods -n gkm-system
NAME                                     READY   STATUS    RESTARTS   AGE
gkm-agent-7ggr2                          1/1     Running   0          74m
gkm-agent-mc9h6                          1/1     Running   0          74m
gkm-controller-manager-c7b6f4f87-9zgns   3/3     Running   0          74m
gkm-csi-node-nd6qn                       2/2     Running   0          74m
gkm-csi-node-tkkc8                       2/2     Running   0          74m
gkm-test-pod                             1/1     Running   0          64m

kubectl exec -it -n gkm-system gkm-test-pod -- sh
sh-5.2# ls /cache/
c4d45c651d6ac181a78d8d2f3ead424b8b8f07dd23dc3de0a99f425d8a633fc6  c880dcbe2ffa9f4c96a3c5ce87fbf0b61a04ee4c46f96ee728d2d1efb65133f6  e0a7f37fbe7bb678faad9ffe683ba5d53d92645aefa5b62195bc2683b9971485
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
