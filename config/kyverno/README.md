# Kyverno Configuration for GKM

This directory contains Kyverno-related configuration for GPU Kernel Manager (GKM).

## Directory Structure

- **`values.yaml`**: Helm values for deploying Kyverno with GPU tolerations (for Kind clusters with simulated GPUs)
- **`policies/`**: Kyverno ClusterPolicy definitions for GKMCache image verification

## Deployment

### Deploy Kyverno

```bash
# With GPU tolerations (for Kind/simulated GPU clusters)
make deploy-kyverno NO_GPU=true

# With default configuration (for real GPU clusters)
make deploy-kyverno NO_GPU=false
```

### Deploy Kyverno Policies

```bash
# Deploy image verification policies
make deploy-kyverno-policies
```

### Automatic Deployment

When using `make run-on-kind` with `KYVERNO_ENABLED=true` (default), both Kyverno and its policies are automatically deployed:

```bash
# Full deployment with Kyverno
make run-on-kind

# Deployment without Kyverno
make run-on-kind KYVERNO_ENABLED=false
```

## Policies

The policies in `policies/` directory enforce image signature verification for:

- **ClusterGKMCache**: Cluster-scoped GPU kernel caches
- **GKMCache**: Namespace-scoped GPU kernel caches

Both policies verify images from `quay.io/*` using keyless signatures with:
- **Issuer**: `https://token.actions.githubusercontent.com`
- **Subject**: `https://github.com/*/*`
- **Rekor**: `https://rekor.sigstore.dev`

The policies also automatically mutate image references to include digests for security.

## Environment Variable

The GKM operator reads `KYVERNO_VERIFICATION_ENABLED` environment variable to determine whether to enforce Kyverno verification in webhooks:

- **`true`** (default): Validates that Kyverno has verified and mutated images
- **`false`**: Skips Kyverno annotation checks (for development/testing only)

This is configured via `gkm.kyverno.enabled` in the ConfigMap.
