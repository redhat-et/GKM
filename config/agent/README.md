# Multi-GPU Agent Deployment

This directory contains configuration for deploying GPU-specific GKM agents that support both NVIDIA and AMD GPUs in heterogeneous clusters.

## Overview

GKM now supports deploying different agent containers based on the GPU hardware present on each node:

- **`gkm-agent-nvidia`**: For nodes with NVIDIA GPUs
- **`gkm-agent-amd`**: For nodes with AMD ROCm GPUs
- **`gkm-agent`**: Legacy generic agent (deprecated)

## Architecture

Each GPU-specific agent uses:
- **Different base images** with appropriate GPU runtime libraries
- **Node selectors** to deploy only on compatible hardware
- **Automatic node labeling** via Node Feature Discovery (NFD)

## Prerequisites

### 1. Node Feature Discovery (NFD)

NFD must be deployed to automatically label nodes with their PCI device information:

```bash
# Deploy NFD
kubectl apply -k config/nfd

# Verify NFD is running
kubectl get pods -n node-feature-discovery

# Check node labels (should see pci-* labels)
kubectl get nodes -o json | jq '.items[].metadata.labels' | grep pci
```

NFD will automatically add labels like:
- `feature.node.kubernetes.io/pci-10de.present=true` (NVIDIA, vendor ID: 0x10de)
- `feature.node.kubernetes.io/pci-1002.present=true` (AMD, vendor ID: 0x1002)

### 2. GPU Device Plugins

Ensure appropriate GPU device plugins are installed:

**For NVIDIA:**
```bash
kubectl apply -f https://raw.githubusercontent.com/NVIDIA/k8s-device-plugin/v0.17.0/deployments/static/nvidia-device-plugin.yml
```

**For AMD:**
```bash
kubectl apply -f https://raw.githubusercontent.com/ROCm/k8s-device-plugin/master/k8s-ds-amdgpu-dp.yaml
```

## Building GPU-Specific Agent Images

### Build All GPU Agents
```bash
make build-image-agents-gpu
```

### Build Individual Agents
```bash
# NVIDIA agent
make build-image-agent-nvidia

# AMD agent
make build-image-agent-amd
```

### Push Images to Registry
```bash
# Set your registry
export QUAY_USER=your-org

# Push GPU-specific agents
make push-images-agents-gpu
```

## Deployment

### Deploy with Kustomize
```bash
kubectl apply -k config/agent
```

This will deploy:
- `agent-nvidia` DaemonSet → Only on nodes with `feature.node.kubernetes.io/pci-10de.present=true`
- `agent-amd` DaemonSet → Only on nodes with `feature.node.kubernetes.io/pci-1002.present=true`

### Verify Deployment
```bash
# Check which agents are running
kubectl get ds -n gkm-system

# Check agent pods and their node placement
kubectl get pods -n gkm-system -o wide

# Verify agents are on correct nodes
kubectl get pods -n gkm-system -l gpu-vendor=nvidia -o wide
kubectl get pods -n gkm-system -l gpu-vendor=amd -o wide
```

## Containerfiles

### NVIDIA Agent ([Containerfile.gkm-agent-nvidia](../../Containerfile.gkm-agent-nvidia))
- Base image: `nvcr.io/nvidia/cuda:12.6.3-base-ubuntu24.04`
- Includes: NVIDIA CUDA runtime with NVML libraries
- Requires: NVIDIA driver on host

### AMD Agent ([Containerfile.gkm-agent-amd](../../Containerfile.gkm-agent-amd))
- Base image: `ubuntu:24.04`
- Includes: ROCm libraries (`amd-smi-lib`, `rocm-smi-lib`)
- Requires: AMD GPU driver on host

## Node Selectors

The DaemonSets use PCI vendor ID-based node selectors:

```yaml
# NVIDIA nodes
nodeSelector:
  feature.node.kubernetes.io/pci-10de.present: "true"

# AMD nodes
nodeSelector:
  feature.node.kubernetes.io/pci-1002.present: "true"
```

## Hybrid GPU Clusters

In clusters with both NVIDIA and AMD nodes:

1. **NFD labels all nodes** with their PCI device information
2. **NVIDIA agent** deploys only to NVIDIA nodes
3. **AMD agent** deploys only to AMD nodes
4. **Operator** works with whichever agent is present on each node

## Troubleshooting

### NFD Not Labeling Nodes

```bash
# Check NFD worker logs
kubectl logs -n node-feature-discovery -l app=nfd-worker

# Manually verify PCI devices
lspci | grep -i vga
lspci -n | grep -E "0300|0302"
```

### Agent Not Scheduling

```bash
# Check node labels
kubectl describe node <node-name> | grep feature.node.kubernetes.io/pci

# Check DaemonSet events
kubectl describe ds agent-nvidia -n gkm-system
kubectl describe ds agent-amd -n gkm-system
```

### GPU Libraries Not Found

```bash
# Check NVIDIA driver
nvidia-smi

# Check AMD driver
rocm-smi

# Verify libraries in container
kubectl exec -it <agent-pod> -n gkm-system -- ls -la /usr/lib/x86_64-linux-gnu/ | grep -E "nvidia|amd"
```

## Migration from Generic Agent

To migrate from the legacy generic agent:

1. Deploy NFD: `kubectl apply -k config/nfd`
2. Build GPU-specific agents: `make build-image-agents-gpu`
3. Update manifests to use new agent DaemonSets
4. Deploy: `kubectl apply -k config/agent`
5. Remove old generic agent: `kubectl delete ds agent -n gkm-system`

## Related Files

- [gkm-agent-nvidia.yaml](gkm-agent-nvidia.yaml) - NVIDIA DaemonSet
- [gkm-agent-amd.yaml](gkm-agent-amd.yaml) - AMD DaemonSet
- [kustomization.yaml](kustomization.yaml) - Kustomize configuration
- [../../Containerfile.gkm-agent-nvidia](../../Containerfile.gkm-agent-nvidia) - NVIDIA Containerfile
- [../../Containerfile.gkm-agent-amd](../../Containerfile.gkm-agent-amd) - AMD Containerfile
