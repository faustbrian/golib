# Changelog

All notable changes to this module are documented here.

## Unreleased

### Changed

- Prove an append whose PostgreSQL backend dies during a blocked statement is
  not committed, reconciles as absent, and retries without duplicate events or
  a global-position gap.
- Redact ordinary PostgreSQL driver diagnostics behind
  `ErrDatabaseOperationFailed` while preserving the cause for `errors.Is` and
  `errors.As` retry and SQLSTATE classification.
- Reject message and snapshot rows whose encoded JSON metadata exceeds the
  schema's 64 KiB limit before decoding or allocating the metadata map.
- Serialize concurrent staging calls made through one caller-owned transaction
  event or checkpoint writer with context-bounded waiting. Direct transaction
  access and separate wrappers remain caller-serialized because `pgx.Tx` is not
  concurrency safe.
- Make the embedded schema history forward-only. Migration runners have no
  rollback operation, and repeated `Up` jobs remain idempotent through the
  durable ledger.
- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

### Added

- PostgreSQL 14 through 18 commit-response fault evidence proving a durably
  committed append reports unknown when its response is dropped, repeated
  reconciliation remains read-only, and no duplicate event is created
- concurrent PostgreSQL 14 through 18 deployment-job evidence proving eight
  jobs serialize through one advisory lock, each migration is applied once,
  and completed ledger state is safe for the remaining jobs to observe
- PostgreSQL 14 through 18 allocator evidence proving independent writers queue
  only at the singleton ordering row, resume with unique gap-free positions,
  and keep vacuumed single-row storage growth within 64 KiB after 2,048 updates
- PostgreSQL 14 through 18 upgrade evidence from the only prior schema version,
  preserving event writers and history across the derived-state migration
  without optional extensions
- realistic-volume PostgreSQL 14 through 18 plan evidence proving complete
  stream and global envelope reads use their exact indexes without sequential
  or bitmap heap scans
- real PostgreSQL one-connection pool evidence proving partial readers retain
  their connection until close or cancellation, blocked appends honor caller
  deadlines without allocating a global position, and released capacity is
  reusable
- real PostgreSQL contention evidence proving globally duplicate message IDs
  commit exactly once without orphan streams or allocator gaps, and busy
  caller-owned event and checkpoint staging remains rollback-safe
- ordered PostgreSQL lock-race evidence for checkpoint advancement, pause,
  resume, and reset without mixed projection state
- read-only append reconciliation by the exact original message identities,
  envelopes, ordering, and expected version, distinguishing confirmed commit,
  confirmed absence, and unsafe partial or divergent durable state
- a serial real-database compatibility matrix for every upstream-supported
  PostgreSQL major version from 14 through 18, using exact current-minor image
  digests and the complete conformance, contention, recovery, promotion, and
  backup/restore suite
- caller-owned transaction staging directly from a prepared core aggregate
  save plan without acknowledging or dispatching before commit
- an equivalent-work PostgreSQL append benchmark comparing the public event
  store with direct `pgx` application code as the documented cost floor
- a real PostgreSQL optimistic-conflict benchmark proving rejected attempts do
  not advance the stream or append messages
- real PostgreSQL server-restart and streaming-replica promotion evidence
  preserving history plus the next stream and global positions
- real PostgreSQL long-stream evidence for bounded pagination across 2,048
  sequential events
- real PostgreSQL schema-contract evidence for envelope constraints, the
  complete message index set, and indexed stream and global read plans
- real PostgreSQL contention evidence for one optimistic hot-stream winner,
  independent-stream success, unique positions, and gap-free global read order
- real PostgreSQL lock-timeout evidence proving a blocked append is not
  committed and can be retried after the locking transaction rolls back
- real PostgreSQL serializable-transaction evidence preserving SQLSTATE
  `40001`, classifying the stage as not committed, and succeeding after a full
  rollback and retry
- real PostgreSQL deadlock evidence preserving SQLSTATE `40P01`, classifying
  the victim stage as not committed, and proving both transactions leave no
  staged event writes after rollback
- real PostgreSQL statement-timeout and mid-operation context-deadline
  evidence classifying blocked appends as not committed and retryable only
  with a fresh operation context after lock release
- real PostgreSQL read-only replica evidence preserving SQLSTATE `25006`
  before promotion and proving the same store becomes writable only after
  explicit promotion
- real PostgreSQL backend-termination evidence proving the existing pool can
  replace a lost connection and resume reads and ordered appends without
  losing durable history
- logical `pg_dump` and `pg_restore` evidence preserving event history,
  snapshots, projection checkpoints, stream heads, and global ordering
- public committed event-store conformance against real PostgreSQL 14 through
  18 databases
- public optional global-reader conformance against real PostgreSQL 14 through
  18 databases
- atomic PostgreSQL stream append with optimistic concurrency and duplicate
  message-ID protection
- bounded stream and global-position reads with caller-closed iterators
- caller-owned transaction composition through `NewTx`
- transactional global-position allocation that preserves committed ordering
  under concurrent writers
- forward-only, engine-neutral schema migration and real PostgreSQL lifecycle
  coverage
- durable snapshot storage with atomic non-regression, exact-retry, conflict,
  deletion, and corrupt-input semantics
- durable projection checkpoints with pause, resume, optimistic advancement,
  expected reset, and same-transaction application read-model composition
- redacted unknown-commit classification for pool-owned snapshot and
  checkpoint transactions
