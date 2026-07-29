# Changelog

## Unreleased

### Added

- Explicit versioned identities for the canonical binary and RFC 9162
  profiles, with SHA-256 selected by stable algorithm identity.
- Bounded, cancellation-aware root construction from ordered raw leaves using
  RFC 9162 domain separation and non-power-of-two tree shape.
- Pre-copy raw-leaf byte limits for untrusted leaf ingestion.
- Immutable root identities that bind the profile, version, algorithm, digest,
  and exact tree size without aliasing caller memory.
- Immutable snapshots that retain node digests for deterministic logarithmic
  RFC 9162 inclusion-path generation without retaining raw leaf bytes.
- Independently verifiable inclusion proofs binding the complete operation
  identity, with typed malformed, unsupported, resource, and authentication
  failures.
- Caller-owned incremental builders with atomic append and batch-append
  operations whose immutable snapshots remain stable after later mutations.
- RFC 9162 consistency proof generation and independent verification binding
  both complete root identities, with bounded hostile-input traversal.
- Deterministic multi-inclusion proofs with canonical index ordering, minimal
  frontier nodes, explicit resource limits, and independent verification.
