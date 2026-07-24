# Changelog

All notable changes follow [Keep a Changelog](https://keepachangelog.com/) and
this project follows Semantic Versioning.

## [Unreleased]

### Changed

- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Use the repository-pinned current `apidiff` revision for root and optional
  authentication-module compatibility checks.

### Added

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
