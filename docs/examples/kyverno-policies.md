# Kyverno Policies for GKM

## Policy Location Change

The Kyverno ClusterPolicy definitions have been moved from the `examples/` directories to a centralized location for better organization and deployment control.

### Previous Locations

- **Cluster-scoped policy**: `examples/cluster/00-kyverno.yaml`
- **Namespace-scoped policy**: `examples/namespace/00-kyverno.yaml`

### New Location

Both policies are now located in:

```
config/kyverno/policies/
├── clustergkmcache-policy.yaml  # Policy for ClusterGKMCache resources
├── gkmcache-policy.yaml          # Policy for GKMCache resources
└── kustomization.yaml            # Kustomize configuration for unified deployment
```

## Deployment

### Deploy Kyverno Policies Only

If you have Kyverno already installed and just need to deploy the GKM policies:

```bash
make deploy-kyverno-policies
```

### Deploy Everything (Kyverno + Policies)

For a complete setup including Kyverno and the GKM policies:

```bash
# With Kyverno enabled (default)
make run-on-kind KYVERNO_ENABLED=true

# Without Kyverno
make run-on-kind KYVERNO_ENABLED=false
```

### Undeploy Kyverno Policies

To remove only the GKM policies:

```bash
make undeploy-kyverno-policies
```

## Policy Details

Both policies enforce image signature verification for GKM cache resources:

- **Cluster-scoped**: `clustergkmcache-policy.yaml` applies to `ClusterGKMCache` resources
- **Namespace-scoped**: `gkmcache-policy.yaml` applies to `GKMCache` resources

### Verification Requirements

Both policies verify images from `quay.io/*` using keyless signatures with:

- **Issuer**: `https://token.actions.githubusercontent.com`
- **Subject**: `https://github.com/*/*`
- **Rekor transparency log**: `https://rekor.sigstore.dev`

### Image Mutation

The policies automatically mutate image references to include digests for enhanced security:

- **Before**: `quay.io/gkm/cache-examples:vector-add-cache-rocm`
- **After**: `quay.io/gkm/cache-examples:vector-add-cache-rocm@sha256:bf6f7ea60274882031ad81434aa9c9ac0e4ff280cd1513db239dbbd705b6511c`

## Runtime Control

The GKM operator honors the `KYVERNO_VERIFICATION_ENABLED` environment variable:

- **`true`** (default): Validates that Kyverno has verified and mutated images
- **`false`**: Skips Kyverno annotation checks (for development/testing only)

This is configured via `gkm.kyverno.enabled` in the ConfigMap during deployment.

## Additional Resources

- [Kyverno Configuration Documentation](../../config/kyverno/README.md)
- [GKM Examples](../../examples/)
