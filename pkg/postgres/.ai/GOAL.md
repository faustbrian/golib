# Goal: `postgres`

## Objective

Build a focused open source Go module for production PostgreSQL application
infrastructure on top of `github.com/jackc/pgx/v5` and `pgxpool`.

The module should standardize safe pool construction, transaction execution,
error classification, health, telemetry, and integration testing without
becoming a driver, ORM, query builder, migration engine, or repository layer.

## Product Position

`postgres` should be:

- explicitly PostgreSQL- and pgx-oriented
- transparent about underlying pgx types and behavior
- suitable for APIs, workers, ingesters, and batch processors
- safe under cancellation, failover, saturation, and shutdown
- compatible with `sqlc` generated code
- modular, with optional observability and test packages

## First-Version Scope

### Pool Construction And Lifecycle

- typed configuration for DSN, TLS, timeouts, pool sizes, and lifetimes
- DSN parsing and secret-safe validation errors
- `pgxpool.Config` hooks without hiding pgx
- startup ping and fail-fast policy
- readiness and pool statistics
- bounded shutdown and acquisition behavior
- session initialization hooks with documented failure semantics

### Transactions

- context-aware transaction runner
- isolation, access mode, and deferrable options
- panic-safe rollback
- commit/rollback error preservation
- nested-operation policy through explicit savepoints, if supported
- retry classification helpers without silently retrying arbitrary closures
- interfaces compatible with pgx and `sqlc.WithTx`

### Error Classification

- typed helpers for SQLSTATE and `pgconn.PgError`
- unique, foreign-key, check, exclusion, serialization, deadlock, timeout,
  cancellation, connectivity, and pool-exhaustion classification
- `errors.Is`/`errors.As` preservation
- no loss of constraint, table, column, detail, hint, or cause where safe
- redaction guidance for values and DSNs

### Observability And Health

- pgx tracing adapters compatible with `telemetry`
- pool acquisition, saturation, query, transaction, and error metrics
- optional `log`/`slog` integration with safe SQL/argument policy
- liveness/readiness checks with strict deadlines
- hooks for query naming from `sqlc` or callers

### Testing Support

- `testcontainers-go` PostgreSQL lifecycle helpers
- deterministic database setup hooks compatible with `migrations`
- transaction-isolated test helpers where semantics permit
- fixtures for errors, cancellation, lock contention, and failover-like loss
- no fake implementation presented as equivalent to PostgreSQL

## Non-Goals

- no PostgreSQL wire-protocol implementation
- no ORM, active record, repository generator, or general query builder
- no replacement for pgx, `sqlc`, or `migrations`
- no automatic migrations during application startup
- no hidden SQL rewriting, prepared-statement magic, or global pool
- no generic multi-database abstraction
- no transparent transaction retry for closures with external side effects

## Required Design Properties

- callers can always access or configure the underlying pgx primitives
- pool defaults must be finite, documented, and overrideable
- cancellation and deadlines must propagate to acquire, query, and transaction
  operations
- transaction cleanup must run exactly once on success, error, panic, or cancel
- error helpers must classify without flattening original errors
- telemetry must be bounded and must not leak query arguments by default
- test helpers must prove behavior against supported PostgreSQL versions

## Documentation Deliverables

- README, quickstart, and full API reference
- pool sizing, timeout, TLS, shutdown, and Kubernetes deployment guide
- `sqlc` integration guide
- transaction and savepoint guide
- SQLSTATE/error classification reference
- observability and redaction guide
- `migrations` integration and Kubernetes migration-job example
- testing guide using Testcontainers
- operational FAQ for saturation, leaked connections, failover, deadlocks,
  serialization failures, and slow queries
- compatibility matrix for Go, pgx, and PostgreSQL versions
- migration guide from direct pgx wiring and `database/sql`

## Testing And Quality Standard

Meaningful 100% production coverage is mandatory. Mocks alone cannot satisfy
the requirement for PostgreSQL semantics.

Required verification includes:

- unit tests for configuration, options, errors, and deterministic helpers
- integration tests against every supported PostgreSQL major version
- transaction success, failure, panic, cancellation, and cleanup tests
- lock contention, deadlock, serialization, constraint, and timeout tests
- pool saturation, acquisition cancellation, connection loss, and shutdown
  tests
- full race suite and goroutine/connection leak detection
- fuzzing for DSNs, SQLSTATE classification, and configuration parsing
- allocation-reporting benchmarks for pool and transaction helper overhead

## Repository And Release Requirements

- GitHub Actions with PostgreSQL matrix integration tests plus format, vet,
  lint, exact meaningful coverage, race, fuzz, benchmark, docs,
  `govulncheck`, and releases
- `make safety`, `make integration`, and `make check` matching CI
- `GO-SAFETY-1`; no production `unsafe`, cgo, or `go:linkname`
- strict `CHANGELOG.md`, SemVer, security, contribution, attribution, and
  release discipline
- explicit compatibility testing before pgx or PostgreSQL support changes

## Execution Plan

1. Define supported version matrix, package boundaries, limits, and errors.
2. Implement configuration, pool construction, lifecycle, and health.
3. Implement transaction, classification, telemetry, and testing packages.
4. Build `sqlc`, `migrations`, service, and worker examples.
5. Run version-matrix, contention, race, leak, fuzz, and benchmark hardening.
6. Complete API/security/compatibility review and publish `v1`.

## Acceptance Criteria

The goal is complete only when the package adds proven production value over
direct pgx wiring without obscuring PostgreSQL, every real database behavior
claim has integration evidence, documentation covers adoption and operations,
and all coverage, safety, CI, compatibility, and release gates pass.
