# vLLM Examples

This directory contains example configurations and scripts for deploying and
benchmarking [vLLM](https://github.com/vllm-project/vllm) startup time on
a host with AMD ROCm GPUs using Kubernetes.

## Directory Structure

- `image/`
  - `Containerfile.vllm`: Container build file for vLLM.
  - `entrypoint-vllm.sh`: Entrypoint script for serving or benchmarking vLLM.
- `k8s/`
  - Example namespace and cache CRD.
  - Example Kubernetes Pod YAML files for vLLM deployments with and without
    pre-caching the Model Kernel.

## Usage

### 0. Create single node Kubeadm cluster

```bash
./../hack/setup-kubeadm-rocm.sh
```

> **Note**: you will also need to `make deploy` GKM into the cluster.

### 1. Build the vLLM Container Image

```bash
cd image
make build
```

> **Note**: It's recommended to push this image to a registry
so that it can be used by the pods in the Kubernetes Cluster.

### 2. Deploy on Kubernetes

Create a namespace for the cache.

```bash
kubectl create -f k8s/00-namespace.yaml
```

Generate a base64 representation for a huggingface token:

```bash
printf %s 'HF-TOKEN' | base64
```

Add this to 02-hf-token.yaml and create the associated secret:

```bash
kubectl create -f 01-hf-token.yaml
```

Create a cache custom resource:

```bash
kubectl create -f 20-llama-cache.yaml
```

Apply one of the pod specs:

```bash
kubectl create -f 30-llama-rocm-pod.yaml
# or
kubectl create -f 31-llama-rocm-cached-pod.yaml
```

## Model Caching

The `llama-rocm-cached-pod.yaml` example demonstrates how to mount a
persistent model cache using the GKM CSI driver.

## Customization

- Edit pod YAML files to change model, resources, or environment variables.
- Modify `image/entrypoint-vllm.sh` for advanced startup or benchmarking options.

## Requirements

- AMD ROCm-compatible GPU
- Kubernetes cluster with ROCm GPU support
- vLLM-compatible models

## References

- [vLLM Project](https://github.com/vllm-project/vllm)
- [ROCm Containers](https://rocm.blogs.amd.com/software-tools-optimization/vllm-container/README.html)
