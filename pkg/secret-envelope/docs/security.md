# Security

## Threat model

The module protects database payload confidentiality and integrity when the
database is exposed without simultaneous access to the authorized wrapping
key. It authenticates application-supplied context to prevent moving an
envelope between owners, records, or fields.

The module does not protect plaintext inside a compromised process, wrapping
key misuse by an authorized principal, weak application authorization, caller
logging, memory dumps, or deletion and rollback of complete valid rows.

## Requirements

- Context values must be non-secret, stable, and derived from trusted identity.
- Keyring values must be generated with cryptographic entropy, stored only in
  the approved secret manager, scoped to the application, and retained until
  every referencing envelope expires or is rewrapped.
- IAM must allow only `kms:GenerateDataKey` and `kms:Decrypt` on exact key ARNs.
- Signature-verification workloads must allow only `kms:Verify` on exact
  asymmetric signing-key ARNs and must not receive `kms:Sign`.
- Applications must use the AWS SDK default credential chain and workload
  identity rather than static credentials.
- Plaintext and decrypted values must never enter logs, traces, metrics, panic
  messages, fixtures, or error strings.
- Envelope fingerprints require a separate reviewed leakage analysis; a raw
  digest of a low-entropy secret can enable offline guessing.

Data-key zeroization is best effort. Go can retain compiler, stack, runtime, or
garbage-collector copies that the module cannot erase.

Keyring wrapping keys remain in process memory for the provider lifetime. This
mode trades an external KMS operation for secret-delivery portability and must
be paired with restricted process access, encrypted secret delivery, versioned
rotation, and a database threat model that excludes simultaneous application
memory compromise.

Signature verification proves only that KMS accepted the exact message,
signature, key, and algorithm. Applications must separately authorize each key
for its role, enforce replay and time policy, and canonicalize the complete
statement before verification.
