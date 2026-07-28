# Changelog

All notable changes follow [Keep a Changelog](https://keepachangelog.com/) and
this project follows Semantic Versioning.

## [Unreleased]

### Security

- Protect static Basic and API-key credentials with random per-authenticator
  HMAC-SHA-256 keys instead of reusable unkeyed secret digests.

### Changed

- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

- Add explicit pipe-compatible bearer extraction for legacy opaque-token
  contracts while retaining strict RFC 6750 syntax by default.
- Preserve `apikey.Static` comparability while retaining per-authenticator
  keyed credential digests.
- Execute API compatibility tooling against the isolated module graph so owned
  dependency source changes cannot conflict with release checksums.
- Refresh owned-module checksums against the final consolidated archives.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Use the repository-pinned current `apidiff` revision for root and optional
  authentication-module compatibility checks.

### Added

- Constant-work static bearer authentication with bounded overlapping tokens
  and atomic whole-set replacement for credential rotation and revocation.
- Immutable bounded principals, typed redacted credentials, explicit anonymous
  results, stable failures, challenges, context helpers, and deterministic
  authenticator composition.
- Constant-time static Basic and API-key authentication, atomic API-key
  rotation, and callback bearer and API-key adapters.
- Strict opt-in HTTP header, query, and cookie extraction with fail-closed
  authentication-only middleware.
- Optional JWT/JWK and OIDC modules with bounded remote key handling, strict
  algorithm and claim validation, rotation, stale-key behavior, and owned
  resource lifecycle.
- Secret-safe `slog` and optional OpenTelemetry instrumentation adapters.
- Deterministic test fixtures, runnable examples, fuzz targets, race tests,
  benchmarks, exact statement coverage gates, API compatibility checks, and
  reproducible release automation.
- Security audit artifacts covering the threat model, findings, protocol and
  failure-injection matrices, authoritative vectors, and secure adoption.

### Changed

- OIDC remote refresh now has bounded cancellation-aware waiters, conditional
  requests, bounded freshness, failure cooldown, and consistent numeric-date
  skew enforcement.
- JWT remote shutdown now owns, cancels, and drains admitted operations.
- JWT and OIDC reject algorithm/key-family and JWK metadata mismatches.
- Basic credentials and HTTP challenges reject control bytes, and challenges
  enforce explicit parameter and field bounds.
- Query credential constructors are deprecated for new designs.

[Unreleased]: https://github.com/faustbrian/golib/pkg/authentication/compare/v0.0.0...HEAD
