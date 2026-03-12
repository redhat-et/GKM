# Node Feature Discovery (NFD) Configuration

This directory contains the configuration for deploying [Node Feature Discovery](https://kubernetes-sigs.github.io/node-feature-discovery/) to automatically label nodes with hardware features, particularly GPU vendor information.

## What is NFD?

Node Feature Discovery is a Kubernetes add-on that detects hardware features available on each node and advertises those features using node labels.

For GKM, NFD automatically labels nodes with PCI device vendor IDs, enabling GPU-specific agent deployment:

- **NVIDIA GPUs**: `feature.node.kubernetes.io/pci-10de.present=true` (vendor ID: 0x10de)
- **AMD GPUs**: `feature.node.kubernetes.io/pci-1002.present=true` (vendor ID: 0x1002)

## Deployment

### Deploy NFD
```bash
kubectl apply -k config/nfd
```

### Verify NFD is Running
```bash
# Check NFD pods
kubectl get pods -n node-feature-discovery

# Expected output:
# NAME                          READY   STATUS    RESTARTS   AGE
# nfd-master-xxxxx              1/1     Running   0          1m
# nfd-worker-xxxxx              1/1     Running   0          1m
# nfd-worker-yyyyy              1/1     Running   0          1m
```

### Check Node Labels
```bash
# View all NFD labels on a node
kubectl get node <node-name> -o json | jq '.metadata.labels | with_entries(select(.key | startswith("feature.node.kubernetes.io")))'

# Check for GPU vendor labels specifically
kubectl get nodes -L feature.node.kubernetes.io/pci-10de.present,feature.node.kubernetes.io/pci-1002.present
```

## How It Works

1. **NFD Master**: Runs as a deployment, manages feature labeling
2. **NFD Worker**: Runs as a DaemonSet on each node, detects features
3. **Worker scans PCI devices** and creates labels for vendor IDs
4. **Labels are applied** to nodes automatically

## Configuration

### Default Configuration

The default NFD configuration (via [kustomization.yaml](kustomization.yaml)) deploys NFD from the official upstream repository.

### Custom Configuration (Optional)

To customize NFD behavior, uncomment the patch in `kustomization.yaml` and modify [nfd-worker-conf.yaml](nfd-worker-conf.yaml):

```yaml
# In kustomization.yaml
patchesStrategicMerge:
  - nfd-worker-conf.yaml
```

The custom configuration enables:
- **PCI device detection** with focus on display controllers (GPUs)
- **Vendor ID labeling** for automatic GPU vendor identification
- **Configurable scan interval** (default: 60s)

## Verification

### Manual PCI Device Check

On each node, you can manually verify GPU devices:

```bash
# List all VGA/Display controllers
lspci | grep -i vga

# Show vendor IDs numerically
lspci -n | grep -E "0300|0302"

# Example outputs:
# NVIDIA: 01:00.0 0300: 10de:1b80 (rev a1)
# AMD:    01:00.0 0300: 1002:67df (rev c7)
```

### Verify Label Creation

```bash
# List nodes with NVIDIA GPUs
kubectl get nodes -l feature.node.kubernetes.io/pci-10de.present=true

# List nodes with AMD GPUs
kubectl get nodes -l feature.node.kubernetes.io/pci-1002.present=true
```

## Integration with GKM Agents

NFD labels are used by GKM agent DaemonSets to deploy GPU-specific agents:

```yaml
# From config/agent/gkm-agent-nvidia.yaml
nodeSelector:
  feature.node.kubernetes.io/pci-10de.present: "true"

# From config/agent/gkm-agent-amd.yaml
nodeSelector:
  feature.node.kubernetes.io/pci-1002.present: "true"
```

This ensures:
- NVIDIA agents only run on NVIDIA GPU nodes
- AMD agents only run on AMD GPU nodes
- No manual node labeling required

## Troubleshooting

### NFD Not Detecting GPUs

1. **Check NFD worker logs:**
   ```bash
   kubectl logs -n node-feature-discovery -l app=nfd-worker
   ```

2. **Verify PCI devices are present:**
   ```bash
   # SSH to node
   lspci | grep -i vga
   ```

3. **Check NFD configuration:**
   ```bash
   kubectl get cm -n node-feature-discovery nfd-worker-conf -o yaml
   ```

### Labels Not Appearing

1. **Restart NFD worker:**
   ```bash
   kubectl rollout restart daemonset/nfd-worker -n node-feature-discovery
   ```

2. **Force re-labeling:**
   ```bash
   kubectl delete pod -n node-feature-discovery -l app=nfd-worker
   ```

### Wrong Vendor ID

Common PCI vendor IDs:
- **NVIDIA**: `10de`
- **AMD**: `1002`
- **Intel**: `8086`

If using a different GPU vendor, update the node selectors in the agent DaemonSets.

## Resources

- [NFD GitHub](https://github.com/kubernetes-sigs/node-feature-discovery)
- [NFD Documentation](https://kubernetes-sigs.github.io/node-feature-discovery/)
- [PCI Vendor IDs Database](https://pci-ids.ucw.cz/)

## Files

- [kustomization.yaml](kustomization.yaml) - Main NFD deployment configuration
- [nfd-worker-conf.yaml](nfd-worker-conf.yaml) - Optional custom NFD worker configuration
