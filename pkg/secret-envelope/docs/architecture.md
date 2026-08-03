# Architecture

The root package owns authenticated local encryption, immutable context,
versioned framing, limits, redaction, and the key-provider contract. It has no
cloud SDK dependency.

`adapters/keyring` owns in-process data-key wrapping with an immutable set of
versioned AES-256 keys supplied by the application. It authenticates the exact
key reference and canonical context, generates a fresh data key and wrapping
nonce for each encryption, and retains no secret-manager client dependency.

`adapters/awskms` owns AWS SDK request and response mapping. It requests
`AES_256`, passes the exact non-secret encryption context to both KMS
operations, stores the resolved key ARN returned by KMS, and specifies that
exact key during decryption. Its separate signature verifier owns a
least-privilege `kms:Verify` surface, fixes one reviewed asymmetric algorithm,
uses raw-message mode, and exposes no signing method.

Applications own canonical plaintext serialization, authorization,
transactions, fingerprints, row lifecycle, secret delivery, provider policy,
and rotation orchestration. Signature consumers also own canonical statement
encoding, signer-to-role authorization, replay policy, and signed-statement
storage. Secret managers remain deployment boundaries; they are not used as
per-row persistence substitutes.
