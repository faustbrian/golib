# Changelog

All notable changes to this module are documented here.

## Unreleased

### Security

- Require canonical unpadded base64url signatures and JSON-number NumericDate
  claims; add exact subject allowlists and custom required-claim policy, and
  reject configurations whose claim bound cannot hold every required claim.
- Reject build-tag-dependent ES256K so the published algorithm matrix remains
  completely executable under the module's canonical build.
- Keep safe standards error categories while replacing provider and transport
  causes with a stable redacted key-provider category.
- Route automatic JWKS refresh through the configured hardened client,
  serialize it with explicit refresh work, detach provider lifetime from the
  constructor context, honor freshness directives and response age, and avoid
  body-limit arithmetic overflow.
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

- Add the stable JWT, JOSE, JSON, remote-JWKS, cache, lifecycle, and diagnostic
  specification decision register with executable evidence links.
- Document strict claim and algorithm policy, local and remote key ownership,
  fail-stale refresh behavior, cancellation, close semantics, error redaction,
  adoption, migration, security tradeoffs, and compatibility.

### Interoperability

- Add the RFC 7515 Appendix A.2 RS256 compact JWS and bidirectional
  golang-jwt interoperability for every shared HMAC, RSA, PSS, and ECDSA
  algorithm.
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
