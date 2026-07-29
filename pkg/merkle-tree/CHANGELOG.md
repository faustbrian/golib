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
