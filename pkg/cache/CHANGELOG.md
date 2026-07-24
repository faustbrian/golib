# Changelog

All notable changes are documented here. The project follows Semantic
Versioning and keeps an Unreleased section at the top.

## [Unreleased]

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Added

- Typed cache API with explicit hit, miss, stale, and negative results.
- Bounded cache-aside loading, cancellation, panic cleanup, negative caching,
  stale policies, and refresh jitter.
- Versioned hashed key spaces and strict versioned JSON codec.
- Bounded memory, native go-redis/v9, and native valkey-go backends.
- Shared backend conformance suite and Testcontainers integration matrix.
- Redacted semantic events with OpenTelemetry and slog adapters.
- Exact production coverage, race, fuzz, leak, safety, benchmark, docs, and
  release automation.
- Authenticated and certificate-verified TLS integration coverage for every
  supported Redis and Valkey version.
- Operation-model backend fuzzer, minimized corpus, recovery tests, duplicate
  OTel construction test, and observer allocation benchmark.
- Semantic truth table, backend matrix, ownership/threat model, findings
  report, operations guide, and release verdict.

### Fixed

- Run fuzz smoke campaigns for a deterministic execution count so the Go fuzz
  harness cannot report its own duration deadline as an application failure.
- Preserve successful same-instance `Set`, conditional mutation, and `Delete`
  precedence over foreground loads and stale background refreshes.
- Reject recursive same-cache loading with `ErrRecursiveLoad` instead of
  waiting on the active flight.
- Use relative server expiry so an injected clock is not confused with the
  Redis or Valkey server wall clock.
- Apply a portable 1 ms minimum server TTL instead of allowing Valkey `PX 0`.
- Strip process-local monotonic readings from portable deadlines so memory and
  serialized backends use the same wall-clock interpretation.
- Reject negative-cache deadline overflow before accessing the backend.
- Treat expired memory records as absent during deletion, matching Redis and
  Valkey.
- Require backend conformance to prove that read, write, and delete outages
  remain errors rather than misses or rejected mutations.
- Keep backend conformance failure messages compatible with standard Go error
  style so strict static analysis remains clean for downstream test suites.

[Unreleased]: https://github.com/faustbrian/golib/pkg/cache/compare/HEAD...HEAD
