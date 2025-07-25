# GKM Security Model

The GPU Kernel Manager (GKM) enforces a multi-layered security model to ensure
that only trusted, signed GPU kernel cache images are used, that workloads
consume validated resources, and that system components maintain strong
ownership boundaries.

---

## Image Signature Verification and Digest Trust

To guarantee the integrity and provenance of kernel cache images, GKM uses
a cosign-based signature verification pipeline enforced via Kubernetes
admission and controller logic.

> **Note:** Integration with projects like Kyverno will be considered in the
> future when it supports image digest mutation for Custom Resource Objects.

### Image Signature Verification and Digest Trust Components (and their Roles)

<!-- markdownlint-disable  MD013 -->
<!-- Temporarily disable MD013 - Line length to keep the table formatting  -->
| Component                  | Responsibilities                                                                 |
|----------------------------|-----------------------------------------------------------------------------------|
| **Admission Webhook (Combined)** | - Verifies signatures<br>- Resolves image tag to digest<br>- Mutates trusted annotation<br>- Validates annotation tampering |
| **GKM Operator**           | - Promotes trusted digest to `.status.resolvedDigest`<br>- Sets `.status.lastUpdated` |
| **GKM Agent**              | - Watches CR `gkm.io/resolvedDigest` annotations. <br>- Pulls image by digest<br>- Validates compatibility against GPU Hardware<br>- Updates `GKMCacheNode` status |

<!-- markdownlint-enable  MD013 -->

### Workflow of Image Signature Verification and Digest Trust

#### Step 1: User Submits GKMCache CR

User creates or updates a GKMCache or ClusterGKMCache with a new spec.image
(e.g., `quay.io/cache:latest`)

#### Step 2: Admission Webhook Verifies and Mutates

This webhook operation is broken down into two phases:

- **Phase 1 - Mutation (on CREATE or UPDATE)**:

  - Resolves the tag to a content digest (`sha256:...`).
  - Verifies the image signature using
    [cosign](https://github.com/sigstore/cosign).
  - Adds a protected annotation to the CR:

    ```yaml
    metadata:
      annotations:
        gkm.io/resolvedDigest: sha256:abc123...
    ```

    > **Note:** This annotation is considered *trusted system state*.
    > The **Validating Admission Webhook** must be configured to reject
    > any attempt by a user to add, modify, or remove this annotation.
    > Only the GKM webhook or controller is authorized to manage this field.


- **Phase 2 - Validation**: Denies the request if:
  - The image is not signed or verification fails
  - The user attempts to:
    - Modify the `gkm.io/resolvedDigest` annotation directly
  - Only trusted service accounts (e.g. the webhook itself) can mutate this annotation.

#### Step 3A: Operator Promotes Digest Annotation to CR Status

The **GKM Operator**:

- Watches `GKMCache` and `ClusterGKMCache` CRs for new or changed `spec.image`
  or annotations.
- When it finds a trusted annotation, it updates the CR `status`:

    ```yaml
    status:
      resolvedDigest: sha256:abc123...
      lastUpdated: "2025-07-24T14:22:00Z"
    ```

- The operator does **not** reverify the signature.

#### Step 3B: Agent Pulls Image and Validates Compatibility

The **GKM Agent** (on each node):

- Also watches `GKMCache` and `ClusterGKMCache` CRs for new or changed `spec.image`
  or annotations.
- When it finds a trusted annotation, it:
  - Pulls the image by digest only (never by tag) and does not run any
    further reverification tests.
  - Extracts kernel cache and performs GPU/driver compatibility checks.
  - Updates the local `GKMCacheNode` or `ClusterGKMCacheNode` CR with per-node status, including:
      - Compatible GPU IDs
      - Incompatibility reasons (if any)
      - Last updated timestamp

> **Note**:  The agent does **not** reverify the signature.

> **Note**:  On CR update, the agent checks if the Cache is already extracted on the node,
> and whether or not it's in use. If a cache is already in use, the Agent might need to track
> the usage of the old cache and do some form of garbage collection when it's no longer
> in use.

### Example Lifecycle of Image Signature Verification and Digest Trust

1. **User** applies:

   ```yaml
   apiVersion: gkm.io/v1alpha1
   kind: GKMCache # Or ClusterGKMCache
   metadata:
     name: llama2
     namespace: ml-apps
   spec:
     image: quay.io/org/llama2:latest
   ```

2. **Webhook** Verifies and adds:

     ```yaml
     metadata:
       annotations:
         gkm.io/resolvedDigest: sha256:abc123...
     ```

3. **Operator** Promotes:

     ```yaml
     status:
       resolvedDigest: sha256:abc123...
       lastUpdated: "2025-07-24T15:00:00Z"
     ```

4. **Agent:**

   - Pulls and validates the image by digest from the annotation in the CR.
   - Updates node-level cache status in `GKMCacheNode` or `ClusterGKMCacheNode`.

### Annotation Enforcement Summary

<!-- markdownlint-disable  MD013 -->
<!-- Temporarily disable MD013 - Line length to keep the table formatting  -->
| Field                         | Who Can Write It              | Enforced By                    |
|-------------------------------|-------------------------------|--------------------------------|
| `metadata.annotations["gkm.io/resolvedDigest"]` | Only the Admission Webhook | Validating logic inside webhook |
| `status.resolvedDigest`       | Only the GKM Operator         | RBAC and controller logic      |
| `spec.image`                  | User-provided input           | Mutating webhook handles digest resolution |
<!-- markdownlint-enable  MD013 -->

### Security Guarantees

- Digest is verified and resolved at admission time
- Spec remains user-controlled, but cannot override digest once resolved
- Runtime components pull by digest only
- Digest is promoted to `.status` by a trusted controller
- Nodes act on immutable, validated digests

---
