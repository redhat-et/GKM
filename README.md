# triton-kernel-manager-operator

## Description
Triton Kernel Manager is a software stack that aims to deploy, manage and
monitor Triton Kernels in a Kubernetes
cluster. It will use the utilities developed in
[TKDK](https://github.com/redhat-et/TKDK) to accomplish these goals.

## Getting Started

### Prerequisites
- go version v1.22.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy a cluster with a simulated GPU

To create a kind cluster with a simulated GPU

```sh
wget -qO- https://raw.githubusercontent.com/maryamtahhan/kind-gpu-sim/refs/heads/main/kind-gpu-sim.sh | bash -s create [rocm|nvidia]
```

To delete a kind cluster with a simulated GPU

```sh
wget -qO- https://raw.githubusercontent.com/maryamtahhan/kind-gpu-sim/refs/heads/main/kind-gpu-sim.sh | bash -s delete
```

#### To run the tkm-operator on this kind cluster

Start by building the operator controller and the operator controller image

```sh
$ make build docker-build
/home/mtahhan/TKM/bin/controller-gen rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
/home/mtahhan/TKM/bin/controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."
go fmt ./...
go vet ./...
go build -o bin/tkm-operator ./cmd/tkm-operator
go build -o bin/tkm-agent ./cmd/tkm-agent
docker build  -f Containerfile.tkm-operator -t quay.io/tkm/operator:latest .
[+] Building 21.8s (19/19) FINISHED                                                                                                                                                                                   docker:default
 => [internal] load build definition from Containerfile.tkm-operator                                                                                                                                                            0.0s
 => => transferring dockerfile: 1.35kB                                                                                                                                                                                          0.0s
 => [internal] load metadata for public.ecr.aws/docker/library/golang:1.24.3                                                                                                                                                   20.4s
 => [internal] load metadata for public.ecr.aws/docker/library/ubuntu:22.04                                                                                                                                                    20.4s
 => [internal] load .dockerignore                                                                                                                                                                                               0.0s
 => => transferring context: 219B                                                                                                                                                                                               0.0s
 => [stage-1 1/3] FROM public.ecr.aws/docker/library/ubuntu:22.04@sha256:67cadaff1dca187079fce41360d5a7eb6f7dcd3745e53c79ad5efd8563118240                                                                                       0.0s
 => [internal] load build context                                                                                                                                                                                               1.2s
 => => transferring context: 1.47MB                                                                                                                                                                                             1.2s
 => [builder  1/10] FROM public.ecr.aws/docker/library/golang:1.24.3@sha256:39d9e7d9c5d9c9e4baf0d8fff579f06d5032c0f4425cdec9e86732e8e4e374dc                                                                                    0.0s
 => CACHED [builder  2/10] WORKDIR /workspace                                                                                                                                                                                   0.0s
 => CACHED [builder  3/10] RUN apt-get update &&     apt-get install -y         libgpgme-dev         btrfs-progs         libbtrfs-dev         libgpgme11-dev         libseccomp-dev         pkg-config         build-essential  0.0s
 => CACHED [builder  4/10] COPY go.mod go.mod                                                                                                                                                                                   0.0s
 => CACHED [builder  5/10] COPY go.sum go.sum                                                                                                                                                                                   0.0s
 => CACHED [builder  6/10] COPY cmd/tkm-operator/main.go cmd/main.go                                                                                                                                                            0.0s
 => CACHED [builder  7/10] COPY api/ api/                                                                                                                                                                                       0.0s
 => CACHED [builder  8/10] COPY internal/controllers/ internal/controllers/                                                                                                                                                     0.0s
 => CACHED [builder  9/10] COPY vendor/ vendor/                                                                                                                                                                                 0.0s
 => CACHED [builder 10/10] RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -mod vendor -o /workspace/manager cmd/main.go                                                                                                     0.0s
 => CACHED [stage-1 2/3] COPY --from=builder /workspace/manager /manager                                                                                                                                                        0.0s
 => CACHED [stage-1 3/3] RUN apt-get update &&     apt-get install -y         libgpgme11         libbtrfs0         libseccomp2 &&     apt-get clean                                                                             0.0s
 => exporting to image                                                                                                                                                                                                          0.0s
 => => exporting layers                                                                                                                                                                                                         0.0s
 => => writing image sha256:32bdbfe0c24fe2e31a36fa7b72e4444f6e997b861c1b1062ee41bb7a15333918                                                                                                                                    0.0s
 => => naming to quay.io/tkm/operator:latest                                                                                                                                                                                    0.0s
docker build   -f Containerfile.tkm-agent -t quay.io/tkm/agent:latest .
[+] Building 97.9s (19/19) FINISHED                                                                                                                                                                                   docker:default
 => [internal] load build definition from Containerfile.tkm-agent                                                                                                                                                               0.0s
 => => transferring dockerfile: 1.26kB                                                                                                                                                                                          0.0s
 => [internal] load metadata for public.ecr.aws/docker/library/ubuntu:22.04                                                                                                                                                     0.1s
 => [internal] load metadata for public.ecr.aws/docker/library/golang:1.24.3                                                                                                                                                    0.1s
 => [internal] load .dockerignore                                                                                                                                                                                               0.0s
 => => transferring context: 219B                                                                                                                                                                                               0.0s
 => [builder  1/10] FROM public.ecr.aws/docker/library/golang:1.24.3@sha256:39d9e7d9c5d9c9e4baf0d8fff579f06d5032c0f4425cdec9e86732e8e4e374dc                                                                                    0.0s
 => CACHED [stage-1 1/3] FROM public.ecr.aws/docker/library/ubuntu:22.04@sha256:67cadaff1dca187079fce41360d5a7eb6f7dcd3745e53c79ad5efd8563118240                                                                                0.0s
 => [internal] load build context                                                                                                                                                                                               1.2s
 => => transferring context: 1.43MB                                                                                                                                                                                             1.2s
 => CACHED [builder  2/10] WORKDIR /workspace                                                                                                                                                                                   0.0s
 => CACHED [builder  3/10] RUN apt-get update &&     apt-get install -y         libgpgme-dev         btrfs-progs         libbtrfs-dev         libgpgme11-dev         libseccomp-dev         pkg-config         build-essential  0.0s
 => CACHED [builder  4/10] COPY go.mod go.mod                                                                                                                                                                                   0.0s
 => CACHED [builder  5/10] COPY go.sum go.sum                                                                                                                                                                                   0.0s
 => [builder  6/10] COPY cmd/tkm-agent/main.go cmd/main.go                                                                                                                                                                      0.0s
 => [builder  7/10] COPY api/ api/                                                                                                                                                                                              0.0s
 => [builder  8/10] COPY pkg/ pkg/                                                                                                                                                                                              0.0s
 => [builder  9/10] COPY vendor/ vendor/                                                                                                                                                                                        4.2s
 => [builder 10/10] RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -mod vendor -o /workspace/agent cmd/main.go                                                                                                             59.9s
 => [stage-1 2/3] COPY --from=builder /workspace/agent /agent                                                                                                                                                                   0.0s
 => [stage-1 3/3] RUN apt-get update &&     apt-get install -y         libgpgme11         libbtrfs0         libseccomp2 &&     apt-get clean                                                                                   29.6s
 => exporting to image                                                                                                                                                                                                          1.5s
 => => exporting layers                                                                                                                                                                                                         1.5s
 => => writing image sha256:9d7003c82e0af4fa6e1cfab425cb93b37b9603f0232ef132dbe83ad96eb35d46                                                                                                                                    0.0s
 => => naming to quay.io/tkm/agent:latest
```

Load the images onto the kind cluster:

```sh
$ kind load docker-image quay.io/tkm/operator --name kind-gpu-sim
Image: "quay.io/tkm/operator" with ID "sha256:b1f3befec93f296e1a64363de5114d83a49d9b888f1cc8652baad481bd51b743" not yet present on node "kind-gpu-sim-control-plane", loading...
Image: "quay.io/tkm/operator" with ID "sha256:b1f3befec93f296e1a64363de5114d83a49d9b888f1cc8652baad481bd51b743" not yet present on node "kind-gpu-sim-worker", loading...
Image: "quay.io/tkm/operator" with ID "sha256:b1f3befec93f296e1a64363de5114d83a49d9b888f1cc8652baad481bd51b743" not yet present on node "kind-gpu-sim-worker2", loading...
$ kind load docker-image quay.io/tkm/agent --name kind-gpu-sim
Image: "quay.io/tkm/agent" with ID "sha256:9d7003c82e0af4fa6e1cfab425cb93b37b9603f0232ef132dbe83ad96eb35d46" not yet present on node "kind-gpu-sim-worker", loading...
Image: "quay.io/tkm/agent" with ID "sha256:9d7003c82e0af4fa6e1cfab425cb93b37b9603f0232ef132dbe83ad96eb35d46" not yet present on node "kind-gpu-sim-worker2", loading...
Image: "quay.io/tkm/agent" with ID "sha256:9d7003c82e0af4fa6e1cfab425cb93b37b9603f0232ef132dbe83ad96eb35d46" not yet present on node "kind-gpu-sim-control-plane", loading...
```

Deploy the operator:

```sh
 make deploy
/home/mtahhan/TKM/bin/controller-gen rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
cd config/manager && /home/mtahhan/TKM/bin/kustomize edit set image controller=quay.io/tkm/operator:latest
/home/mtahhan/TKM/bin/kustomize build config/default | kubectl apply -f -
namespace/tkm-system created
customresourcedefinition.apiextensions.k8s.io/tritonkernelcacheclusters.tkm.io created
customresourcedefinition.apiextensions.k8s.io/tritonkernelcachenodestatuses.tkm.io created
customresourcedefinition.apiextensions.k8s.io/tritonkernelcaches.tkm.io created
serviceaccount/tkm-operator-controller-manager created
role.rbac.authorization.k8s.io/tkm-operator-leader-election-role created
clusterrole.rbac.authorization.k8s.io/tkm-operator-manager-role created
clusterrole.rbac.authorization.k8s.io/tkm-operator-metrics-auth-role created
clusterrole.rbac.authorization.k8s.io/tkm-operator-metrics-reader created
clusterrole.rbac.authorization.k8s.io/tkm-operator-tritonkernelcache-editor-role created
clusterrole.rbac.authorization.k8s.io/tkm-operator-tritonkernelcache-viewer-role created
clusterrole.rbac.authorization.k8s.io/tkm-operator-tritonkernelcachecluster-editor-role created
clusterrole.rbac.authorization.k8s.io/tkm-operator-tritonkernelcachecluster-viewer-role created
clusterrole.rbac.authorization.k8s.io/tkm-operator-tritonkernelcachenodestatus-editor-role created
clusterrole.rbac.authorization.k8s.io/tkm-operator-tritonkernelcachenodestatus-viewer-role created
rolebinding.rbac.authorization.k8s.io/tkm-operator-leader-election-rolebinding created
clusterrolebinding.rbac.authorization.k8s.io/tkm-operator-manager-rolebinding created
clusterrolebinding.rbac.authorization.k8s.io/tkm-operator-metrics-auth-rolebinding created
service/tkm-operator-controller-manager-metrics-service created
deployment.apps/tkm-operator-controller-manager created
```

Check the TKM operator pod:

```sh
$ kubectl get pods -A
NAMESPACE            NAME                                                 READY   STATUS    RESTARTS   AGE
kube-system          amdgpu-device-plugin-daemonset-qtnt9                 1/1     Running   0          6h48m
kube-system          amdgpu-device-plugin-daemonset-zcwkg                 1/1     Running   0          6h48m
kube-system          coredns-668d6bf9bc-2xskm                             1/1     Running   0          6h50m
kube-system          coredns-668d6bf9bc-h6jdb                             1/1     Running   0          6h50m
kube-system          etcd-kind-gpu-sim-control-plane                      1/1     Running   0          6h50m
kube-system          kindnet-2cqbb                                        1/1     Running   0          6h50m
kube-system          kindnet-bfjq6                                        1/1     Running   0          6h50m
kube-system          kindnet-n9xj4                                        1/1     Running   0          6h50m
kube-system          kube-apiserver-kind-gpu-sim-control-plane            1/1     Running   0          6h50m
kube-system          kube-controller-manager-kind-gpu-sim-control-plane   1/1     Running   0          6h50m
kube-system          kube-proxy-4wxtn                                     1/1     Running   0          6h50m
kube-system          kube-proxy-97jwg                                     1/1     Running   0          6h50m
kube-system          kube-proxy-qvt2j                                     1/1     Running   0          6h50m
kube-system          kube-scheduler-kind-gpu-sim-control-plane            1/1     Running   0          6h50m
local-path-storage   local-path-provisioner-7dc846544d-kjqqz              1/1     Running   0          6h50m
tkm-system           tkm-operator-controller-manager-6ffb68ddb-2fdjl      1/1     Running   0          28s
```

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

To build with podman:

```sh
make docker-build CONTAINER_TOOL=podman IMG=quay.io/tkm/operator:latest CONTAINER_FLAGS="--network=host"
```

To build with docker:

```sh
make docker-build docker-push IMG=quay.io/tkm/operator:latest
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=quay.io/tkm/operator:latest
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following are the steps to build the installer and distribute this project to users.

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=quay.io/tkm/operator:latest
```

NOTE: The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without
its dependencies.

2. Using the installer

Users can just run kubectl apply -f <URL for YAML BUNDLE> to install the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/triton-kernel-manager-operator/<tag or branch>/dist/install.yaml
```

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

