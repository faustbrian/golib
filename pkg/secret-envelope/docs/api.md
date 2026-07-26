# API and persistence

`NewContext` creates immutable, canonically ordered non-secret associated data.
`NewService` requires a `KeyProvider` and defaults nonce generation to
`crypto/rand.Reader`. `Encrypt` returns an immutable `Envelope`; `Decrypt`
requires the exact expected context.

`Envelope.MarshalBinary` and `ParseEnvelope` own the stable persistence format:

1. `SEV1` magic;
2. version and AES-256-GCM algorithm identifiers;
3. bounded key-reference, wrapped-key, nonce, and ciphertext lengths; and
4. the corresponding bytes.

The key reference is duplicated in the database as an operator-visible field
when applications require rotation and IAM audits. The encoded envelope keeps
its own copy so moving ciphertext without its exact wrapping key is rejected.

The format is not JSON. JSON, text, Go-syntax, and `slog` representations are
redacted. Encrypted bytes are not plaintext secrets, but exposing them still
increases offline attack and operational risk.
