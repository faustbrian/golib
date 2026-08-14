# Changelog

All notable changes follow Keep a Changelog and semantic versioning.

## Unreleased

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

### Added

- Authenticated AES-256-GCM envelope encryption with immutable encryption
  contexts and a versioned binary persistence representation.
- AWS KMS data-key generation and decryption through a least-privilege client
  contract.
- A provider-neutral versioned keyring adapter for wrapping data keys with
  application keys delivered by an external secret manager.
- Verify-only AWS KMS authentication for bounded externally signed raw
  statements with explicit RSASSA-PSS, ECDSA, or Ed25519 algorithms and
  secret-safe typed failures.
- Exact statement coverage, race, fuzz, API, security, vulnerability, and
  documentation gates.

### Changed

- Increased the authenticated plaintext bound to 4 MiB for bounded encrypted
  evidence and object-storage payloads.
- Hardened exact context, envelope, wrapped-key, and AWS KMS signature request
  boundaries, including minimum and maximum valid envelope encodings.
- Documented rotation and custody requirements for externally delivered
  keyrings without introducing a cloud runtime dependency.

No release has been published.
