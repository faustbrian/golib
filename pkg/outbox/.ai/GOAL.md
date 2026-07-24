# Goal: `outbox`

## Objective

Build a serious open source Go implementation of the transactional outbox
pattern for reliably transferring records committed with application state to
an external publisher with at-least-once delivery.

PostgreSQL and pgx are the first-class persistence path. Publisher adapters
must be additive and must not weaken the atomic transaction guarantee.

## Product Position

`outbox` should be:

- explicit about at-least-once delivery and duplicate possibility
- transactionally correct with PostgreSQL application writes
- safe for multiple concurrent relay instances
- bounded, observable, cancellation-aware, and operationally maintainable
- usable as an embedded library and relay component
- compatible with `queue` publishers without depending on application code

## First-Version Scope

### Transactional Writer

- stable event/message envelope with ID, topic, payload, metadata, timestamps,
  ordering key, attempts, and availability time
- pgx transaction integration
- support for `sqlc`-owned application transactions
- deterministic serialization and size validation
- batch insertion with explicit atomicity
- idempotency-key hooks and optional `idempotency` integration without false
  exactly-once claims

### PostgreSQL Store

- documented schema and migrations consumable through `migrations`, without
  exposing or requiring Goose in application code
- safe claiming using PostgreSQL locking primitives
- concurrent relay coordination
- lease ownership, extension, expiry, and recovery
- retry scheduling and terminal/dead-letter states
- retention, pruning, and archival hooks
- indexes designed for a small hot working set
- partitioning guidance without mandatory partitioning

### Relay

- bounded polling and batch sizes
- configurable worker concurrency
- publish then mark-delivered semantics
- recovery after process death at every state transition
- exponential backoff with jitter and maximum attempts
- graceful draining and lease release/recovery
- per-topic or ordering-key serialization where configured
- publisher error classification

### Publisher Adapters

- small generic publisher contract
- first-class adapter for `queue`
- adapters remain separate packages so unused brokers add no dependencies
- conformance tests for acknowledgement and failure behavior

### Operations And Observability

- backlog age/depth, claim, publish, retry, dead-letter, prune, and latency
  metrics
- health/readiness based on database and publisher state
- OpenTelemetry trace linkage through optional `telemetry` integration
- `log`-compatible structured diagnostics without payload disclosure
- administrative inspection and replay primitives with explicit safeguards

## Delivery Contract

The package promises atomic persistence of application state and outbox record
only when both use the same successful database transaction. Relay delivery is
at least once. Consumers must be idempotent. A publisher success followed by a
database failure can produce a duplicate and must never be documented as
exactly once.

## Non-Goals

- no distributed transaction or exactly-once claim
- no event sourcing framework or domain event model
- no queue/broker protocol implementation
- no business routing or consumer orchestration
- no transparent transaction management around external side effects
- no mandatory daemon, schema name, payload codec, or broker
- no generic multi-database support in `v1`

## Required Design Properties

- transaction ownership must be explicit and impossible to fake accidentally
- claim/update queries must remain safe with multiple processes
- every crash point must have a documented recovery result
- leases, retries, batches, payloads, and concurrency must be bounded
- ordering guarantees must be scoped and never overstated
- cleanup must not delete records that can still require delivery
- publisher and database errors must preserve useful causes
- clocks, IDs, backoff, and polling must be injectable for deterministic tests

## Documentation Deliverables

- README, quickstart, full API reference, and architecture diagrams
- exact delivery, duplicate, ordering, transaction, and recovery guarantees
- PostgreSQL schema, index, migration, partition, and retention guide
- pgx and `sqlc` transaction examples
- `queue` relay example
- consumer `idempotency` integration guide
- Kubernetes deployment, scaling, shutdown, and monitoring guide
- replay, dead-letter, incident recovery, and disaster recovery runbooks
- capacity planning and performance guide
- FAQ, troubleshooting, security, compatibility, and migration documentation

## Testing And Quality Standard

Meaningful 100% production coverage is mandatory. Real PostgreSQL tests are
required for atomicity, locking, leases, and recovery claims.

Required verification includes:

- PostgreSQL version-matrix Testcontainers tests
- transaction commit/rollback and application-write atomicity tests
- multiple-relay contention and race tests
- deterministic crash-point tests before and after every state transition
- duplicate-delivery and idempotent-consumer examples
- lease expiry, retry, dead-letter, replay, prune, and cancellation tests
- publisher conformance and fault-injection tests
- fuzzing for envelopes, metadata, codecs, and option combinations
- leak/resource-bound tests and allocation-reporting throughput benchmarks

## Repository And Release Requirements

- GitHub Actions for all standard Go gates plus PostgreSQL and publisher
  integration matrices, race, fuzz, benchmarks, docs, and `govulncheck`
- `make safety`, `make integration`, and `make check` matching CI
- `GO-SAFETY-1`; no production `unsafe`, cgo, or `go:linkname`
- complete OSS files and strict `CHANGELOG.md` discipline
- SemVer treatment of schema, envelope, delivery semantics, errors, metrics,
  and publisher contracts
- upgrade and migration tests for every released schema version

## Execution Plan

1. Specify guarantees, envelope, schema, state machine, errors, and limits.
2. Implement transactional writer and PostgreSQL store with migrations.
3. Implement relay, leases, retries, dead letters, retention, and publisher API.
4. Implement `queue` adapter, operations, observability, and examples.
5. Run crash, contention, race, migration, fuzz, and performance hardening.
6. Complete delivery/security/compatibility audits and publish `v1`.

## Acceptance Criteria

The package is ready only when every crash point and concurrency claim has
executable PostgreSQL evidence, delivery guarantees are precisely documented,
operational recovery is complete, meaningful 100% coverage is maintained, and
all CI, safety, migration, documentation, and release gates pass.
