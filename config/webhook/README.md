# Webhook Configuration

## Important Notes

### Reinvocation Policy

The `reinvocationPolicy: IfNeeded` setting in `manifests.yaml` is
**manually added** and critical for proper operation with Kyverno.

**Background:**

- Kyverno mutates the image field to add the digest (e.g.
  , `image:tag@sha256:...`)
- The GKM mutating webhook needs to run **after** Kyverno to extract
  the digest from the mutated image
- With `IfNeeded`, the GKM webhook runs again after Kyverno's mutation

**DO NOT remove this setting** when regenerating manifests with `make manifests`.

If you regenerate the webhook manifests using controller-gen, you **must** re-add:

```yaml
reinvocationPolicy: IfNeeded
```

to both mutating webhooks:

- `mclustergkmcache.kb.io`
- `mgkmcache.kb.io`

### Why controller-gen doesn't include it

Kubebuilder's controller-gen doesn't support setting `reinvocationPolicy` via
markers, so it must be added manually after generation.
