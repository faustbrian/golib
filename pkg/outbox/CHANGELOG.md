# Changelog

All notable changes to this project will be documented in this file. The
format follows Keep a Changelog, and releases follow Semantic Versioning.

Schema, envelope encoding, delivery semantics, exported errors, metrics, and
publisher contracts are public compatibility surfaces.

## [Unreleased]

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Added

- Bounded deterministic envelope construction.
- Caller-owned pgx transactional writer and atomic batch insertion.
- PostgreSQL schema, claims, leases, retries, dead letters, replay audit, and
  delivered-record pruning.
- Bounded cancellation-aware relay with scoped ordering and error
  classification.
- Payload-safe publisher panic containment through normal retry policy.
- Automatic in-flight lease renewal with cancellation on ownership uncertainty.
- Separately versioned `queue` publisher adapter.
- Payload-safe lifecycle events, structured logging, readiness checks, and
  PostgreSQL backlog diagnostics.
- Archive-before-delete retention with idempotent archive guidance.
- Bounded payload-free administrative inspection and replay/retention events.
- Bounded dead-letter pruning and archive-before-delete retention.
- Separately versioned `telemetry` metrics and trace-linkage adapter.
- Low-cardinality backlog depth and oldest-pending-age gauges.
- PostgreSQL 14-18 CI matrix, safety checks, fuzz targets, allocation
  benchmarks, and meaningful 100% production coverage gates.
- Goroutine-leak detection, real migration rollback, and hot-set/retention
  query-plan assertions.
- Real PostgreSQL duplicate-window fault injection after publisher acceptance.
- Real PostgreSQL graceful-cancellation lease-release evidence.
- Compiled duplicate-consumer and queue relay examples.
- Exact `idempotency` consumer integration and atomic completion guidance.
- Real PostgreSQL canceled-context atomicity matrix for every store transition.
- Workspace-disabled publisher adapter integration matrix in CI.
- Writer-side envelope and batch validation that prevents direct construction
  from bypassing resource or new-record bounds.
- Infallible canonical timestamp encoding with insert-time JSON range checks.
- Observer and structured-logger panic containment at diagnostic boundaries.
- Conduct, support, contribution, and custom-schema migration policies.
- Go 1.26.5 minimum to exclude GO-2026-5856 from release toolchains.
- Canonical `migrations` source format and cross-repository compatibility
  tests.
- PostgreSQL writer failure, timeout, read-only, fairness, policy-callback,
  persistence-ceiling, and archive-panic hardening evidence.
- Default-deny replay authorization with a panic-contained, payload-safe
  `ReplayAuthorizer` hook.
- Real PostgreSQL serialization-failure, deadlock, retention-cutoff,
  long-snapshot, VACUUM, database-restart, and concurrent operator evidence.
- Real four-process claim contention and ambiguous publisher-timeout duplicate
  evidence.
- Actual post-claim process-death, lease-reclaim, and stale-token evidence.
- Empty, normal, and 40,100-row claim/delivered/dead query-plan evidence.
- Dedicated publisher-failure fuzzing and real PostgreSQL claim benchmarks at
  empty, 1,000-row, and 100,000-row backlogs.
- Reproducible recovery and sibling `migrations` integration gates.
- Relay option fuzzing across every configured numeric bound and timer.
- Explicit schema-upgrade, concurrency, and rollback-policy matrix.
- Unreleased security-support policy and explicit callback liveness boundary.

### Changed

- Readiness now requires a writable PostgreSQL primary.
- Persisted publisher failure diagnostics are bounded and payload-safe.
- Relay heartbeat, classifier, and backoff callbacks are panic-contained and
  bounded.
- The initial schema aligns the full payload-version range and enforces
  string-map metadata, payload, identity, replay, lease, and diagnostic
  ceilings.
- Store and relay batches, workers, attempts, and lease durations now have
  absolute ceilings; oversized custom Store responses fail before relay-owned
  job-buffer allocation.
- Retry persistence now accepts a bounded duration and schedules from the
  PostgreSQL clock, so relay host skew cannot extend the one-minute bound.
- Replay and archive rollback cleanup now uses a detached five-second deadline
  so canceled callers or network partitions cannot leave cleanup unbounded.
- Attempts are constrained and claim-saturated at the relay's absolute 10,000
  ceiling so direct SQL cannot create an overflow-based poison row.
- Message and replay-audit timestamps must be finite so PostgreSQL infinity
  values cannot create rows that pgx cannot decode.
- Persistent timestamps are restricted to envelope years 0000–9999 so direct
  SQL cannot create non-RFC3339 canonical payloads.
- Replay schedules outside years 0000–9999 fail before authorization or SQL.
- Relay and store diagnostic clock panics degrade to zero-time metrics without
  interrupting durable operations.
