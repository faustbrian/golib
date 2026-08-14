# Changelog

All notable changes are documented here. The project follows semantic
versioning after v1.

## Unreleased

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

### Security

- Parse Argon2id parallelism at its exact eight-bit representation before
  conversion, while retaining stricter configured resource limits.

### Changed

- Delegate local mutation checks to the canonical exact-100 repository runner
  and remove the superseded package-local Gremlins configuration.
- Remove an obsolete conversion suppression now that Argon2id parallelism is
  parsed at eight-bit width.
- Execute API compatibility tooling against the isolated module graph so owned
  dependency source changes cannot conflict with release checksums.
- Updated the pinned `apidiff` revision used by API compatibility checks.

### Fixed

- Prevented mixed Argon2id policy transitions from lowering any stronger
  memory, salt, or output dimension.
- Rejected bcrypt passwords above 72 bytes before verification work instead of
  misclassifying the primitive failure as a mismatch.
- Rejected oversized Argon2id salt and output fields before base64 decoding.

### Security

- Extended the interoperability gate to generate fresh PHP hashes for Go
  verification in addition to generating fresh Go hashes for PHP.
- Added bcrypt and malformed-path timing regression evidence plus a
  cgroup-constrained Kubernetes benchmark gate.

## v1.0.0

### Added

- Immutable Argon2id and bcrypt policy with strict resource limits.
- Canonical PHC Argon2id and Laravel-compatible bcrypt parsing.
- Hash, verify, rehash, and explicit verify-and-upgrade operations.
- Bounded admission with cancellation and drainable lifecycle.
- Secret-safe classified errors, encoded-hash formatting, and observations.
- Immutable classified errors with read-only kind, operation, and cause access.
- Synthetic PHP 8.5 Laravel compatibility fixtures and maintained vectors.
- Application lookup/CAS, service lifecycle, and deterministic test adapters.
- Exact production coverage, race, fuzz, timing, and benchmark evidence.

### Security

- Hostile encoded hashes are rejected before primitive execution.
- Rehash decisions are monotonic and cannot downgrade Argon2id to bcrypt.
- Password inputs are copied, never retained, and omitted from diagnostics.
