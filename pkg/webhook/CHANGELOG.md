# Changelog

All notable changes are documented here. This project follows Keep a Changelog
and Semantic Versioning.

## [Unreleased]

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Refreshed the canonical logging checksum after its API compatibility tooling
  was standardized.

### Added

- Versioned HMAC-SHA-256 and HMAC-SHA-512 signing and verification.
- Signed, bounded nonces with injectable generation and a `crypto/rand` default.
- Exact-byte HTTP verification, bounded bodies and headers, rotation windows,
  safe typed failures, middleware, atomic replay protection, and a
  `idempotency` adapter.
- Deterministic envelopes, bounded delivery and retries, `Retry-After`,
  dead-letter and replay hooks, fan-out, SSRF and DNS-rebinding protection,
  and `queue` and `outbox` adapters.
- Secret-safe observations, independent Python vectors, fuzzers, allocation
  benchmarks, complete production coverage, and release gates.
- Compiled `log` diagnostics, telemetry HTTP propagation, deterministic
  consumer fixtures, and an executable queued-delivery example.
- Pinned GitHub Actions workflow linting in local and CI release gates.
- Enforced a pure-Go dependency graph in the standalone safety gate.

### Fixed

- Check response-body and telemetry-runtime cleanup failures in adapter and
  SSRF tests instead of silently discarding them.
- Express the accepted HTTPS or explicitly enabled HTTP schemes directly,
  preserving the default-deny SSRF policy without negation ambiguity.
- Saturate oversized numeric `Retry-After` values and reject body limits whose
  sentinel byte would overflow an `int64` bound.
- Compare caller timestamps at the signature protocol's Unix-second precision.
- Select rotation keys at the signed timestamp, reject negative timestamps,
  and reject inverted key validity windows.
- Keep replay identity stable across overlapping secret rotation keys.
- Cover bounded `Content-Type` and `Idempotency-Key` values in v1 signatures.
- Preserve and authenticate duplicate query-value order while sorting keys.
- Preserve and authenticate the exact case-sensitive HTTP method.
- Clamp delivery latency observations when an injected clock moves backward.

## [1.0.0] - 2026-07-15

The first release will freeze the `v1` canonicalization and wire contracts.

[Unreleased]: https://github.com/faustbrian/golib/pkg/webhook/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/faustbrian/golib/pkg/webhook/releases/tag/v1.0.0
