# Kyverno Image Verification for GKM

This document explains how to use Kyverno's image verification feature with
GKM resources to verify container image signatures using Cosign v2 (legacy
format) and Cosign v3 (bundle format).

## Overview

GKM integrates with Kyverno to provide cryptographic verification of container
images before they are processed. This ensures that only trusted, signed images
are used for kernel module caching.

### Key Features

- **Dual Format Support**: Works with both Cosign v2 (legacy `.sig` tags) and
  Cosign v3 (OCI 1.1 bundle format)
- **Label-Based Selection**: Uses resource labels to determine which
  verification method to apply
- **Automatic Digest Mutation**: Kyverno automatically adds image digests to
  verified images
- **Webhook Ordering**: Ensures Kyverno runs before GKM webhook to provide
  verified digests

## Cosign v2 vs v3

### Cosign v2 (Legacy Format)

- **Signature Storage**: Uses separate `.sig` tag alongside the image (e.g.,
  `image:tag` and `image:sha256-<hash>.sig`)
- **Type**: `Cosign`
- **Use Case**: Images signed with older Cosign versions or legacy workflows
- **Example**: `quay.io/gkm/cache-examples:vector-add-cache-rocm`

### Cosign v3 (Bundle Format)

- **Signature Storage**: Uses OCI 1.1 Referrers API with
  `application/vnd.dev.sigstore.bundle` artifact type
- **Type**: `SigstoreBundle`
- **Use Case**: Images signed with Cosign v3+ using the new bundle format
- **Example**: `quay.io/mtahhan/vllm-flash-attention:rocm`

> **Note**: Kyverno v1.13.0+ is required for Cosign v3 bundle format support.

## Configuration

### 1. Label Your GKMCache Resources

Add the `gkm.io/signature-format` label to specify which verification method
to use:

**For Cosign v2 (legacy format):**

```yaml
apiVersion: gkm.io/v1alpha1
kind: GKMCache
metadata:
  name: vector-add-cache-rocm-1
  namespace: gkm-test-ns-scoped-1
  labels:
    gkm.io/signature-format: cosign-v2
spec:
  image: quay.io/gkm/cache-examples:vector-add-cache-rocm
```

**For Cosign v3 (bundle format):**

```yaml
apiVersion: gkm.io/v1alpha1
kind: GKMCache
metadata:
  name: vllm-flash-attention-1
  namespace: gkm-test-ns-scoped-1
  labels:
    gkm.io/signature-format: cosign-v3
spec:
  image: quay.io/mtahhan/vllm-flash-attention:rocm
```

### 2. Deploy Kyverno Policies

#### Deploy Kyverno GKM Policies Only

GKM provides separate ClusterPolicy definitions for each verification format:

```bash
# Deploy both Cosign v2 and v3 policies
kubectl apply -f config/kyverno/policies/gkmcache-policy-v2.yaml
kubectl apply -f config/kyverno/policies/gkmcache-policy-v3.yaml
```

Or use the unified deployment:

```bash
make deploy-kyverno-policies
```

#### Deploy Everything (Kyverno + Policies)

For a complete setup including Kyverno and the GKM policies:

```bash
# With Kyverno enabled (default)
make run-on-kind KYVERNO_ENABLED=true

# Without Kyverno
make run-on-kind KYVERNO_ENABLED=false
```

#### Undeploy Kyverno GKM Policies

To remove only the GKM policies:

```bash
make undeploy-kyverno-policies
```

## Policy Details

Policies are located in:

```text
config/kyverno/policies/
├── clustergkmcache-policy.yaml  # Policy for ClusterGKMCache resources
├── gkmcache-policy-v2.yaml      # Policy for GKMCache (cosign v2)
├── gkmcache-policy-v3.yaml      # Policy for GKMCache (cosign v3)
└── kustomization.yaml           # Kustomize configuration
```

### Cosign v2 Policy (verify-gkmcache-images-v2)

This policy verifies images signed with Cosign v2 using GitHub Actions keyless
signatures:

```yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: verify-gkmcache-images-v2
spec:
  validationFailureAction: Enforce
  background: true
  webhookTimeoutSeconds: 30
  rules:
    - name: verify-v2-legacy-images
      match:
        any:
          - resources:
              kinds:
                - GKMCache
              selector:
                matchLabels:
                  gkm.io/signature-format: cosign-v2
      imageExtractors:
        GKMCache:
          - path: /spec/image
      verifyImages:
        - imageReferences:
            - "*"
          type: Cosign
          mutateDigest: true
          attestors:
            - count: 1
              entries:
                - keyless:
                    issuer: https://token.actions.githubusercontent.com
                    subjectRegExp: "https://github.com/.*"
                    rekor:
                      url: https://rekor.sigstore.dev
```

**Key Configuration:**

- **selector**: Matches resources with label
  `gkm.io/signature-format: cosign-v2`
- **type**: `Cosign` for legacy format
- **issuer**: GitHub Actions OIDC token issuer
- **subjectRegExp**: Matches any GitHub repository workflow
- **mutateDigest**: Automatically adds digest to image reference

### Cosign v3 Policy (verify-gkmcache-images-v3)

This policy verifies images signed with Cosign v3 using the new bundle format:

```yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: verify-gkmcache-images-v3
spec:
  validationFailureAction: Enforce
  background: true
  webhookTimeoutSeconds: 30
  rules:
    - name: verify-v3-bundle-images
      match:
        any:
          - resources:
              kinds:
                - GKMCache
              selector:
                matchLabels:
                  gkm.io/signature-format: cosign-v3
      imageExtractors:
        GKMCache:
          - path: /spec/image
      verifyImages:
        - imageReferences:
            - "*"
          type: SigstoreBundle
          mutateDigest: true
          attestors:
            - count: 1
              entries:
                - keyless:
                    issuer: https://github.com/login/oauth
                    subject: mtahhan@redhat.com
                    rekor:
                      url: https://rekor.sigstore.dev
```

**Key Configuration:**

- **selector**: Matches resources with label
  `gkm.io/signature-format: cosign-v3`
- **type**: `SigstoreBundle` for bundle format
- **issuer**: GitHub OAuth token issuer (different from Actions)
- **subject**: Specific user email for keyless signing

## How It Works

### 1. Image Verification Flow

```text
User creates GKMCache with label
         ↓
Kyverno webhook (mutate.kyverno.svc-fail) runs first
         ↓
Kyverno extracts image from /spec/image
         ↓
Kyverno verifies signature based on label selector
         ↓
Kyverno mutates image to include digest
         ↓
Kyverno adds annotation (kyverno.io/verify-images)
         ↓
GKM webhook (z-mgkmcache.kb.io) runs second
         ↓
GKM validates Kyverno annotation exists
         ↓
GKM extracts digest from mutated image
         ↓
GKM processes kernel cache with verified image
```

### 2. Webhook Execution Order

The webhook execution order is critical for proper verification:

- **Kyverno webhook**: `mutate.kyverno.svc-fail` (runs first alphabetically)
- **GKM webhook**: `z-mgkmcache.kb.io` (runs second due to `z-` prefix)

The GKM webhook uses `reinvocationPolicy=Never` to prevent multiple
invocations.

### 3. Image Digest Mutation

When verification succeeds, Kyverno automatically mutates the image reference:

**Before verification:**

```yaml
spec:
  image: quay.io/gkm/cache-examples:vector-add-cache-rocm
```

**After verification:**

```yaml
spec:
  image: quay.io/gkm/cache-examples:vector-add-cache-rocm@sha256:bf6f7ea60274882031ad81434aa9c9ac0e4ff280cd1513db239dbbd705b6511c
```

### 4. Verification Annotation

Kyverno adds an annotation to track verification status:

```yaml
metadata:
  annotations:
    kyverno.io/verify-images: '...pass'
```

The GKM webhook validates this annotation exists when
`KYVERNO_VERIFICATION_ENABLED=true`.

## Choosing the Right Format

### Use Cosign v2 (label `cosign-v2`) when

- Images are signed with Cosign v1.x or v2.x
- Your signing workflow uses legacy `.sig` tag format
- You're using GitHub Actions workflows with `cosign-installer@v2` or older

### Use Cosign v3 (label `cosign-v3`) when

- Images are signed with Cosign v3.x+
- Your signing workflow uses the new bundle format with `--bundle` flag
- You want to leverage OCI 1.1 Referrers API for better registry integration

## Troubleshooting

### Problem: "no signatures found"

**Cause**: The image was signed with a different format than specified in the
policy.

**Solution**:

1. Check how the image was signed (v2 legacy or v3 bundle)
2. Use the correct label: `cosign-v2` or `cosign-v3`
3. Verify the signature exists:

   ```bash
   # For v2 (legacy)
   cosign verify --insecure-ignore-tlog=true \
     --certificate-identity-regexp=".*" \
     --certificate-oidc-issuer-regexp=".*" \
     quay.io/gkm/cache-examples:vector-add-cache-rocm

   # For v3 (bundle)
   cosign verify --insecure-ignore-tlog=true \
     --certificate-identity-regexp=".*" \
     --certificate-oidc-issuer-regexp=".*" \
     quay.io/mtahhan/vllm-flash-attention:rocm
   ```

### Problem: "kyverno.io/verify-images must be set by kyverno"

**Cause**: The GKM webhook is running before Kyverno.

**Solution**: Ensure webhook names are configured correctly:

- Kyverno: `mutate.kyverno.svc-fail`
- GKM: `z-mgkmcache.kb.io` (with `z-` prefix)

### Problem: Kyverno not processing images

**Causes**:

1. Incorrect imageExtractors configuration
2. Label selector not matching resource
3. Subject/issuer pattern not matching signature

**Solution**:

1. Verify imageExtractors uses simple path: `path: /spec/image`
2. Confirm resource has correct label: `gkm.io/signature-format: cosign-v2`
   or `cosign-v3`
3. Check subject pattern matches your signing workflow
4. Review Kyverno logs:
   `kubectl logs -n kyverno -l app.kubernetes.io/component=admission-controller`

### Problem: Empty digest in GKM webhook logs

**Cause**: Kyverno failed to verify or mutate the image.

**Solution**:

1. Check Kyverno logs for verification errors
2. Verify the signature exists and is valid
3. Ensure the issuer and subject match your signing workflow
4. Confirm the label matches the policy selector

## Examples

### Complete Example Files

See the following files in the repository:

- [examples/namespace/11-gkmcache.yaml](../../examples/namespace/11-gkmcache.yaml)
  \- Cosign v2 example
- [examples/namespace/12-gkmcache-cosign-v3.yaml](../../examples/namespace/12-gkmcache-cosign-v3.yaml)
  \- Cosign v3 example
- [config/kyverno/policies/gkmcache-policy-v2.yaml](../../config/kyverno/policies/gkmcache-policy-v2.yaml)
  \- v2 policy
- [config/kyverno/policies/gkmcache-policy-v3.yaml](../../config/kyverno/policies/gkmcache-policy-v3.yaml)
  \- v3 policy

### Testing Verification

1. Create a test namespace:

   ```bash
   kubectl apply -f examples/namespace/10-namespace.yaml
   ```

2. Deploy Kyverno policies:

   ```bash
   kubectl apply -f config/kyverno/policies/gkmcache-policy-v2.yaml
   kubectl apply -f config/kyverno/policies/gkmcache-policy-v3.yaml
   ```

3. Create a GKMCache resource:

   ```bash
   kubectl apply -f examples/namespace/11-gkmcache.yaml
   ```

4. Verify the image was mutated:

   ```bash
   kubectl get gkmcache vector-add-cache-rocm-1 -n gkm-test-ns-scoped-1 -o yaml | grep image:
   ```

   Expected output shows digest:

   ```text
   image: quay.io/gkm/cache-examples:vector-add-cache-rocm@sha256:bf6f7ea60274882031ad81434aa9c9ac0e4ff280cd1513db239dbbd705b6511c
   ```

5. Check the Kyverno annotation:

   ```bash
   kubectl get gkmcache vector-add-cache-rocm-1 -n gkm-test-ns-scoped-1 -o jsonpath='{.metadata.annotations.kyverno\.io/verify-images}'
   ```

   Expected output:

   ```text
   ...pass
   ```

## Runtime Control

The GKM operator honors the `KYVERNO_VERIFICATION_ENABLED` environment
variable:

- **`true`** (default): Validates that Kyverno has verified and mutated images
- **`false`**: Skips Kyverno annotation checks (for development/testing only)

This is configured via `gkm.kyverno.enabled` in the ConfigMap during
deployment.

## Limitations

### Cluster-Scoped Resources

**Current Status**: Kyverno's `verifyImages` feature does **NOT** currently
support cluster-scoped custom resources (e.g., `ClusterGKMCache`).

**Root Cause**: Kyverno's webhook configuration uses `namespaceSelector`, which
only applies to namespaced resources. Cluster-scoped resources are not
processed by the webhook.

**Workaround**: Only use `GKMCache` (namespaced) resources with Kyverno
verification. For `ClusterGKMCache` resources, signature verification is not
currently supported.

**Future**: This limitation is being tracked in the upstream Kyverno project.

## Additional Resources

- [Kyverno Configuration Documentation](../../config/kyverno/README.md)
- [Kyverno Policies Documentation](./kyverno-policies.md)
- [GKM Examples](../../examples/)
- [Kyverno Image Verification Documentation](https://kyverno.io/docs/writing-policies/verify-images/)
- [Cosign Keyless Signing](https://docs.sigstore.dev/signing/overview/)
