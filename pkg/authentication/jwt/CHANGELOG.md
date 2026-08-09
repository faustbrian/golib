# Changelog

All notable changes to this module are documented here.

## Unreleased

### Security

- Reject invalid UTF-8 in protected headers and claim sets instead of allowing
  JSON decoding to replace malformed bytes.
- Enforce algorithm-specific HMAC sizes, RSA modulus bounds, exact EC curves,
  public-only asymmetric verification keys, and reject token-provided key
  references, unpaired Unicode surrogates, and oversized JSON numbers.
- Bound remote JWK headers, bodies, key counts, initialization, and concurrent
  operations; reject redirects and compression; validate responses before
  caching; deep-copy returned sets; coalesce refreshes; and independently
  jitter refresh schedules across provider instances.

### Documentation

- Document strict claim and algorithm policy, local and remote key ownership,
  fail-stale refresh behavior, cancellation, close semantics, error redaction,
  adoption, migration, security tradeoffs, and compatibility.

### Interoperability

- Verify bidirectional HS256 compatibility with golang-jwt v5 in addition to
  the pinned RFC 7520 JWK vector and lestrrat-go/jwx implementation.

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
