# PV/PVC Storage Architecture Design

**OCTOET-1262**: Update GKM to use PV & PVC for Cache storage

**Status**: Design Proposal
**Date**: 2026-03-03
**Author**: Claude (via OCTOET-1262)

## Executive Summary

This document proposes a new storage architecture for GKM that replaces direct host path access with Persistent Volumes (PV) and Persistent Volume Claims (PVC). This change will:

1. **Reduce privilege requirements** - No more root access or privileged containers
2. **Enable cloud-native deployments** - Works in managed Kubernetes services
3. **Eliminate CSI Driver dependency** - Use native Kubernetes PVC mounting
4. **Align with KServe patterns** - Similar to how KServe handles model storage
5. **Improve security** - Standard Kubernetes RBAC controls

## Design Options Considered

### Option 1: One PVC per GKMCache (RECOMMENDED)

**Architecture**:
- Each GKMCache or ClusterGKMCache CR gets a dedicated PVC
- PVC name derived from cache name: `gkm-cache-{namespace}-{name}`
- Agent extracts cache to this PVC on each node
- Pods mount the PVC directly via standard Kubernetes volumeMounts

**Advantages**:
- ✅ Clear lifecycle - PVC deleted when GKMCache deleted
- ✅ Simple ownership model - 1:1 mapping
- ✅ Standard Kubernetes patterns
- ✅ Easy to track storage usage per cache
- ✅ Can use ReadOnlyMany after extraction
- ✅ Works with most storage providers

**Disadvantages**:
- ⚠️ Multiple PVCs in large deployments
- ⚠️ Small overhead per PVC

**Storage Class Requirements**:
- ReadWriteOnce (RWO) during extraction
- Can transition to ReadOnlyMany (ROX) after extraction completes
- Recommended size: Based on cache size (typically 1-50GB per cache)

### Option 2: One PVC per Node

**Architecture**:
- Each node gets a single PVC for all caches
- PVC name: `gkm-node-cache-{nodename}`
- Directory structure inside PVC mirrors current: `/{namespace}/{name}/{digest}/`
- Agent manages all caches on that node

**Advantages**:
- ✅ Fewer total PVCs
- ✅ Centralized storage per node

**Disadvantages**:
- ❌ Complex lifecycle - when to delete?
- ❌ Tied to node names (not portable)
- ❌ Harder to clean up individual caches
- ❌ Requires ReadWriteOnce per node (node affinity)
- ❌ No benefit over host path approach

### Option 3: Single Shared PVC

**Architecture**:
- One cluster-wide PVC for all caches
- Directory structure: `/{namespace}/{name}/{digest}/`
- All agents write to same PVC

**Advantages**:
- ✅ Minimal PVC count

**Disadvantages**:
- ❌ Requires ReadWriteMany (RWX) storage - not all providers support
- ❌ Performance bottleneck - all nodes access same volume
- ❌ Complex cleanup logic
- ❌ Single point of failure
- ❌ Harder to track per-cache usage

## Recommended Architecture: Option 1

### Overview

```
┌─────────────────────────────────────────────────────────────┐
│ GKMCache CR: cache-vllm-llama2                              │
│   namespace: ml-apps                                        │
│   image: quay.io/example/cache-vllm-llama2:latest          │
└───────────────────────┬─────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────────┐
│ PVC: gkm-cache-ml-apps-cache-vllm-llama2                   │
│   namespace: ml-apps                                        │
│   size: 10Gi                                                │
│   accessModes: [ReadWriteOnce] → [ReadOnlyMany]            │
└───────────────────────┬─────────────────────────────────────┘
                        │
        ┌───────────────┴───────────────┐
        ▼                               ▼
┌──────────────────┐           ┌──────────────────┐
│ Node A           │           │ Node B           │
│                  │           │                  │
│ GKM Agent        │           │ GKM Agent        │
│ - Mounts PVC RWO │           │ - Mounts PVC RWO │
│ - Extracts cache │           │ - Extracts cache │
│ - Updates status │           │ - Updates status │
└──────────────────┘           └──────────────────┘
        │                               │
        ▼                               ▼
┌──────────────────┐           ┌──────────────────┐
│ Workload Pod 1   │           │ Workload Pod 2   │
│ - Mounts PVC ROX │           │ - Mounts PVC ROX │
│ - Reads cache    │           │ - Reads cache    │
└──────────────────┘           └──────────────────┘
```

### PVC Naming Convention

**Namespace-scoped GKMCache**:
```
gkm-cache-{namespace}-{name}
```
Example: `gkm-cache-ml-apps-cache-vllm-llama2`

**Cluster-scoped ClusterGKMCache**:
```
gkm-clustercache-{name}
```
Example: `gkm-clustercache-cache-vllm-llama2`

### PVC Specification

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: gkm-cache-ml-apps-cache-vllm-llama2
  namespace: ml-apps
  labels:
    app.kubernetes.io/name: gkm
    app.kubernetes.io/component: cache-storage
    gkm.io/cache-name: cache-vllm-llama2
    gkm.io/cache-digest: sha256:abc123...
  annotations:
    gkm.io/cache-image: quay.io/example/cache-vllm-llama2:latest
    gkm.io/extraction-status: pending  # pending|extracting|completed|failed
spec:
  accessModes:
    - ReadWriteOnce  # During extraction
  resources:
    requests:
      storage: 10Gi  # Based on cache size metadata
  storageClassName: gkm-cache-storage  # Configurable
```

### Storage Class Configuration

Users should create a StorageClass for GKM caches:

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: gkm-cache-storage
provisioner: kubernetes.io/aws-ebs  # Or appropriate provisioner
parameters:
  type: gp3
  fsType: ext4
reclaimPolicy: Delete  # Or Retain for production
allowVolumeExpansion: true
volumeBindingMode: WaitForFirstConsumer  # Better node distribution
```

**Recommended Parameters**:
- **Provisioner**: Cloud-specific (AWS EBS, GCE PD, Azure Disk, etc.)
- **ReclaimPolicy**: `Delete` (PVC deleted when GKMCache deleted)
- **VolumeBindingMode**: `WaitForFirstConsumer` (binds to node where pod scheduled)
- **AllowVolumeExpansion**: `true` (allow growing cache storage)

## Component Changes

### 1. GKM Operator Changes

**New Responsibilities**:
- Create PVC when GKMCache CR is created
- Set PVC size based on image metadata (from cosign or default)
- Add finalizer to prevent PVC deletion before cache cleanup
- Delete PVC when GKMCache CR is deleted
- Update PVC annotations with extraction status

**New RBAC Permissions**:
```yaml
- apiGroups: [""]
  resources: [persistentvolumeclaims]
  verbs: [create, delete, get, list, patch, update, watch]
```

**PVC Creation Logic**:
```go
func (r *Reconciler) ensurePVC(ctx context.Context, gkmCache *GKMCache) error {
    pvcName := generatePVCName(gkmCache)
    pvc := &corev1.PersistentVolumeClaim{}

    // Check if PVC exists
    err := r.Get(ctx, types.NamespacedName{
        Name: pvcName,
        Namespace: gkmCache.Namespace,
    }, pvc)

    if err != nil && errors.IsNotFound(err) {
        // Create new PVC
        pvc = &corev1.PersistentVolumeClaim{
            ObjectMeta: metav1.ObjectMeta{
                Name: pvcName,
                Namespace: gkmCache.Namespace,
                Labels: map[string]string{
                    "app.kubernetes.io/name": "gkm",
                    "gkm.io/cache-name": gkmCache.Name,
                },
                Annotations: map[string]string{
                    "gkm.io/cache-image": gkmCache.Spec.Image,
                    "gkm.io/extraction-status": "pending",
                },
            },
            Spec: corev1.PersistentVolumeClaimSpec{
                AccessModes: []corev1.PersistentVolumeAccessMode{
                    corev1.ReadWriteOnce,
                },
                Resources: corev1.ResourceRequirements{
                    Requests: corev1.ResourceList{
                        corev1.ResourceStorage: calculateStorageSize(gkmCache),
                    },
                },
                StorageClassName: &gkmCache.Spec.StorageClassName,
            },
        }

        return r.Create(ctx, pvc)
    }

    return err
}
```

### 2. GKM Agent Changes

**New Responsibilities**:
- Mount PVC (ReadWriteOnce) to extract cache
- Extract cache using MCV to PVC-mounted directory
- Update PVC annotation when extraction completes
- Unmount PVC after extraction
- Remove privileged mode and root requirements

**Volume Mount Configuration**:
```yaml
# In Agent DaemonSet
volumeMounts:
  - name: cache-extraction
    mountPath: /mnt/gkm-cache
    # No more /var/lib/gkm needed!

volumes:
  # Dynamic volume based on PVC
  # Mounted only when extracting specific cache
```

**Extraction Process**:
1. Agent watches for GKMCache with `extraction-status: pending`
2. Creates a Pod with PVC mounted (or uses init container in Agent)
3. Runs MCV extraction to `/mnt/gkm-cache/`
4. Updates PVC annotation to `extraction-status: completed`
5. Updates GKMCacheNode status

**Security Context** (Reduced Privileges):
```yaml
securityContext:
  runAsUser: 1000  # Non-root!
  runAsGroup: 1000
  fsGroup: 1000
  privileged: false  # No longer privileged!
  allowPrivilegeEscalation: false
  capabilities:
    drop: [ALL]
  # No more CAP_DAC_OVERRIDE, CAP_FOWNER needed!
```

### 3. CSI Driver - REMOVED!

**Rationale**:
With PVCs, pods can mount caches directly using standard Kubernetes volumeMounts. The CSI driver is no longer needed!

**Before** (with CSI):
```yaml
volumes:
  - name: kernel-cache
    csi:
      driver: csi.gkm.io
      volumeAttributes:
        csi.gkm.io/GKMCache: cache-vllm-llama2
```

**After** (with PVC):
```yaml
volumes:
  - name: kernel-cache
    persistentVolumeClaim:
      claimName: gkm-cache-ml-apps-cache-vllm-llama2
      readOnly: true
```

**Benefits of Removing CSI Driver**:
- ✅ Simpler architecture
- ✅ Fewer components to maintain
- ✅ No CSI driver registration needed
- ✅ Standard Kubernetes patterns
- ✅ Better debugging (kubectl describe pvc)

### 4. Workload Pod Changes

**Pod Specification Update**:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: vllm-workload
  namespace: ml-apps
spec:
  containers:
  - name: vllm
    image: vllm/vllm-openai:latest
    volumeMounts:
    - name: kernel-cache
      mountPath: /root/.cache/vllm  # Or appropriate cache path
      readOnly: true

  volumes:
  - name: kernel-cache
    persistentVolumeClaim:
      claimName: gkm-cache-ml-apps-cache-vllm-llama2
      readOnly: true  # ReadOnly mount for workloads
```

**Migration Note**: Kyverno policies can auto-inject PVC based on labels/annotations if desired.

## Extraction Strategy

### Option A: Agent with Init Container (RECOMMENDED)

Agent DaemonSet includes an init container that handles extraction:

```yaml
spec:
  initContainers:
  - name: cache-extractor
    image: quay.io/gkm/agent:latest
    command: ["/bin/gkm-extract"]
    env:
    - name: CACHE_NAME
      value: cache-vllm-llama2
    - name: CACHE_NAMESPACE
      value: ml-apps
    volumeMounts:
    - name: cache-pvc
      mountPath: /mnt/cache

  volumes:
  - name: cache-pvc
    persistentVolumeClaim:
      claimName: gkm-cache-ml-apps-cache-vllm-llama2
```

### Option B: Separate Extraction Job

Operator creates a Job for extraction:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: extract-cache-vllm-llama2
  namespace: ml-apps
spec:
  template:
    spec:
      containers:
      - name: extractor
        image: quay.io/gkm/agent:latest
        command: ["/bin/gkm-extract"]
        volumeMounts:
        - name: cache
          mountPath: /mnt/cache
      volumes:
      - name: cache
        persistentVolumeClaim:
          claimName: gkm-cache-ml-apps-cache-vllm-llama2
      restartPolicy: OnFailure
```

**Recommendation**: Option A (Init Container) is simpler and leverages existing Agent image.

## Storage Size Management

### Size Determination

1. **From OCI Image Metadata** (Preferred):
   - Query image registry for compressed size
   - Add buffer (2x for decompression)
   - Minimum: 5Gi, Maximum: 100Gi

2. **From GKMCache Spec** (User Override):
   ```yaml
   spec:
     image: quay.io/example/cache:latest
     storage:
       size: 20Gi  # User-specified
   ```

3. **Default**: 10Gi

### Size Expansion

If cache extraction fails due to insufficient space:
1. Update PVC size (if StorageClass allows expansion)
2. Re-trigger extraction
3. Update GKMCache status with new size

## Lifecycle Management

### PVC Creation Flow

```
1. User creates GKMCache CR
   └─> GKM Operator validates image signature
       └─> Operator creates PVC
           └─> Operator updates GKMCache status (pvcName, size)
               └─> Agent detects new PVC
                   └─> Agent extracts cache to PVC
                       └─> Agent updates GKMCacheNode status
```

### PVC Deletion Flow

```
1. User deletes GKMCache CR
   └─> GKM Operator checks if cache in use (GKMCacheNode status)
       ├─> In use: Set finalizer, wait
       └─> Not in use: Delete PVC
           └─> Operator removes finalizer
               └─> GKMCache CR deleted
```

## Backward Compatibility

### Migration Path

For existing GKM deployments using host paths:

1. **Deploy PVC-enabled GKM** (side-by-side):
   - New GKMCaches use PVC
   - Old caches still on host paths

2. **Migrate existing caches**:
   - Create PVC for each existing cache
   - Copy data from `/var/lib/gkm` to PVC
   - Update cache references

3. **Remove old deployment**:
   - Delete old Agent DaemonSet
   - Clean up `/var/lib/gkm` host directories

### Feature Flag

Add a feature flag to support both modes during transition:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: gkm-config
data:
  gkm.storage.mode: "pvc"  # or "hostpath" for legacy
```

## Implementation Phases

### Phase 1: PVC Infrastructure
- [ ] Update CRDs to include storage spec
- [ ] Operator creates/manages PVCs
- [ ] Agent mounts PVCs (still with privileged for testing)

### Phase 2: Extraction
- [ ] Agent extracts to PVC instead of host path
- [ ] Update status tracking
- [ ] Test with real GPU kernels

### Phase 3: Privilege Reduction
- [ ] Remove root requirements from Agent
- [ ] Remove privileged mode
- [ ] Update security contexts

### Phase 4: CSI Removal
- [ ] Update workload pod specs to use PVC directly
- [ ] Deprecate CSI driver
- [ ] Remove CSI components

### Phase 5: Documentation & Migration
- [ ] Update all documentation
- [ ] Create migration guide
- [ ] Update examples

## Open Questions

1. **Multi-node extraction**: Should we extract once and share (RWX) or extract per-node (RWO)?
   - **Recommendation**: Extract once, use ROX for pods (requires RWX-capable storage or node-local copies)

2. **Storage class configuration**: Should GKM ship with default StorageClass?
   - **Recommendation**: No, users provide their own (documented examples for major clouds)

3. **Cache updates**: How to handle cache updates (new digest)?
   - **Recommendation**: Create new PVC with new digest, delete old when unused

4. **Garbage collection**: Who cleans up PVCs for deleted caches?
   - **Recommendation**: Operator handles via finalizers

## Success Criteria

- ✅ No root or privileged containers required
- ✅ Works in managed Kubernetes (EKS, GKE, AKS)
- ✅ CSI driver removed
- ✅ All tests pass with PVC storage
- ✅ Documentation updated
- ✅ Migration guide provided

## Next Steps

1. Update GKMCache and ClusterGKMCache CRDs with storage spec
2. Implement Operator PVC creation logic
3. Update Agent extraction to use PVC mounts
4. Create test suite for PVC functionality
5. Update documentation

---

**Document Status**: Design Proposal - Ready for Review
**Related Issue**: OCTOET-1262
