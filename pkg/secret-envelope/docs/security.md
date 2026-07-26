# Security

## Threat model

The module protects database payload confidentiality and integrity when the
database is exposed without simultaneous access to the authorized KMS key. It
authenticates application-supplied context to prevent moving an envelope
between owners, records, or fields.

The module does not protect plaintext inside a compromised process, KMS misuse
by an authorized principal, weak application authorization, caller logging,
memory dumps, or deletion and rollback of complete valid rows.

## Requirements

- Context values must be non-secret, stable, and derived from trusted identity.
- IAM must allow only `kms:GenerateDataKey` and `kms:Decrypt` on exact key ARNs.
- Applications must use the AWS SDK default credential chain and workload
  identity rather than static credentials.
- Plaintext and decrypted values must never enter logs, traces, metrics, panic
  messages, fixtures, or error strings.
- Envelope fingerprints require a separate reviewed leakage analysis; a raw
  digest of a low-entropy secret can enable offline guessing.

Data-key zeroization is best effort. Go can retain compiler, stack, runtime, or
garbage-collector copies that the module cannot erase.
