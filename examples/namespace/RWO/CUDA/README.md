# NVIDIA GPU Examples for GKM (ReadWriteOnce)

This directory contains examples for deploying GKM with NVIDIA GPU support
using ReadWriteOnce (RWO) access mode.

## Prerequisites

1. Kubernetes cluster with NVIDIA GPUs
2. NVIDIA GPU Operator or device plugin installed
3. Node Feature Discovery (NFD) installed and configured
4. GKM operator deployed in the cluster
5. A storage class that supports ReadWriteOnce volumes

## Storage Class Configuration

Before deploying, verify your storage class:

```bash
kubectl get sc
```

Update the `storageClassName` field in
[11-gkmcache.yaml](11-gkmcache.yaml) to match your cluster's storage
class.

## Deployment

### Option 1: Deploy All Resources

```bash
kubectl apply -f examples/namespace/RWO/CUDA/
```

### Option 2: Deploy Step by Step

1. Create the namespace:

   ```bash
   kubectl apply -f 10-namespace.yaml
   ```

2. Create the GKMCache resource:

   ```bash
   kubectl apply -f 11-gkmcache.yaml
   ```

3. Wait for the PVC to be created and bound:

   ```bash
   kubectl get pvc -n gkm-test-ns-nvidia-rwo-1 -w
   ```

4. Deploy a test workload (choose one):
   - DaemonSet: `kubectl apply -f 12-ds.yaml`
   - Pod: `kubectl apply -f 13-pod.yaml`

## Verification

Check the GKMCache status:

```bash
kubectl get gkmcache -n gkm-test-ns-nvidia-rwo-1
kubectl describe gkmcache vector-add-cache-cuda-rwo -n gkm-test-ns-nvidia-rwo-1
```

Check the PVC:

```bash
kubectl get pvc -n gkm-test-ns-nvidia-rwo-1
```

Check the extraction job:

```bash
kubectl get jobs -n gkm-test-ns-nvidia-rwo-1
kubectl get pods -n gkm-test-ns-nvidia-rwo-1
```

Check the test workload:

```bash
# For Pod
kubectl get pod gkm-test-nvidia-pod-1 -n gkm-test-ns-nvidia-rwo-1
kubectl logs gkm-test-nvidia-pod-1 -n gkm-test-ns-nvidia-rwo-1

# For DaemonSet
kubectl get ds gkm-test-nvidia-rwo-ds-1 -n gkm-test-ns-nvidia-rwo-1
kubectl get pods -n gkm-test-ns-nvidia-rwo-1 -l name=gkm-test-nvidia-rwo-ds-1
```

Verify the cache is mounted:

```bash
kubectl exec -it -n gkm-test-ns-nvidia-rwo-1 gkm-test-nvidia-pod-1 -- ls -la /cache
```

## Troubleshooting

### PVC Pending State

If the PVC remains in Pending state:

```bash
kubectl describe pvc vector-add-cache-cuda-rwo -n gkm-test-ns-nvidia-rwo-1
```

Common issues:

- Storage class not available or incorrect
- No nodes match the node selector
- Volume binding mode is `WaitForFirstConsumer` (PVC will bind when a
  pod using it is scheduled)

### Extraction Job Not Scheduling

Check the extraction job:

```bash
kubectl get jobs -n gkm-test-ns-nvidia-rwo-1
kubectl describe job <job-name> -n gkm-test-ns-nvidia-rwo-1
```

Check for pod scheduling issues:

```bash
kubectl get events -n gkm-test-ns-nvidia-rwo-1 --sort-by='.lastTimestamp'
```

### Pod Not Scheduling on GPU Nodes

If your cluster doesn't have NFD labels, you can either:

1. Install and configure NFD (recommended)
2. Remove the `affinity` section from the pod/daemonset specs and use a
   simpler node selector or label your GPU nodes manually

Example without NFD:

```yaml
nodeSelector:
  your-gpu-label: "true"  # Use whatever label identifies your GPU nodes
```

## Cleanup

```bash
kubectl delete -f examples/namespace/RWO/CUDA/
```
