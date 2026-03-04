# Current Host Storage Implementation Analysis

## Overview

This document analyzes the current GKM implementation that uses host directory storage for GPU Kernel Caches. This analysis is part of OCTOET-1262, which aims to convert GKM to use PV/PVC instead of host directories.

## Current Architecture

### Storage Locations

GKM currently uses two host directories:

1. **Cache Storage** (`/var/lib/gkm`):
   - Caches directory: `/var/lib/gkm/caches/<Namespace>/<Name>/<Digest>/<CacheFiles>`
   - Metadata file: `/var/lib/gkm/caches/<Namespace>/<Name>/cache.json`
   - Contains extracted GPU Kernel Cache from OCI images
   - Mounted with `Bidirectional` propagation

2. **Runtime/Usage Storage** (`/run/gkm`):
   - Usage directory: `/run/gkm/usage/<Namespace>/<Name>/<Digest>/usage.json`
   - Tracks which pods are using which caches
   - Mounted with `Bidirectional` propagation

### Components and Their Roles

#### 1. GKM Agent (DaemonSet)

**Purpose**: Extracts GPU Kernel Cache from OCI images to host directories

**Configuration** (`config/agent/gkm-agent.yaml`):
- Runs as: `runAsUser: 0` (root)
- Security: `privileged: true`
- Capabilities: `CAP_DAC_OVERRIDE`, `CAP_FOWNER`
- Volume mounts:
  - `/var/lib/gkm` - Cache storage
  - `/run/gkm` - Usage tracking
  - `/sys` - GPU detection (read-only)
  - `/dev` - Device access

**Key Functions** (`pkg/database/cache.go`):
- `ExtractCache()`: Calls MCV to extract cache from OCI image to `/var/lib/gkm/caches/<Namespace>/<Name>/<Digest>/`
- `GetExtractedCacheList()`: Walks `/var/lib/gkm/caches/` to discover extracted caches
- `RemoveCache()`: Deletes cache directories when no longer needed
- `writeCacheFile()`: Writes metadata to `cache.json`

**Directory Structure Created**:
```
/var/lib/gkm/
└── caches/
    ├── <Namespace>/
    │   └── <Name>/
    │       ├── cache.json          # Metadata file
    │       └── <Digest>/           # Actual cache content
    │           └── <cache-files>
    └── cluster-scoped/             # For ClusterGKMCache
        └── <Name>/
            ├── cache.json
            └── <Digest>/
                └── <cache-files>
```

#### 2. CSI Plugin (DaemonSet)

**Purpose**: Mounts extracted caches into pods using bind mounts

**Configuration** (`config/csi-plugin/gkm-csi-plugin.yaml`):
- Runs as: `runAsUser: 0` (root)
- Security: `privileged: true`
- Volume mounts:
  - `/var/lib/gkm` - Access to extracted caches
  - `/run/gkm` - Usage tracking
  - `/var/lib/kubelet/pods` - Pod mount directory (Bidirectional)

**Key Functions** (`pkg/gkm-csi-plugin/driver/node_server.go`):
- `NodePublishVolume()`:
  1. Reads cache metadata from `/var/lib/gkm/caches/<Namespace>/<Name>/cache.json`
  2. Builds source path: `/var/lib/gkm/caches/<Namespace>/<Name>/<Digest>/`
  3. Performs bind mount from source to pod's target path
  4. Records usage in `/run/gkm/usage/`

## Privilege Requirements

### Why Root Access is Required

1. **Host Directory Write Access**:
   - Agent needs to write to `/var/lib/gkm` on the host
   - Requires `CAP_DAC_OVERRIDE` to bypass file permission checks
   - Requires `CAP_FOWNER` to change file ownership

2. **Bind Mount Operations**:
   - CSI Plugin performs bind mounts from host directories into pod filesystems
   - Requires `privileged: true` for mount operations
   - Needs access to `/var/lib/kubelet/pods` with Bidirectional propagation

3. **Device Access**:
   - Agent needs to access `/dev` for GPU detection
   - Used by MCV to validate cache compatibility with GPUs

### Security Context Summary

**GKM Agent**:
```yaml
securityContext:
  runAsUser: 0
  privileged: true
  capabilities:
    add: ["CAP_DAC_OVERRIDE", "CAP_FOWNER"]
  seccompProfile:
    type: Unconfined
```

**CSI Plugin**:
```yaml
securityContext:
  runAsUser: 0
  privileged: true
  capabilities:
    add: ["NET_BIND_SERVICE"]
  allowPrivilegeEscalation: true
```

## Key Observations for PV/PVC Migration

### Advantages of Current Approach
1. Simple implementation - direct filesystem operations
2. No additional infrastructure needed
3. Fast access - no network overhead

### Disadvantages (Why We're Migrating)
1. **High Privileges**: Requires root and privileged containers
2. **Not Cloud-Native**: Host paths don't work well in cloud environments
3. **Node-Specific**: Caches tied to specific node filesystem
4. **Backup/Restore**: No standard way to backup/restore caches
5. **Storage Management**: No standard Kubernetes storage management
6. **CSI Driver Complexity**: Need full CSI driver just for bind mounts

### What Needs to Change for PV/PVC

#### Agent Changes
- Extract caches to PVC-mounted volumes instead of `/var/lib/gkm`
- Reduce/eliminate need for root privileges
- Remove host path dependencies
- Manage PVC lifecycle (creation, attachment, deletion)

#### CSI Plugin Changes
- **Option 1**: Eliminate CSI Plugin entirely
  - Use native Kubernetes PVC mounting in pod specs
  - No need for custom bind mounts

- **Option 2**: Simplify CSI Plugin
  - Just reference PVC instead of bind mounting from host
  - Still track usage but in different way

#### Storage Architecture
- One PVC per cache (namespace + name + digest)?
- Shared PVC per node for all caches?
- Dynamic provisioning vs static PVs?
- Storage class requirements (ReadWriteMany? ReadOnlyMany?)

#### Privilege Reduction
- Agent won't need:
  - Root access for host filesystem writes
  - CAP_DAC_OVERRIDE, CAP_FOWNER capabilities
  - Bidirectional mount propagation

- CSI Plugin (if kept):
  - May not need privileged mode
  - Standard CSI operations less privileged

## File Locations Reference

### Key Source Files
- `pkg/database/cache.go` - Cache extraction and management
- `pkg/database/files.go` - File operations helpers
- `internal/controller/gkm-agent/common.go` - Agent reconciliation logic
- `pkg/gkm-csi-plugin/driver/node_server.go` - CSI mount operations
- `pkg/utils/contants.go` - Configuration constants

### Key Configuration Files
- `config/agent/gkm-agent.yaml` - Agent DaemonSet definition
- `config/csi-plugin/gkm-csi-plugin.yaml` - CSI Plugin DaemonSet definition
- `config/rbac/gkm-agent/role.yaml` - Agent RBAC permissions

## Next Steps

1. Design PV/PVC architecture (see task #4)
2. Decide on CSI Plugin fate (remove or simplify)
3. Implement PVC-based extraction in Agent
4. Update manifests and RBAC
5. Create migration guide for existing deployments
