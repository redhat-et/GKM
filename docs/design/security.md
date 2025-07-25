# GKM Security Model

The GPU Kernel Manager (GKM) enforces a multi-layered security model to ensure
that only trusted, signed GPU kernel cache images are used, that workloads
consume validated resources, and that system components maintain strong
ownership boundaries.

## Image Signature Verification and Digest Trust

To guarantee the integrity and provenance of kernel cache images, GKM uses
a cosign-based signature verification pipeline enforced via Kubernetes
admission and controller logic.

> **Note:** Integration with projects like Kyverno will be considered in the
> future when it supports image digest mutation for Custom Resource Objects.

### Components and Roles

<!-- markdownlint-disable  MD013 -->
<!-- Teporarily disable MD013 - Line length to keep the table formatting  -->
| Component                  | Responsibilities                                                                 |
|----------------------------|-----------------------------------------------------------------------------------|
| **Admission Webhook (Combined)** | - Verifies signatures<br>- Resolves image tag to digest<br>- Mutates trusted annotation<br>- Validates annotation tampering |
| **GKM Operator**           | - Promotes trusted digest to `.status.resolvedDigest`<br>- Sets `.status.lastUpdated` |
| **GKM Agent**              | - Watches `.status.resolvedDigest`<br>- Pulls image by digest<br>- Validates compatibility<br>- Updates `GKMCacheNode` status |

<!-- markdownlint-enable  MD013 -->

### Workflow

#### User Action

User creates or updates a GKMCache or ClusterGKMCache with a new spec.image
(e.g., `quay.io/cache:latest`)

#### Combined Validation Admission Webhook Executes

##### Mutation Phase (on CREATE or UPDATE):

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
    > The **Validating Admission Webhook** must be configured to
    > reject any attempt by a user to add, modify, or remove this annotation.
    > Only the GKM webhook or controller is authorized to manage this field.

#### Validation Phase:

Denies the request if:

- The image is not signed or verification fails
- The user attempts to:
  - Modify the `gkm.io/resolvedDigest` annotation directly
- Only trusted service accounts (e.g. the webhook itself) can mutate
  this annotation.

### Operator Flow

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

### GKM Agent Flow

The **GKM Agent** (on each node):

- Watches the `status.resolvedDigest` field.
- Pulls the image by digest only (never by tag) and does not run any
  further reverification tests.
- Extracts kernel cache and performs GPU/driver compatibility checks.
- Updates the local `GKMCacheNode` or `ClusterGKMCacheNode` CR with per-node status, including:
    - Compatible GPU IDs
    - Incompatibility reasons (if any)
    - Last updated timestamp

### Example Lifecycle

1. **User applies:**

   ```yaml
   apiVersion: gkm.io/v1alpha1
   kind: GKMCache
   metadata:
     name: llama2
     namespace: ml-apps
   spec:
     image: quay.io/org/llama2:latest
   ```

2. **Webhook:**

   - Verifies and adds:
     ```yaml
     metadata:
       annotations:
         gkm.io/resolvedDigest: sha256:abc123...
     ```

3. **Operator:**

   - Promotes:
     ```yaml
     status:
       resolvedDigest: sha256:abc123...
       lastUpdated: "2025-07-24T15:00:00Z"
     ```

4. **Agent:**

   - Pulls and validates the image by digest
   - Updates node-level cache status in `GKMCacheNode`

### Annotation Enforcement Summary

<!-- markdownlint-disable  MD013 -->
<!-- Teporarily disable MD013 - Line length to keep the table formatting  -->
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
