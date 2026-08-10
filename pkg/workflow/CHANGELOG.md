# Changelog

All notable changes are documented here. The format follows Keep a Changelog,
and releases use Semantic Versioning.

## [Unreleased]

### Added

- Fenced child-start processing with persisted pre-creation attempts, explicit
  known-started, known-absent, and unknown outcomes, stable idempotency keys,
  crash-safe redelivery, and durable policy-bound retries only after known
  absence.
- Version-pinned child workflow definitions, atomic parent history and
  `WorkChild` admission, bounded dispatch metadata, replayed child progress,
  known terminal outcomes, and deterministic parent orchestration.
- Persisted bounded signal and approval races with deterministic winner
  selection, immutable replayed race progress, and winner recording before
  later orchestration can advance.
- Deterministic bounded parallel activity admission that persists every branch
  schedule atomically, waits for all branch outcomes, and advances through
  explicit joins without treating partial admission as valid progress.
- Fenced activity work processing that persists attempt starts before external
  handlers, reconciles uncertain store commits, preserves unknown side-effect
  outcomes on crash redelivery, and durably schedules bounded known-safe retries.
- Independently fenced compensation work processing with start-before-effect
  persistence, conservative unknown rollback outcomes, and durable policy-bound
  retries that never report failed compensation as successful rollback.
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
- Stable PostgreSQL instance listing across active and archived views plus
  exact reconciliation for uncertain transition commit outcomes.
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
- Audited operator commands for activity retry, explicit compensation, and
  truthful manual compensation resolution, with audit history and due work
  committed through one optimistic transition.
- Deterministic ordered orchestration decisions for activity, timer, signal,
  and audited human-approval steps, including explicit waiting and truthful
  known-failure terminal outcomes.
- Bounded deterministic instance inspection and streaming history export over
  stable forward pages with explicit traversal limits.
- Immutable durable lifecycle history, deterministic replay, pinned definition
  verification, explicit persisted migration decisions, cancellation,
  termination, and continue-as-new outcomes.
- Immutable explicitly versioned workflow definitions, bounded activity and
  compensation policies, pinned registry lookup, and explicit migrations.

### Changed

- `StepChild` definitions now require `ChildDefinition`; existing child steps
  must pin the exact registered child name, version, and fingerprint.
