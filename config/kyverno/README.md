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

The policies in `policies/` directory enforce image signature verification using a **label-based approach** that supports both Cosign v2 and v3 formats:

### Available Policies

1. **gkmcache-policy-v2.yaml**: Verifies images signed with Cosign v2 (legacy format)
   - Matches: Resources with label `gkm.io/signature-format: cosign-v2`
   - Type: `Cosign`
   - Uses legacy `.sig` tag format

2. **gkmcache-policy-v3.yaml**: Verifies images signed with Cosign v3 (bundle format)
   - Matches: Resources with label `gkm.io/signature-format: cosign-v3`
   - Type: `SigstoreBundle`
   - Uses OCI 1.1 Referrers API with bundle artifacts

3. **clustergkmcache-policy.yaml**: For ClusterGKMCache resources
   - **Note**: Currently NOT functional due to Kyverno limitation with cluster-scoped resources

### How to Use

Add the `gkm.io/signature-format` label to your GKMCache resources:

```yaml
apiVersion: gkm.io/v1alpha1
kind: GKMCache
metadata:
  name: my-cache
  namespace: default
  labels:
    gkm.io/signature-format: cosign-v2  # or cosign-v3
spec:
  image: quay.io/example/my-image:tag
```

The policies automatically mutate image references to include digests for enhanced security.

For comprehensive documentation, see:
- [Image Verification Guide](../../docs/examples/kyverno-image-verification.md) - Complete guide with examples and troubleshooting
- [Kyverno Policies Overview](../../docs/examples/kyverno-policies.md) - Policy deployment and management

## Environment Variable

The GKM operator reads `KYVERNO_VERIFICATION_ENABLED` environment variable to determine whether to enforce Kyverno verification in webhooks:

- **`true`** (default): Validates that Kyverno has verified and mutated images
- **`false`**: Skips Kyverno annotation checks (for development/testing only)

This is configured via `gkm.kyverno.enabled` in the ConfigMap.
