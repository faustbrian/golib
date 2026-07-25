# PostgreSQL Schema And Operations

## Migrations

`postgres.Migrations()` returns an `fs.FS` containing one canonical
`000001_create_outbox.sql` file with `-- +migrations Up` and
`-- +migrations Down` sections. A migration runner, including
`migrations`, can consume that filesystem without exposing runner-specific
types through the package API.

The initial schema creates `outbox_messages`, its hot-set and retention
indexes, and immutable `outbox_replay_audit` rows. Apply migrations before
constructing a writer or relay store. Schema and delivery-semantics changes are
SemVer-sensitive public contracts.

The embedded SQL targets `public.outbox_messages` and
`public.outbox_replay_audit`. `WriterConfig` and `StoreConfig` can select a
different schema or message table, but the application then owns an equivalent
versioned migration. Keep the replay audit table in the selected schema, retain
all constraints and indexes, and test every transition against that layout.
Configuration does not rewrite embedded migration SQL.

## States and important columns

- `pending`: eligible after `available_at`; no lease or terminal timestamp.
- `leased`: owner, opaque token, and expiry are all present.
- `delivered`: `delivered_at` is present and lease fields are clear.
- `dead`: `dead_lettered_at` is present and lease fields are clear.

Database constraints reject inconsistent state-field combinations, attempts
outside 0–10,000, empty identifiers/topics, invalid payload versions, payloads
larger than 1 MiB, non-object metadata, non-string metadata values, encoded metadata
larger than 128 KiB, lease and audit identifiers larger than 255 bytes, replay
reasons larger than 4096 bytes, failure diagnostics larger than 4096 bytes,
non-finite message or replay-audit timestamps, and timestamps outside envelope
years 0000–9999. The writer performs matching actual-value validation before
SQL even when application limits are broader.

Claim increments attempts with saturation at 10,000. This keeps the relay's
absolute policy ceiling aligned with direct SQL while leaving a boundary row
claimable for delivery or dead-letter transition.

## Indexes

- `outbox_messages_claim_idx`: available/created/ID order for the non-terminal
  hot set.
- `outbox_messages_lease_expiry_idx`: expired lease recovery.
- `outbox_messages_ordering_idx`: non-empty ordering-key serialization.
- delivered and dead retention indexes: bounded terminal maintenance.
- partial unique idempotency index: non-empty writer keys only.

Keep pending and leased rows a small fraction of retained history. Monitor
query plans and vacuum behavior at representative backlog sizes.

Integration tests inspect claim, delivered-retention, and dead-retention plans
at empty, normal 150-row, and large terminal-heavy 40,100-row sizes. Sequential
scans are valid for empty and small tables. At large size all three queries
must avoid a sequential scan and use their matching partial index on every
supported PostgreSQL major.

## Claim coordination

Claims run as one CTE update using `FOR UPDATE SKIP LOCKED`. Concurrent relay
instances obtain disjoint records without a coordinator. Scoped ordering adds
a correlated earliest-non-terminal predicate before locking; it does not add a
global lock.

`SKIP LOCKED` is intentionally not strict FIFO fairness. A row lock can make
the oldest candidate temporarily invisible while later rows are claimed; the
oldest row becomes eligible again after lock release. Table-level lock
acquisition is not skipped. Configured `lock_timeout`, `statement_timeout`, or
context deadlines therefore surface as claim errors without a partial lease.
The integration suite asserts PostgreSQL codes `55P03` and `57014` for the two
server timeout modes.

Claims execute under READ COMMITTED by default and are also valid as single
statements under SERIALIZABLE. PostgreSQL serialization or deadlock errors are
returned to the caller; the library does not hide them with an internal retry
that could multiply polling or publisher retry policy. Live SQLSTATE `40001`
and `40P01` cases prove a losing caller-owned application/outbox transaction
rolls back both records atomically.

Lease deadlines use the database clock. Late updates require the original
token and affect exactly one still-leased row or return `ErrLeaseLost`.
Retry accepts a bounded duration and adds it to `clock_timestamp()` inside the
token-qualified update. A relay host clock skewed by 24 hours therefore still
schedules a 30-second policy delay for approximately 30 seconds.

## Retention and archival

Pruning is intentionally limited to delivered or dead rows older than a
supplied cutoff through separate APIs. Never delete pending or leased rows,
even when they appear old. Retaining or archiving dead letters preserves
incident evidence; deleting them also removes their replay source.

Use `ArchiveAndPruneDelivered` when policy requires archive-before-delete. It
locks a bounded terminal batch with `SKIP LOCKED`, calls the supplied archive
hook while the transaction remains open, and deletes only after success.
Archive implementations must deduplicate by envelope ID because a successful
archive followed by an ambiguous PostgreSQL commit can repeat the hook.

Use `PruneDelivered` only when direct permanent deletion is intentional.
The parallel dead-letter APIs are `ArchiveAndPruneDead` and `PruneDead`.

Both paths use a strict timestamp `< cutoff`. Locked candidates are skipped,
archive and replay serialize through row locks, and long snapshots can retain
dead tuples until they end. Keep maintenance batches short, monitor
`n_dead_tup`, transaction age, and autovacuum progress, then use normal
`VACUUM (ANALYZE)` after clearing a transaction-age incident. The recovery
suite exercises the long-snapshot visibility boundary and a subsequent
VACUUM.

Replay and archive own explicit transactions. Their deferred rollback uses a
detached five-second deadline so caller cancellation does not suppress cleanup
and a network partition cannot leave the cleanup call unbounded.

## Partitioning

The embedded schema is deliberately unpartitioned. Start with that table and
the partial indexes above. Consider time-based partitions only when retained
terminal history, vacuum cost, or maintenance windows justify the operational
burden.

Partition boundaries must not allow dropping a partition containing pending,
leased, or retained dead records. A custom partitioned table is an
application-owned schema and is outside the embedded migration guarantee;
route and verify every supported state transition and replay-audit operation
against its partition keys before adopting it.

## Timeouts and connections

Use primary read-write connections. `Store.Ping` checks both
`transaction_read_only` and `pg_is_in_recovery()` so relay readiness rejects a
read-only route with `ErrNotWritable`; a mere TCP/SQL round trip is not enough.
Configure context deadlines plus sensible PostgreSQL `statement_timeout` and
`lock_timeout` values at the application boundary. Do not run writers or
relays against replicas or read-only sessions.

Pool sizing must account for relay worker concurrency, application
transactions, and administrative operations. Workers publish outside a
database transaction; leases bound recovery if publication stalls.
