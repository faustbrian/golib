# Architecture

The root package owns authenticated local encryption, immutable context,
versioned framing, limits, redaction, and the key-provider contract. It has no
cloud SDK dependency.

`adapters/awskms` owns AWS SDK request and response mapping. It requests
`AES_256`, passes the exact non-secret encryption context to both KMS
operations, stores the resolved key ARN returned by KMS, and specifies that
exact key during decryption.

Applications own canonical plaintext serialization, authorization,
transactions, fingerprints, row lifecycle, IAM policy, AWS configuration, and
rotation orchestration. Secrets Manager remains the boundary for deployment
credentials; it is not used as a per-row persistence substitute.
