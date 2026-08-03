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

Plaintext is limited to 4 MiB. The encoded-envelope limit derives from that
bound plus the versioned header, key reference, wrapped key, nonce, and GCM
authentication tag.

The key reference is duplicated in the database as an operator-visible field
when applications require rotation and custody audits. The encoded envelope
keeps its own copy so moving ciphertext without its exact wrapping key is
rejected.

## Versioned keyring provider

`keyring.New` copies one to 32 versioned 32-byte wrapping keys. New encryption
selects an explicit reference; decryption selects the exact reference embedded
in the envelope. The provider authenticates the reference and canonical
context while wrapping each fresh data key with AES-256-GCM.

The application owns decoding secret-manager values, selecting the active
reference, retaining historical keys, and rolling out rotation. Removing a key
while persisted ciphertext still references it makes that ciphertext
unreadable.

The format is not JSON. JSON, text, Go-syntax, and `slog` representations are
redacted. Encrypted bytes are not plaintext secrets, but exposing them still
increases offline attack and operational risk.

## AWS KMS signature verification

`awskms.NewSignatureVerifier` fixes one reviewed signing algorithm for the
verifier lifetime. `Verify` accepts an explicit KMS key reference, one non-empty
raw message of at most 4096 bytes, and one bounded signature. It copies caller
bytes before calling KMS and never exposes a signing operation.

Accepted raw-message algorithms are RSASSA-PSS SHA-256/384/512, ECDSA
SHA-256/384/512, and Ed25519 SHA-512. PKCS#1 v1.5, digest-mode, SM2, ML-DSA,
and implicit algorithm selection are rejected.

`ErrSignatureRejected` is an authenticated negative result.
`ErrKMSSignatureVerification` is an operational KMS failure.
`ErrInvalidSignatureResponse` rejects incomplete or contradictory successful
responses. Wrapped causes remain available to `errors.Is` and `errors.As`, but
formatted errors never render them.
