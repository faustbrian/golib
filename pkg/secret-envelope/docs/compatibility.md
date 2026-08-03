# Compatibility

The module requires Go 1.26.5. Public API and binary persistence changes follow
semantic versioning after the first release.

Envelope version 1 is AES-256-GCM with a 32-byte data key and 12-byte nonce.
Changing field order, length encoding, magic, algorithm identifiers, context
canonicalization, redaction, or maximum accepted sizes is a compatibility
change.

AWS KMS integration is pinned through the module's `go.mod`. The adapter uses
symmetric KMS data keys for envelopes. Its separate verify-only boundary
supports bounded raw-message authentication with explicitly selected
RSASSA-PSS, ECDSA, or Ed25519 asymmetric KMS keys. It does not support signing,
digest-mode verification, PKCS#1 v1.5, SM2, ML-DSA, Nitro recipient
attestation, custom algorithms, or implicit key discovery.

The keyring adapter's wrapped-data-key format is provider-private version 1:
one version byte, a 12-byte AES-GCM nonce, and a 32-byte data key plus its
16-byte authentication tag. The key reference and canonical context are AAD.
Changing this representation, reference binding, or key size is a compatibility
change for envelopes created through that adapter.
