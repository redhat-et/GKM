# Kyverno Policy Moved

This file has been moved to: `config/kyverno/policies/clustergkmcache-policy.yaml`

To deploy Kyverno policies, use:

```bash
make deploy-kyverno-policies
```

Or deploy everything (Kyverno + policies) with:

```bash
make run-on-kind KYVERNO_ENABLED=true
```
