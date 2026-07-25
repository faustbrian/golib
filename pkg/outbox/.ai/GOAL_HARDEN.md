# Goal Harden: `outbox`

## Mission

Perform an evidence-driven atomicity, PostgreSQL locking, relay state-machine,
crash recovery, delivery, schema migration, observability, and resource-safety
audit of `outbox`, then close every gap before production release.

The audit must assume process death, network partitions, publisher ambiguity,
database failover, clock skew, duplicate delivery, and concurrent operators.

## Authoritative Inputs

- PostgreSQL transaction, lock, isolation, `SKIP LOCKED`, index, partition,
  vacuum, timeout, and durability documentation for supported versions
- pgx transaction and error contracts
- publisher contracts, including `queue`, used by adapters
- transactional outbox pattern guarantees, clearly separated from package
  policy and application responsibility
- `.ai/GOAL.md`, `migrations` schema integration, public APIs, docs, tests,
  fuzzers,
  benchmarks, workflows, dependencies, and changelog

## Phase 1: Baseline And State Model

1. Inventory every exported API, table/column/index, migration, state,
   transition, query, lease, worker, timer, publisher result, and metric.
2. Reconstruct the writer and relay state machines, listing every crash point
   before and after each database and publisher operation.
3. Build PostgreSQL/publisher compatibility and schema-upgrade matrices.
4. Run all quality/integration/race/fuzz/benchmark/docs/security gates.
5. Threat-model record loss, premature deletion, duplicate explosion, tenant
   escape, poison payloads, lease theft, replay abuse, and payload disclosure.
6. Require a failing real-database regression before each behavior fix.

## Transactional Writer Audit

- same-transaction proof for application write plus outbox insertion
- caller-owned transaction, wrong connection, already-aborted transaction,
  commit/rollback failure, panic, cancellation, and connection loss
- single and batch event atomicity
- envelope IDs, timestamps, metadata, topic, ordering key, payload version,
  size limits, and duplicate idempotency keys
- no API that appears atomic while internally opening an independent transaction

## Claim, Lease, And Concurrency Audit

- zero/one/many relays across processes
- `SKIP LOCKED` fairness, starvation, batch boundaries, isolation levels,
  statement/lock timeout, deadlock, serialization failure, and failover
- lease acquisition, extension, expiry, reclaim, ownership tokens, clock skew,
  process suspension, and late acknowledgements
- ordering-key serialization and documented absence of global ordering
- bounded polling, workers, batches, retries, payloads, and in-memory buffers

## Publish And Crash-Recovery Audit

Test deterministic failure at every point:

- before claim, after claim, before publish, during publish, after publisher
  acceptance, before delivered update, during update, and after commit
- ambiguous timeout where the publisher may have accepted the message
- retry scheduling, max attempts, dead-letter transition, replay, and repeated
  operator actions
- cancellation and shutdown while claiming, publishing, extending, or marking
- publisher panic, malformed response, permanent/transient misclassification,
  and nested retry multiplication
- duplicate delivery remains expected and bounded by documented policy

## Schema, Retention, And Operations Audit

- clean install and upgrade from every released schema version
- concurrent application/relay behavior during compatible migrations
- indexes and query plans at empty, normal, and large backlog sizes
- pruning/archival cutoff races, dead-letter retention, partition boundaries,
  vacuum/bloat, long transactions, and replica/read-only mistakes
- replay authorization hooks, auditability, selection bounds, and idempotency
- health/readiness semantics during database or publisher outage

## Mandatory Hardening Evidence

- meaningful 100% production coverage plus real PostgreSQL evidence
- PostgreSQL-major and publisher-adapter matrices
- deterministic crash-point fault injection for every transition
- multi-process contention and full race/leak suites
- fuzzing for envelopes, metadata, codecs, options, and publisher errors
- migration, rollback-policy, retention, and query-plan tests
- allocation and throughput benchmarks at representative backlog sizes
- operational recovery exercises documented and reproducible

## Required Deliverables

1. State/crash/guarantee/schema/compatibility matrices and threat model.
2. Findings report with severity, loss/duplication impact, and disposition.
3. Real-database regressions, fault injectors, fuzz seeds, fixes, and benchmarks.
4. Updated API, delivery, schema, operations, recovery, security,
   compatibility, migration, troubleshooting, and changelog docs.
5. Release verdict stating exact commands and residual delivery risks.

## Release Blockers

Block release for any record-loss path, false atomicity/exactly-once claim,
unsafe lease ownership, premature pruning, unrecoverable schema migration,
unbounded duplicate/retry behavior, payload leak, race, integration flake,
coverage game, or red gate.

## Completion Criteria

Hardening is complete only when every crash point has deterministic PostgreSQL
evidence, record-loss risks are closed, duplicate behavior is accurately
bounded and documented, all high/medium findings are resolved, schema upgrades
are proven, and every quality, integration, security, operations, and release
gate passes.
