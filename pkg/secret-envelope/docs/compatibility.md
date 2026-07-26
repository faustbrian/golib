# Compatibility

The module requires Go 1.26.5. Public API and binary persistence changes follow
semantic versioning after the first release.

Envelope version 1 is AES-256-GCM with a 32-byte data key and 12-byte nonce.
Changing field order, length encoding, magic, algorithm identifiers, context
canonicalization, redaction, or maximum accepted sizes is a compatibility
change.

AWS KMS integration is pinned through the module's `go.mod`. The adapter uses
only symmetric KMS data keys and does not support asymmetric keys, Nitro
recipient attestation, custom algorithms, or implicit key discovery.
