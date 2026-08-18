# Changelog

All notable changes are documented here. The project follows Semantic
Versioning and keeps an Unreleased section at the top.

## [Unreleased]

### Documentation

- Replace obsolete standalone-repository links and workflow claims with
  monorepo-canonical targets and current release guidance.

- Link the package README to the repository-wide Golib documentation portal.

### Changed

- Replace obsolete owned-module pseudo-version pins with the monorepo's local
  `v0.0.0` source-proxy coordinates; release tooling continues to emit exact
  `v1.0.0` dependency versions.
- Guard schema-prefix allocation arithmetic against the platform integer limit
  after enforcing the configured payload boundary.
- Remove unused CLI-related indirect dependencies from canonical module
  metadata.
- Pin owned sibling modules to exact resolvable main pseudo-versions so
  standalone and clean external consumers use immutable dependency content.

- OpenTelemetry API and metric SDK dependencies now use 1.44.x consistently
  after adding the service lifecycle adapter.
- Wait for Redis and Valkey readiness logs as well as listening sockets before
  running backend conformance tests.

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Added

- A `cacheservice` lifecycle adapter for explicit cache and Valkey resources,
  opt-in startup validation and readiness, and shared or transferred shutdown
  ownership.
- Atomic Valkey `SetIfOwned` publication guarded by an active lease owner and
  fencing token, with fail-closed ownership errors and typed cache support.
- Atomic `SetNegativeIfOwned` publication for authoritative absence under the
  same active lease and fencing-token guarantee.
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

- Reject versioned JSON payloads whose schema-prefix allocation would overflow
  the platform integer size.
- Require Redis protocol readiness and an actual `NOAUTH` response before
  authenticated backend assertions begin.
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

[Unreleased]: https://github.com/faustbrian/golib/commits/main/pkg/cache
