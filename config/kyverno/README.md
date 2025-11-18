# Kyverno Integration for GKM

This directory contains Kyverno configuration for the GKM (GPU Kernel Manager) project.

## Overview

Kyverno is deployed with GPU tolerations to run on the Kind GPU simulation cluster. It provides image verification and digest mutation for ClusterGKMCache custom resources.

## Files

- `values.yaml` - Helm values for Kyverno deployment with GPU tolerations
- `verify-clustergkmcache-images.yaml` - Sample policy for ClusterGKMCache image verification

## Usage

### Deploy Kyverno

```bash
make deploy-kyverno
```

This will:
1. Install Kyverno using Helm
2. Configure all controllers with GPU tolerations and node selectors
3. Wait for Kyverno to be ready

### Apply the ClusterGKMCache Image Verification Policy

```bash
kubectl apply -f config/kyverno/verify-clustergkmcache-images.yaml
```

### Undeploy Kyverno

```bash
make undeploy-kyverno
```

## Image Verification Policy

The sample policy (`verify-clustergkmcache-images.yaml`) does the following:

1. **Matches** all `ClusterGKMCache` resources
2. **Verifies** images from `quay.io/gkm/cache-examples:*`
3. **Validates** signatures using GitHub OIDC (keyless signing)
4. **Mutates** image references to use digest instead of tags (e.g., `@sha256:...`)

### Customizing the Policy

To customize for your specific use case:

1. **Change the image pattern**:
   ```yaml
   imageReferences:
   - "your-registry/your-repo:*"
   ```

2. **Update the GitHub subject** (for specific org/repo):
   ```yaml
   subject: "https://github.com/your-org/your-repo/.github/workflows/*"
   ```

3. **Use static keys instead of keyless**:
   ```yaml
   attestors:
   - count: 1
     entries:
     - keys:
         publicKeys: |-
           -----BEGIN PUBLIC KEY-----
           <your-public-key>
           -----END PUBLIC KEY-----
   ```

4. **Change validation action** (Audit mode for testing):
   ```yaml
   spec:
     validationFailureAction: Audit  # or Enforce
   ```

## Testing

1. Deploy Kyverno:
   ```bash
   make deploy-kyverno
   ```

2. Apply the verification policy:
   ```bash
   kubectl apply -f config/kyverno/verify-clustergkmcache-images.yaml
   ```

3. Create a ClusterGKMCache with an unsigned image (should fail):
   ```bash
   kubectl apply -f - <<EOF
   apiVersion: gkm.io/v1alpha1
   kind: ClusterGKMCache
   metadata:
     name: test-unsigned
   spec:
     image: quay.io/gkm/cache-examples:unsigned-image
   EOF
   ```

4. Create a ClusterGKMCache with a signed image (should succeed and mutate to digest):
   ```bash
   kubectl apply -f - <<EOF
   apiVersion: gkm.io/v1alpha1
   kind: ClusterGKMCache
   metadata:
     name: test-signed
   spec:
     image: quay.io/gkm/cache-examples:vector-add-cache-rocm
   EOF
   ```

5. Check that the image was mutated to use digest:
   ```bash
   kubectl get clustergkmcache test-signed -o yaml | grep image:
   ```

## Troubleshooting

### Check Kyverno pod status
```bash
kubectl get pods -n kyverno
```

### View Kyverno logs
```bash
kubectl logs -n kyverno -l app.kubernetes.io/component=admission-controller
```

### Check policy status
```bash
kubectl get clusterpolicy verify-clustergkmcache-images -o yaml
```

### View policy reports
```bash
kubectl get policyreport -A
```
