# GPU-kernel-manager-operator

<!-- markdownlint-disable  MD033 -->
<img src="docs/images/gkm-logo.png" alt="gkm" width="20%" height="auto">
<!-- markdownlint-enable  MD033 -->

## Description

GPU Kernel Manager (GKM) is a Kubernetes Operator that propagates GPU Kernel
Caches across Kubernetes Nodes, speeding up the startup time of pods in
Kubernetes clusters using GPU Kernels.

But what is a GPU Kernel Cache?

A GPU Kernel is the binary that is ultimately loaded on a GPU for execution.
Many frameworks like PyTorch run through multiple stages during the compilation
of a GPU Kernel.
One of the last steps is a Just-in-time (JIT) compilation where code is
compiled into GPU-specific machine code at runtime, rather than ahead of time.
This allows for runtime optimizations based on the specific detected hardware
and workload, creating highly specialized kernels that can be faster than
pre-compiled, general-purpose ones.
This produces hardware specific kernels at the cost of runtime compilation
time.
However, once the JIT compile has completed, the generated binary along with the
pre-stage artifacts are present in a local directory known as the GPU Kernel
Cache.

This is where GKM comes in.
This directory containing GPU Kernel Cache can be packaged into an OCI Image
using the utilities developed in [MCV](mcv/README.md) and pushed to an image
repository.
GKM pulls down the generated OCI Image from the repository and extracts it into
a PersistentVolumeCache (PVC).
The PVC can then be volume mounted as a directory in a workload pod.
As long as the node has the same GPU as the extracted GPU Kernel Cache was
generated on, the the workload is none the wiser and skips the JIT compilation,
decreasing the pod start-up time by up to half.

## Getting Started

If you already know what GKM is and how it works and just want to deploy it,
visit the [Getting Started Guide](docs/GettingStartedGuide.md).

## GKM Overview

To use GKM, the first step is to generate an OCI Image that contains a GPU
Kernel Cache.
GKM has a local tool called MCV.
MCV can be used to generate and extract the Kernel GPU Cache to and from an OCI
Image.
The extraction is handle automatically by GKM, but currently the generation is
manual.
To generate, `cd` into project directory and provide the `mcv` with the OCI
Image name and directory the GPU Kernel Cache is located.
GKM has some sample GPU Kernel caches in the
[./mcv/example/](https://github.com/redhat-et/GKM/tree/main/mcv/example)
directory that can be used to experiment with GKM.
This example also pushed the OCI Image to a repository and uses `cosign` to sign
the OCI Image.
Signing the image is option but recommended in production.
See [MCV](mcv/README.md) for details on packaging OCI Images and `cosign`.

```bash
cd ~/src/GKM/
mcv -c -i quay.io/$QUAY_USER/vector-add-cache:rocm -d ./mcv/example/vector-add-cache-rocm
podman push quay.io/$QUAY_USER/vector-add-cache:rocm
cosign sign -y quay.io/$QUAY_USER/vector-add-cache@sha256:<digest>
```

Next, create a GKMCache CR that will reference the desired OCI Image (there are
several more sample GKMCache and ClusterCache CRs in
[./examples/](https://github.com/redhat-et/GKM/tree/main/examples)):

```yaml
apiVersion: gkm.io/v1alpha1
kind: GKMCache
metadata:
  name: vector-add-cache-rocm-v2-rox
  namespace: gkm-test-ns-rox-1
spec:
  image: quay.io/$QUAY_USER/cache-examples:vector-add-cache:rocm
```

GKM will extract the OCI Image into a PVC that can then be mounted as a volume
in a Pod.
Here is a sample Pod spec.
Notice the PVC Claim contains the name of the GKMCache CR.

```yaml
kind: Pod
apiVersion: v1
metadata:
  name: gkm-test-ns-rox-pod-1
  namespace: gkm-test-ns-rox-1
spec:
  containers:
    - name: test
      image: quay.io/fedora/fedora-minimal
      imagePullPolicy: IfNotPresent
      command: [sleep, 365d]
      volumeMounts:
        - name: kernel-volume
          mountPath: /cache
  volumes:
    - name: kernel-volume
      persistentVolumeClaim:
        claimName: vector-add-cache-rocm-v2-rox
```

When the pod comes up, the extracted GPU Kernel Cache will be in the directory
specified by the Volume Mount.
Adjust the `mountPath:` as needed by the application running in the pod.

This is a simple example.
See [Deployment Options](docs/DeploymentOptions.md) for details on how to
customize the deployment to meet your needs.

## Documentation

Below are links to documents the go into more depth about different GKM based
topics:

- [Getting Started Guide](docs/GettingStartedGuide.md):
  List of prerequisites to building, instructions on building GKM and
  description of how to deploy GKM.
- [Deployment Options](docs/DeploymentOptions.md):
  Details on the GKM Custom Resource Definitions, and how to tailor the
  different optional fields for different environments.

12345678901234567890123456789012345678901234567890123456789012345678901234567890

Below are links to documents on more advanced topics:

- [Kyverno Integration](config/kyverno/README.md):
  Image signature verification with Kyverno, a third party webhook used by GKM
  to verify image signing.
- [Webhook Configuration](config/webhook/README.md):
  GKM webhook configuration details.

### Image Signature Verification

GKM supports image signature verification using Kyverno for namespace-scoped
`GKMCache` resources. Use the `gkm.io/signature-format` label to specify the
signature format:

- **`gkm.io/signature-format: cosign-v2`** - For images signed with Cosign v2
  (legacy `.sig` tag format)
- **`gkm.io/signature-format: cosign-v3`** - For images signed with Cosign v3
  (OCI 1.1 bundle format)

Example:

```yaml
apiVersion: gkm.io/v1alpha1
kind: GKMCache
metadata:
  name: my-cache
  namespace: my-namespace
spec:
  image: quay.io/example/my-image:tag
```

For detailed information about image verification, see:

- [Kyverno Image Verification Guide](docs/examples/kyverno-image-verification.md)
- [Kyverno Policies Documentation](docs/examples/kyverno-policies.md)

**Note:** `ClusterGKMCache` resources have built-in signature verification
and automatically detect both Cosign v2 and v3 formats without requiring
labels.

## Contributing

// TODO(user): Add detailed information on how you would like others to
contribute to this project

> **Note:** Run `make help` for more information on all potential `make`
> targets.

More information can be found via the
[Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)
