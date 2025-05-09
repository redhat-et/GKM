# Image Verification with Cosign

The TKM Operator leverages Cosign for secure verification of Triton kernel
cache images. Cosign, a tool from the Sigstore project, is designed to verify
OCI container images by checking their signatures against those stored in a
registry.

## How Cosign Works with OCI Images

### Signature Storage

Cosign signatures are stored as separate artifacts within the OCI registry,
linked to the image digest but not embedded directly within the image itself.
This separation ensures that the image remains untouched while its integrity
can be independently verified.

### Efficient Verification Process

Instead of pulling the entire image for verification, Cosign fetches only the
signature artifact. It then compares the digest of the fetched signature
against the image's manifest digest, thereby confirming the image's
authenticity without downloading the entire image.

## Kubernetes Integration

### Global Toggle for Signature Validation

To provide flexibility and support various environments, the TKM Operator
includes a global configuration toggle to enable or disable image signature
verification.

- If enabled, the operator will verify each Triton kernel cache image using
  Cosign before marking it as `Verified`.
- If disabled, the operator will skip the Cosign verification step, and the
  image will be marked as `Verified` automatically.
- The toggle setting is stored in a ConfigMap within the operator's namespace,
  allowing cluster administrators to update the behavior without redeploying
  the operator.

#### Configuration Example

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: tkm-operator-config
  namespace: tkm-system
data:
  enableSignatureVerification: "true"
```

The operator will read this configuration during startup and periodically to
accommodate any changes made while it is running.

### Operator Verification Workflow

- On detecting a new TritonKernelCache or TritonKernelCacheCluster CR, the
  operator will:
  - Extract the image reference from the CR.
  - Check the global configuration to see if signature validation is enabled.
  - If enabled, use Cosign to verify the image signature.
  - Update the CR status to reflect the verification result.

- Agent Coordination: Agents on individual nodes will not perform signature
  verification themselves. Instead, they will monitor the CR status for the
  Verified condition, indicating that the operator has successfully verified
  the image. Once verified, the agent proceeds to the next step of
  compatibility checks.

An example workflow is shown below:

```sh
                         +--------------------------------+
                         | User creates TritonKernelCache |
                         | (CR) with cache image reference|
                         +---------------+----------------+
                                         |
                                         v
               +-------------------------+---------------------------+
               | Operator reads global configuration for verification|
               +-------------------------+---------------------------+
                                         |
                     +-------------------+----------------------+
                 Yes |                                          | No
     +---------------v---------------+        +-----------------v------------------+
     | Is signature verification     |        | Signature verification disabled    |
     | enabled?                      |        |                                    |
     +---------------+---------------+        +-----------------+------------------+
                     |                                          |
                     v                                          v
     +---------------+---------------+        +-----------------+------------------+
     | Trigger Cosign verification   |        | Skip verification                  |
     +---------------+---------------+        | Mark CR as "Verified" immediately  |
                     |                        +------------------------------------+
            +--------+----------+
     Success|                   | Failure
  +---------v---------+       +-v---------------+
  | Mark CR as        |       | Mark CR as      |
  | "Verified"        |       | "Failed"        |
  +---------+---------+       +-----------------+
            |
            v
+-----------+-----------+
| Agents read CR status |
+-----------+-----------+
            |
   +--------+-----------+
   |                    |
Verified             Failed
   |                    |
   v                    v
+--+---------------+  +-+-----------------------------+
| Run compatibility|  | Log error and halt processing |
| checks on node   |  +-------------------------------+
+------------------+
```

### Advantages of this approach

1. Centralized Verification: Performing image signature verification in the
   control plane minimizes the computational overhead on worker nodes and
   ensures consistent validation results.

2. Dynamic Configuration: The use of a global toggle allows for seamless
   adaptation to different security policies, ranging from strict validation
   in production environments to relaxed validation in development clusters.

### Example Cosign Verification

TODO...

## Open Items

- Is there a (documented/best practice) pattern that openshift uses?
- what about an admission webhook for CRDs?
- What is the image signature verification overhead?
- If we do sig verification on the controller:
    - Are there concerns about scaling/back pressure issues?
    - What will the worker needs still need to check?
- what about OCI runtime image sig verification? is that something we
  can re-use?

## References

- https://docs.sigstore.dev/cosign/verifying/verify/
- https://edu.chainguard.dev/open-source/sigstore/cosign/how-to-verify-file-signatures-with-cosign/