# Changelog

All notable changes to this module are documented here.

## Unreleased

### Security

- Enforce exact token issuers, trusted additional audiences, duplicate-audience
  rejection, provider-advertised algorithms, strict metadata, and fail-closed
  JWKS expiry during provider outages.
- Bound and synchronize discovery plus metadata/JWKS refresh, eagerly initialize
  keys, probe unknown key IDs after a cooldown, spread refresh with per-instance
  jitter, and redact provider failures.
- Enforce hard configuration ceilings and algorithm-specific public-key shape
  and size bounds before accepting provider keys.
- Support caller-owned nonce replay checks with panic containment and optional
  `at_hash` and `c_hash` validation through `ValidateIDToken`.

### Added

- Add `TrustedAudiences`, `TokenBinding`, and `ValidateIDToken` for explicit
  multi-audience and front-channel token-binding policy.
- Document supported profiles, exclusions, setup, adoption, cache rotation,
  concurrency, cancellation, resource lifetime, security, migration, and FAQ.

### Distribution

- Include the canonical MIT licence in the independently published module.

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

- Refresh owned-module checksums against the final consolidated archives.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Refreshed the canonical authentication checksum after its test archive
  changed, preserving isolated module verification.
- Refreshed the canonical authentication checksum after its API compatibility
  baseline was normalized to the module boundary.
