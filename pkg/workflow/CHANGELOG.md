# Changelog

All notable changes are documented here. The format follows Keep a Changelog,
and releases use Semantic Versioning.

## [Unreleased]

### Added

- Explicit bounded activity requests and registries with persisted deadlines,
  attempt metadata, idempotency keys, tenant/correlation propagation, and
  distinct success, known-failure, and unknown-outcome results.
- Atomic activity scheduling, persisted attempt starts, explicit outcome
  transitions, and deterministic retry admission with durable work that keeps
  redelivery identity separate from semantic retry identity.
- Deterministic replay of durably scheduled activity attempts, known outcomes,
  unknown outcomes, and bounded exponential retry admission decisions.
- Immutable bounded transition plans that atomically couple optimistic history
  appends with due work, plus stable history pagination and explicit unknown
  commit outcomes for durable-store adapters.
- PostgreSQL durable storage with a versioned caller-owned schema, atomic
  optimistic history-and-work commits, conflicting transition detection,
  idempotent exact replay, bounded stable pagination, and rollback-safe failure
  handling.
- Bounded durable-work claims with atomic PostgreSQL admission, expiring leases,
  monotonically increasing fencing tokens, crash recovery, renewal, explicit
  retry times, stale-owner rejection, completion, and dead-letter disposition.
- Bounded workers with deterministic clocks, tenant-fair admission, explicit
  processor dispositions, periodic lease renewal, stale-owner cancellation,
  graceful draining, bounded finalization, and synchronous lifecycle hooks.
- Durable timer scheduling and firing with atomic due-work admission, plus
  bounded deduplicated signal acceptance that must commit before transport
  acknowledgement and replays from persisted decisions only.
- Explicit ordered compensation history and durable first-attempt dispatch,
  bounded input, persisted attempt starts and explicit outcomes, independently
  durable retries, stable idempotency identities, and manual resolution that
  remains distinct from successful rollback.
- Idempotent audited lifecycle operator commands for pause, resume,
  cancellation, and termination with optimistic concurrency and replay checks
  that reject orphaned or mismatched audit records.
- Bounded deterministic instance inspection and streaming history export over
  stable forward pages with explicit traversal limits.
- Immutable durable lifecycle history, deterministic replay, pinned definition
  verification, explicit persisted migration decisions, cancellation,
  termination, and continue-as-new outcomes.
- Immutable explicitly versioned workflow definitions, bounded activity and
  compensation policies, pinned registry lookup, and explicit migrations.
