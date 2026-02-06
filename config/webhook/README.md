# Webhook Configuration

## Important Notes

### Reinvocation Policy

The `reinvocationPolicy: IfNeeded` setting is **critical** for proper operation with Kyverno.

**Background:**

- Kyverno mutates the image field to add the digest (e.g., `image:tag@sha256:...`)
- The GKM mutating webhook needs to run **after** Kyverno to extract
  the digest from the mutated image
- With `IfNeeded`, the GKM webhook runs again after Kyverno's mutation

**Implementation:**

Since Kubebuilder's controller-gen doesn't support setting `reinvocationPolicy` via
markers, it is applied using a Kustomize patch:

- `webhook_reinvocation_patch.yaml` - Adds `reinvocationPolicy: IfNeeded` to both mutating webhooks
- `kustomization.yaml` - Applies this patch during deployment

**DO NOT** manually add `reinvocationPolicy` to `manifests.yaml` as it will be
overwritten by controller-gen. The patch ensures it's always applied correctly.
