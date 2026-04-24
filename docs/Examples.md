# Examples Directory

How GKM is used will depend on the GPUs in the Kubernetes cluster, what storage
backend are supported in the cluster, and the namespaces of the workloads
consuming the GPU Kernel Cache.
[Deployment Options](DeploymentOptions.md) describes in detail many of these
options.
Quick summary is that the two major factors that dictate deployment are:

- **Namespace of the GPU Kernel Cache:**
  If a given GPU Kernel Cache will only be deployed in a single Kubernetes
  Namespace, then the `GKMCache` should be used.
  If a given GPU Kernel Cache will be deployed in multiple Kubernetes Namespaces,
  then the `ClusterGKMCache` should be used.
- **Cluster Storage Backend:**
  If the Kubernetes StorageClass backend supports an Access Mode of `ReadOnlyMany`
  then the storage backend can distribute extracted GPU Kernel Cache to each
  node to each node.
  If the Kubernetes StorageClass backend does not support an Access Mode of
  `ReadOnlyMany`, GKM needs to handle the distribution of the extracted GPU Kernel
  Cache to each node.
  If this is the case, certain concession need to be made.

To handle these different deployment options, the Examples directory is using a
tool called `kustomize` along with a shell script to tailor a set of base yaml
files to work in multiple environments.

Here are the set of options the examples supports:

- **rox** vs **rwo**: The access mode of `ReadOnlyMany` or `ReadWriteOnly`.
  - `rox` implies Pods will be used.
  - `rwo` implies DaemonSets will be used.
- **namespace** vs **cluster**: The scope.
  - **namespace** implies GKMCache will be used.
  - **cluster** implies ClusterGKMCache will be used. Also implies two namespaces
     will be created.
- **rocm** vs **cuda**: The GPU type.
- **v2** vs **v3**: The Cosign version used to sign the OCI Image.
- **kind** vs **nfd**: The environment the example is being deployed in.
  - **kind** has some special restrictions that are being managed.
  - **nfd** implies Node Feature Discovery is being used in real hardware (not
    KIND) and nodes are labeled with detect GPU hardware.

The object names, namespaces and generated output filenames are appended with
a suffix generated from these options.
For example, the GKMCache instance may be named something like:
`gkm-test-obj-rwo-namespace-rocm-v2`

## Directory Layout

A set of base yaml files are created, one for each object that will be created.
For a GKM use case, the following objects are needed:

- **Namespace** (two Namespaces if cluster scoped)
- **GKMCache** (namespace scoped) or **ClusterGKMCache** (cluster scoped)
- **Pod** (for ReadOnlyMany (rox)) or **DaemonSet** (for ReadWriteOnce (rwo))

So the yaml files for these basic objects is laid out as follows.
The `kustomization.yaml` file is a `kustomize` file that lists the set of files
the tool should include.

```sh
$ tree examples/base/
examples/base/
├── access
│   ├── rox
│   │   ├── kustomization.yaml
│   │   ├── pod-1.yaml
│   │   ├── pod-2.yaml
│   │   └── pod-3.yaml
│   └── rwo
│       ├── ds-1.yaml
│       ├── ds-2.yaml
│       ├── ds-3.yaml
│       └── kustomization.yaml
├── common
│   ├── kustomization.yaml
│   └── namespace-1.env
└── scope
    ├── cluster
    │   ├── clustergkmcache.yaml
    │   ├── kustomization.yaml
    │   └── namespace-2.env
    └── namespace
        ├── gkmcache.yaml
        └── kustomization.yaml
```

The base objects are just the bare bones yaml for the object.
Different deployments require additional fields in the object to be set.
For example, a deployment in a KIND Cluster requires an Init-Container be added to
the GKMCache/ClusterGKMCache and Pod/DaemonSet that sets the permissions of the
PVC VolumeMount so the workload can access the contents.
If using the Node Feature Discovery (NFD), the GKMCache/ClusterGKMCache and
Pod/DaemonSet objects need Affinity set so they are deployed on the proper node
based on the labels set by NFD.

The variants directory contains `kustomize` patches, that mutate base yaml files
with the desired field updates.
A basic `kustomize` patch looks something like:

```yaml
- target:
    kind: Pod
    name: gkm-test-pod-1
  patch: |-
    - op: replace
      path: /metadata/namespace
      value: gkm-test-ns-1-rox-cluster-rocm-v2
```

This says for the Pod object with the name "gkm-test-pod-1", replace the value
at "metadata.namespace" with the value of "gkm-test-ns-1-rox-cluster-rocm-v2".
To make the examples more useful, the goal is to deploy more than one instance
at a given time.
So the object names and the namespaces need to be dynamic, based on the input
deployment settings.
`kustomize` does not manage dynamic naming, so the examples use a script
(`examples/generate-files.sh`) with multiple `sed` commands to adjust the updated
fields as necessary.
So before the `sed` command runs, the above patch, which is stored in a
`kustomization.env` file, looks like:

```yaml
- target:
    kind: Pod
    name: gkm-test-pod-1
  patch: |-
    - op: replace
      path: /metadata/namespace
      value: NAMESPACE_1
```

`kustomize` uses the `kustomization.yaml` files as mentioned above.
So `examples/generate-files.sh` runs `sed` commands on the `kustomization.env`
files and pipes the output to `kustomization.yaml` files for `kustomize` to
consume.
The patches are stored as follows:

```sh
$ tree examples/variants/
examples/variants/
├── access
│   ├── rox
│   │   └── kustomization.env
│   └── rwo
│       └── kustomization.env
└── scope
    ├── cluster
    │   └── kustomization.env
    └── namespace
        └── kustomization.env
```

Finally, not all the files are used in every deployment.
So `kustomize` uses the `kustomization.yaml` in the `examples/overlays` directory
which includes the set of files to include.
To control the order the objects are generated, the `kustomization.yaml` file in
the `examples/overlays` is broken into two files.
These files are generated by the `examples/generate-files.sh` script, so neither
of these files are checked into the repo.

```sh
$ tree examples/overlays/
examples/overlays/
├── access
└── scope
```

Once the `examples/generate-files.sh` script is run, the output looks something
like the following:

```sh
$ cat examples/overlays/scope/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- ../../base/common
- ../../base/scope/namespace

components:
- ../../variants/scope/namespace

nameSuffix: -rwo-namespace-rocm-v3
```

```sh
$ cat examples/overlays/access/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- ../../base/access/rwo

components:
- ../../variants/access/rwo

nameSuffix: -rwo-namespace-rocm-v3
```

None of the files that are generated by the `examples/generate-files.sh` script
are checked into the repo.
The `examples/.gitignore` keeps the generated files as being flagged as changed.
There is also a `examples/cleanup-files.sh` script that will delete all the
generated yaml files if needed.

## Deploy Examples from Makefile

The Makefile has a few pre-canned deployment options.
If these don't fit a given deployment, visit the next section,
[Custom Example Deployments](#custom-example-deployments), for ways to customize
the deployment.

### Makefile Deploy on KIND

The KIND Cluster deployment is using a simulated ROCm GPU.
To deploy the examples in a KIND Cluster, run:

```sh
make deploy-examples-kind
```

This runs the `examples/generate-files.sh` script four times, with the
following parameters for eacg run:

- `rwo` - `namespace` - `rocm` - `v2` - `kind`
- `rwo` - `cluster` - `rocm` - `v3` - `kind`
- `rox` - `namespace` - `rocm` - `v3` - `kind`
- `rox` - `cluster` - `rocm` - `v2` - `kind`

The KIND cluster is unique in that even though the backend storage does not
support `ReadOnlyMany`, because KIND is running each node in it's own
container on the same server, each Node can see the extracted cache, so it's
like `ReadOnlyMany`.
So both `rwo` and `rox` are supported.

To unwind the deployment, run:

```sh
make undeploy-examples-kind
```

### Makefile Deploy on NFD Cluster

GKM works on clusters with Node Feature Discovery (NFD) deployed.
NFD is a Kubernetes Operator that automatically detects GPU hardware and adds
labels to nodes with details about which GPUs were detected.
GKM works in conjunction with this to only deploy GKM Agents built with drivers
for the detected GPU hardware.
This allows GKM Agent image sizes to be much smaller by not carrying around
unused drivers.

When creating examples, these labels also allow Affinity and Tolerations to be
set on GKMCache/ClusterGKMCache instances and Pod/DaemonSet instances.
To this end, the following make commands deploy the examples for given GPU
hardware when running with NFD:

```sh
make deploy-examples-nfd-cuda
```

This runs the `examples/generate-files.sh` script twice, with the following
parameters:

- `rwo` - `namespace` - `cuda` - `v2` - `nfd`
- `rwo` - `cluster` - `cuda` - `v3` - `nfd`

And:

```sh
make deploy-examples-nfd-rocm
```

This runs the `examples/generate-files.sh` script twice, with the following
parameters:

- `rwo` - `namespace` - `rocm` - `v2` - `nfd`
- `rwo` - `cluster` - `rocm` - `v3` - `nfd`

To unwind the deployments, run either:

```sh
make undeploy-examples-nfd-cuda
```

Or:

```sh
make undeploy-examples-nfd-rocm
```

## Custom Example Deployments

There are too many deployment scenarios to have Makefile cover all of them.
The `examples/generate-files.sh` script can be called directly.
The input parameters are in fixed locations and all are required except
ENVIRONMENT, which is optional.

The help text associated with the script describes how is should be used:

```sh
$ ./examples/generate-files.sh --help

./generate-files.sh will generate a yaml file from the base files
   and the input which can then be applied to a Kubernetes cluster.
   Generated filename is printed from script and files can be found
   in the "output/" directory.
Syntax:
  ./generate-files.sh <SCOPE> <ACCESS> <GPU> <COSIGN-VERSION> [<ENVIRONMENT>]
Where:
  <SCOPE> is "rox" or "rwo" and required.
  <ACCESS> is "namespace" or "cluster" and required.
  <GPU> is "cuda" or "rocm" and required.
  <COSIGN-VERSION> is "v2" or "v3" and required.
  <ENVIRONMENT> is "kind" or "nfd" and optional.
Samples:
  ./generate-files.sh rwo namespace rocm v3 kind
  ./generate-files.sh rox cluster cuda v2 nfd
  ./generate-files.sh rox namespace rocm v3
```

Then run the script with the parameters as needed:

```sh
$ ./generate-files.sh rwo namespace rocm v3 kind
output/rwo-namespace-rocm-v3-kind.yaml
```

Then apply the output file to Kubernetes cluster when ready:

```sh
kubectl apply -f output/rwo-namespace-rocm-v3-kind.yaml
```

`examples/generate-files.sh` script can also be controlled with some Environment
Variables.

- `DEBUG`: Script will also print the generated output file before exiting.
  Helpful for examining the yaml before applying to Kubernetes cluster.
- `CUSTOM_AFFINITY`: The location of a file containing the JSON for custom
  Affinity that will be applied to GKMCache/ClusterGKMCache and Pod/DaemonSet.
  This is used in a `kustomize` patch.
  Example is provided in `examples/patch/affinity-nfd-cuda.txt`.
- `CUSTOM_TOLERATION`: The location of a file containing the JSON for custom
  Toleration that will be applied to GKMCache/ClusterGKMCache and Pod/DaemonSet.
  This is used in a `kustomize` patch.
  Example is provided in `examples/patch/toleration-nfd-cuda.txt`.
- `CUSTOM_NODE_SELECTOR_1`-`CUSTOM_NODE_SELECTOR_3`: The location of a file
  containing the JSON for custom NodeSelector that will be applied to
  Pod/DaemonSet.
  CUSTOM_NODE_SELECTOR_1 applies to Pod-1/DaemonSet-1, CUSTOM_NODE_SELECTOR_2
  applies to Pod-2/DaemonSet-2, and CUSTOM_NODE_SELECTOR_3 applies to
  Pod-3/DaemonSet-3,
  These are used in `kustomize` patches.
  Example is provided in `examples/patch/node-selector-kind-true.txt`.

**NOTE:** The spacing in the custom files is important.
The content of the files are being piped directly into the generated
`kustomization.yaml` files that contain the patches applied to `kustomize`.
If an error occurs while running `examples/generate-files.sh`, like the
following, it is probably a spacing problem.

<!-- markdownlint-disable  MD013 -->
<!-- Temporarily disable MD013 - Line length to keep the block formatting  -->
```sh
$ ./generate-files.sh rox cluster rocm v3 kind
Error: accumulating components: accumulateDirectory: "recursed accumulation of path '/home/bmcfall/src/GKM/examples/variants/scope/cluster': trouble configuring builtin PatchTransformer with config: `\npatch: |-\n  # Overwrite the OCI Image in the ClusterGKMCache with the CUDA/ROCm and V2/V3 tag. Whole image, not just tag overwritten\n  - op: replace\n    path: /spec/image\n    value: quay.io/gkm/cache-examples:vector-add-cache-rocm\n\n  # Add Cosign Version Label to ClusterGKMCache\n  - op: add\n    path: /metadata/labels\n    value: {}\n  - op: add\n    path: /metadata/labels/gkm.io~1signature-format\n    value: cosign-v3\n\n  # Overwrite the namespaces to the `spec.workloadNamespaces` slice in the ClusterGKMCache\n  - op: replace\n    path: /spec/workloadNamespaces/0\n    value: gkm-test-ns-1-rox-cluster-rocm-v3\n  - op: replace\n    path: /spec/workloadNamespaces/1\n    value: gkm-test-ns-2-rox-cluster-rocm-v3- op: add path: /spec/accessModes/- value: ReadOnlyMany\ntarget:\n  kind: ClusterGKMCache\n  name: gkm-test-obj\n`: unable to parse SM or JSON patch from [patch: \"# Overwrite the OCI Image in the ClusterGKMCache with the CUDA/ROCm and V2/V3 tag. Whole image, not just tag overwritten\\n- op: replace\\n  path: /spec/image\\n  value: quay.io/gkm/cache-examples:vector-add-cache-rocm\\n\\n# Add Cosign Version Label to ClusterGKMCache\\n- op: add\\n  path: /metadata/labels\\n  value: {}\\n- op: add\\n  path: /metadata/labels/gkm.io~1signature-format\\n  value: cosign-v3\\n\\n# Overwrite the namespaces to the `spec.workloadNamespaces` slice in the ClusterGKMCache\\n- op: replace\\n  path: /spec/workloadNamespaces/0\\n  value: gkm-test-ns-1-rox-cluster-rocm-v3\\n- op: replace\\n  path: /spec/workloadNamespaces/1\\n  value: gkm-test-ns-2-rox-cluster-rocm-v3- op: add path: /spec/accessModes/- value: ReadOnlyMany\"]"
```
<!-- markdownlint-enable  MD013 -->

Try to use the files already in the `examples/patch/` directory as examples.
If error occurs, the script probably generated an invalid `kustomization.yaml`
and the error was when `kustomize` tried to process it.
Examine the generated `kustomization.yaml` files in `examples/variants/`.

- `variants/access/rox/kustomization.yaml`
- `variants/access/rwo/kustomization.yaml`
- `variants/scope/cluster/kustomization.yaml`
- `variants/scope/namespace/kustomization.yaml`

Below is an example of running the script with some of the control variables:

<!-- markdownlint-disable  MD013 -->
<!-- Temporarily disable MD013 - Line length to keep the block formatting  -->
```sh
CUSTOM_AFFINITY=patch/affinity-nfd-rocm.txt DEBUG=true ./generate-files.sh rwo namespace rocm v3
```
<!-- markdownlint-enable  MD013 -->
