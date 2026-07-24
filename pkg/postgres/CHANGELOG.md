# Changelog

All notable changes are documented here. The project follows Semantic
Versioning and keeps an Unreleased section until a release is tagged.

## [Unreleased]

### Added

- `postgrestest.RunIsolated` for bounded always-rollback integration tests
  that preserve callback errors and panic values
- pinned fail-closed API compatibility checks against a committed stable v1
  module baseline

### Changed

- hardening documentation now records the exact hosted `v1.0.0` release proof
- security support and roadmap documentation now reflect the stable release
- hardening evidence now covers the full transaction-mode matrix, representative
  DSN forms, native pool hooks, authentication redaction, and strict TLS refusal
- nested savepoint evidence now proves inner rollback and outer persistence
- startup evidence now covers wrong-protocol endpoints and explicit native DNS
  ownership alongside stable-endpoint recovery
- performance documentation now records a dated allocation baseline for every
  required helper and real-PostgreSQL benchmark
- API compatibility checks now use the repository-pinned current `apidiff`
  revision

### Fixed

- transaction, savepoint, and isolated-test callbacks that terminate their
  goroutine now roll back exactly once; observed operations report `aborted`
- rollback panics can no longer replace a callback panic or convert
  `runtime.Goexit` into a different terminal failure
- commit and savepoint-release panics now attempt bounded rollback before the
  original panic propagates, preventing stranded pooled transactions
- rollback panics can no longer replace returned transaction or isolation
  callback errors
- panicking Testcontainers setup hooks now clean up the owned container before
  preserving the original panic, including when termination also panics
- Testcontainers cleanup panics can no longer replace a returned setup error
- Testcontainers termination panics can no longer replace connection-string
  startup failures
- Testcontainers setup hooks that call `testing.T.FailNow` now clean up the
  owned container before terminating the test goroutine
- typed TLS overrides copy certificate pools and mutable protocol and
  certificate data before pool construction
- successful isolated callbacks no longer hide a rollback panic as success
- `golang.org/x/text` now uses the latest fixed release, removing
  `GO-2026-5970` from reachable pgx pool construction paths

## [1.0.0] - 2026-07-16

### Added

- finite typed pgxpool configuration with secret-safe validation and panic
  containment for malformed DSNs
- fail-fast or lazy startup, bounded acquire, health, statistics, liveness, and
  shutdown operations with direct native pool access
- context-aware transaction and savepoint runners with panic-safe rollback and
  preserved callback, commit, and rollback errors
- SQLSTATE, constraint, cancellation, timeout, connectivity, saturation, and
  retry-advisory classification without flattening original errors
- bounded lifecycle observations, safe `slog`, and optional OpenTelemetry
  metrics that omit SQL, arguments, DSNs, and raw database errors
- Testcontainers support plus PostgreSQL 14-18 integration evidence for
  contention, deadlock, serialization, cancellation, server timeouts,
  constraints, transaction loss, stop/restart, saturation, shutdown, and cleanup
- executable sqlc, migrations, service, and worker adoption examples
- exact production coverage, race, leak, fuzz, benchmark, safety, lint,
  vulnerability, documentation, compatibility, and release automation

[Unreleased]: https://github.com/faustbrian/golib/pkg/postgres/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/faustbrian/golib/pkg/postgres/releases/tag/v1.0.0
