# event-sourcing PostgreSQL

`postgres` is the independently releasable PostgreSQL adapter for
`github.com/faustbrian/golib/pkg/event-sourcing`. Installing the core module
does not add `pgx` or database dependencies.

## Install

```sh
go get github.com/faustbrian/golib/pkg/event-sourcing/postgres
```

Apply the embedded migration through the repository's engine-neutral
`migrations` package or another runner that understands the same directives:

```go
source, err := migrations.NewFSSource(eventpostgres.Migrations(), ".")
if err != nil {
	return err
}
```

Migrations belong in a dedicated deployment job. Constructors do not inspect
or modify schema. The embedded history is forward-only: migrations expose no
rollback operation, so schema changes are reversed through a reviewed forward
repair or restore rather than destructive down SQL. The migration runner's
checksum-bound ledger makes repeated `Up` jobs idempotent and must serialize
concurrent deployment jobs; the embedded SQL is not independently idempotent.
The real-database upgrade suite reconstructs the only supported prior schema,
runs an event writer before and after the derived-state migration, preserves
history, and activates snapshots and projections without optional PostgreSQL
extensions across PostgreSQL 14 through 18.

## Append and read

```go
store, err := eventpostgres.New(pool, eventpostgres.Config{})
if err != nil {
	return err
}

persisted, err := store.Append(
	ctx,
	stream,
	eventsourcing.ExpectExactVersion(version),
	pending,
)
```

Pool-backed appends own one short PostgreSQL transaction. Validation and
statement failures are `CommitNotCommitted`; a commit error is
`CommitUnknown` and requires reconciliation by message ID before retry.
Messages are returned only after a successful commit.

When `Append` returns `CommitUnknown`, call `ReconcileAppend` with the exact
stream, expected-version value, and pending messages from the original attempt:

```go
messages, outcome, err := store.ReconcileAppend(
	ctx,
	stream,
	expected,
	pending,
)
switch outcome {
case eventsourcing.CommitCommitted:
	// Confirm the original save with messages. Do not append it again.
case eventsourcing.CommitNotCommitted:
	// Retrying the same immutable pending messages is safe.
case eventsourcing.CommitUnknown:
	// Stop and investigate err. A partial or divergent identity match is unsafe.
}
```

Reconciliation does not mutate stored data. It queries the bounded original
message-ID set and
accepts a commit only when every stored envelope matches in the original order
with contiguous stream versions and global positions. No identities means the
append did not commit. Before reading identities it locks the transactional
global-position allocator, so an original append still resolving its commit or
rollback must finish before absence can be reported. Use a bounded operation
context because this barrier waits behind in-flight appends. The adapter reads
only IDs and positions while holding that lock, releases it, and then fetches
the complete immutable envelopes so reconciliation does not amplify the global
append lock with payload transfer or decoding. `SELECT FOR UPDATE` requires a
locking-capable primary connection; a read-only transaction or replica cannot
reconcile commit ambiguity. A partial, reordered, divergent, or wrong-version
match
returns `ErrAppendReconciliationMismatch` and remains unknown, so it cannot turn
an ambiguous response into duplicate events by retrying. Database and scan
failures return redacted `ErrAppendReconciliationFailed` errors while preserving
their causes for `errors.Is` and `errors.As`.

Reads are bounded by the core read options. Returned iterators own their
`pgx.Rows`; callers must always call `Close`. Cancellation stops iteration and
closes the rows. A partial reader therefore retains its pool connection until
close or cancellation. The one-connection pool test proves a competing append
honors its caller deadline without allocating a global position, then succeeds
at the next gap-free position after the iterator releases capacity. Stream and
global ordering are ascending and stable.

## Caller-owned transactions

Use `NewTx` when event persistence must share an application transaction:

```go
tx, err := pool.Begin(ctx)
if err != nil {
	return err
}
defer tx.Rollback(cleanupCtx)

writer, err := eventpostgres.NewTx(tx, eventpostgres.Config{})
if err != nil {
	return err
}
plan, err := repository.PrepareSave(ctx, aggregate)
if err != nil {
	return err
}
if plan.Empty() {
	_, err = repository.ConfirmCommitted(aggregate, plan, nil)
	return err
}
staged, err := writer.StagePlan(ctx, plan)
if err != nil {
	return err // roll back the caller-owned transaction
}

if err := tx.Commit(ctx); err != nil {
	_, unknownErr := repository.MarkCommitUnknown(
		aggregate,
		plan,
		staged,
		err,
	)
	return unknownErr
}
result, err := repository.ConfirmCommitted(aggregate, plan, staged)
if err != nil {
	return err
}
_, err = repository.DispatchCommitted(ctx, result)
return err
```

`TxWriter` intentionally does not implement `eventsourcing.EventStore`, because
successful staging is not a durable append. `NewTx` never commits or rolls back
the supplied transaction. Returned messages are transactionally written but
are not durable until the caller commits. After any staging error, callers must
roll back because PostgreSQL may have marked the transaction failed. Commit
ambiguity belongs to the caller.

One `TxWriter` serializes its own concurrent `Stage` and `StagePlan` calls.
Waiting is bounded by each call's context and a canceled waiter is classified
as `CommitNotCommitted`. A `pgx.Tx` is still not concurrency safe: the caller
must use one writer per transaction and serialize direct transaction calls plus
all access through checkpoint, outbox, or other transaction wrappers. This
coordination must cover commit and rollback as well as staging.

`StagePlan` accepts the consumer-owned `AppendPlan` contract, which the core
`eventsourcing.SavePlan` implements. The lower-level `Stage` method remains
available to custom repositories that already own stable stream, expectation,
and pending-message values.

## Snapshots

`NewSnapshotStore` provides durable `eventsourcing.SnapshotStore` persistence.
Each save owns one short transaction, serializes concurrent writes to one
aggregate, accepts exact retries, and rejects aggregate or snapshot-schema
regressions and same-version state conflicts. State and metadata retain the
core bounds, and creation times round-trip at the core's canonical UTC
microsecond precision.

Snapshots remain derived acceleration data. Snapshot save is deliberately not
atomic with event append: a crash can leave a missing or stale snapshot, which
the snapshot manager follows with authoritative event history. Commit errors
return `ErrCommitOutcomeUnknown`; callers load the snapshot and compare every
observable field before retrying. Deletion is idempotent and exists for rebuild
and repair workflows.

## Projection checkpoints

`NewProjectionStore` implements `projection.ControlStore` with durable
optimistic checkpoints and running or paused operational state. A missing
projection is running with no checkpoint. `Pause` and `Resume` are idempotent;
checkpoint advancement is rejected while paused, and checkpoint reset requires
the exact expected position while paused.

Use `NewTxCheckpointWriter` when application read-model state and its checkpoint
share PostgreSQL:

```go
tx, err := pool.Begin(ctx)
if err != nil {
	return err
}
defer tx.Rollback(cleanupCtx)

if _, err := tx.Exec(ctx, updateReadModelSQL, arguments...); err != nil {
	return err
}
writer, err := eventpostgres.NewTxCheckpointWriter(
	tx,
	eventpostgres.Config{},
)
if err != nil {
	return err
}
if err := writer.Stage(ctx, name, expected, next); err != nil {
	return err
}

return tx.Commit(ctx)
```

`TxCheckpointWriter` intentionally does not implement
`projection.CheckpointStore`: successful staging is not durable until the
caller commits. It never commits or rolls back. After any error, the caller
must roll back. A commit error remains ambiguous, so recovery reads both the
application model and durable checkpoint before retrying.

One `TxCheckpointWriter` serializes its own concurrent `Stage` calls, and each
waiter observes its context. The caller must still serialize direct transaction
calls, commit, rollback, and access through event, outbox, or other wrappers.

Pool-owned checkpoint advancement and reset wrap commit failures with
`ErrCommitOutcomeUnknown`. Idempotent pause and resume operations can be
retried and reconciled through `Status`.

Real-database control races prove that checkpoint advancement, pause, resume,
and reset follow PostgreSQL row-lock order without producing a mixed state. A
checkpoint committed before pause is retained, reset queued before resume
removes the checkpoint, and resume queued before reset preserves it.

## Schema and ordering

The first migration creates:

- `event_sourcing.streams`, one locked optimistic-concurrency head per stream;
- `event_sourcing.positions`, one transactional global-position allocator; and
- `event_sourcing.messages`, immutable envelopes keyed by global position with
  unique message ID and stream-version indexes.

The second migration creates:

- `event_sourcing.snapshots`, one replaceable derived record per stream; and
- `event_sourcing.projections`, one durable checkpoint and run state per
  canonical projection name.

The real-database schema contract starts from an empty database, applies the
embedded migrations, verifies the complete message index set, rejects an
invalid event schema version through the named check constraint, and proves
the stream and global read shapes select their intended indexes. Query-plan
evidence disables sequential scans only inside the test transaction so small
fixtures cannot hide a missing index. A separate regression loads and analyzes
65,536 complete envelopes without planner overrides, then proves PostgreSQL 14
through 18 use exact `Limit` to `Index Scan` plans through
`messages_stream_version_idx` and `messages_pkey`. It rejects sequential and
bitmap heap scans. Production planning remains PostgreSQL's responsibility for
the deployed data distribution and statistics.

The allocator row deliberately serializes position assignment until commit.
This ensures a global reader cannot checkpoint a later committed event while
an earlier position remains uncommitted. It is a correctness-first tradeoff:
global ordering can become the append throughput bottleneck. Benchmarks and
capacity tests must include this lock rather than comparing against unordered
or sequence-only inserts. The real-database allocator suite queues eight
independent writers behind the singleton row, proves no writer completes while
that lock is held, and then observes unique, gap-free positions after release.
It also disables autovacuum, performs 2,048 committed allocator updates, runs
explicit `VACUUM (ANALYZE)`, and bounds the one-row relation's physical growth
to 64 KiB across PostgreSQL 14 through 18. Operators must still monitor lock
waits, relation size, and vacuum health under their actual append rate and
long-running transactions.

The real-database contention suite proves that concurrent writers on one new
stream produce one committed winner and only optimistic conflicts, while
concurrent independent streams all commit. Globally duplicate message IDs race
to exactly one durable winner without orphan streams or allocator gaps. A busy
caller-owned event or checkpoint writer serializes a second call until its
context expires, then a full rollback leaves neither call durable. The suite
also verifies unique, gap-free, ascending global positions through the public
global reader. This correctness evidence is not a throughput claim.

The PostgreSQL 14 through 18 integration matrix runs the public committed
event-store conformance profile for atomic append, every expected-version mode,
duplicate identity rejection, bounded reads, ownership, cancellation, and
iterator semantics. It separately runs the optional global-reader profile for
empty reads, cross-stream ordering, inclusive ranges, limits, ownership,
cancellation, and closure.

PostgreSQL `bigint` bounds stream versions and global positions to
`math.MaxInt64`, even though the storage-independent core types use `uint64`.
Exhaustion returns `eventsourcing.ErrVersionOverflow`.

## Operations

Configure statement, lock, and transaction timeouts on the caller-owned pool
or transaction. Keep transactions short; caller-owned transactions retain
stream and global-position locks until commit or rollback. The adapter starts
no goroutines and does not own pool shutdown.

Set server-side bounds in the pgx pool configuration before creating the pool,
and give each operation a shorter or equal context deadline:

```go
config, err := pgxpool.ParseConfig(connectionString)
if err != nil {
	return err
}
config.ConnConfig.RuntimeParams["statement_timeout"] = "5s"
config.ConnConfig.RuntimeParams["lock_timeout"] = "1s"
config.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "15s"
pool, err := pgxpool.NewWithConfig(ctx, config)
```

The real-database suite holds an append transaction open, proves that a
competing append reaches PostgreSQL `lock_timeout` with SQLSTATE `55P03` and
`CommitNotCommitted`, then proves a new append can succeed after rollback.
Timeout errors before commit are safe to retry under the normal optimistic
version contract. A commit error remains ambiguous and must be reconciled
before retry.

The caller-owned transaction suite also proves PostgreSQL serialization
failure `40001` and deadlock detection `40P01`. Both failures occur before
commit, preserve their `pgconn.PgError` cause, and report
`CommitNotCommitted`. The caller must roll back the entire failed transaction,
discard its staged result, reconstruct any application state read inside that
transaction, and retry under the original optimistic-version contract. The
adapter does not retry transactions because it cannot safely repeat
application reads, writes, callbacks, or side effects. The deadlock scenario
locks two streams in opposite application-defined order; ordinary adapter
appends acquire their own stream and global-position locks in one consistent
order, but a larger caller-owned transaction can introduce additional lock
ordering.

The same blocked-append fixture proves server-side `statement_timeout`
returns SQLSTATE `57014` and a caller deadline returns
`context.DeadlineExceeded`. Both are `CommitNotCommitted` because they occur
before commit. After the locking transaction ends, a new operation context can
retry safely; a canceled context and a PostgreSQL transaction left in an error
state must never be reused.

The backend-recovery suite durably appends history, terminates the pool's
PostgreSQL backend, and proves the existing pool can establish a replacement
connection, read unchanged history, and append the next stream and global
versions.

The server-restart suite stops and starts the PostgreSQL container, resolves
its new test endpoint, reconstructs the caller-owned pool, reads unchanged
history, and appends the next stream and global versions. The failover suite
builds a physical streaming replica from the primary, appends additional WAL,
waits for the replica to reach the exact stream version, and proves an append
to the still-read-only replica fails with SQLSTATE `25006` and
`CommitNotCommitted`. It then stops the primary, promotes the replica, and
proves ordered reads and the next append through the same store and pool.
Replication uses SCRAM authentication on an isolated Docker network.

These tests prove adapter behavior after reconnecting to a writable server and
preserve authoritative ordering across each pinned PostgreSQL 14 through 18
promotion.
They do not make pgx discover a new primary. Applications or managed-service
infrastructure own DNS, proxy, topology, fencing, retry, and pool-refresh
policy and must fault-inject that exact deployment path.

History is authoritative and must not be deleted as routine cleanup. Use
PostgreSQL backup plus tested point-in-time recovery. Restore the stream heads,
position allocator, and messages together, verify uniqueness and foreign keys,
then either restore or rebuild derived snapshots, checkpoints, and read models
as one coordinated operation. Any history repair requires a reviewed, auditable
procedure and must preserve message identity and ordering.

The backup/restore integration suite uses PostgreSQL's `pg_dump` custom format
and `pg_restore` into a clean database. It verifies identical event envelopes,
snapshot state, and projection checkpoints, then appends the next expected
stream version and global position. This proves logical backup compatibility
for every pinned supported major version; production point-in-time recovery,
replica promotion, encryption, retention, and storage-provider restoration
still need deployment-specific drills.

Partitioning, archival, and retention require an application-specific design.
Do not detach or delete partitions while active streams, legal retention, or
replay requirements still reference them. Tenant erasure and cryptographic
shredding policies belong to the application and its compliance review.
See the core [database-structure and capacity guide](../docs/database-structure.md)
for shared-table alternatives, partitioning prerequisites, archive semantics,
and deployment evidence.

Direct database-to-Kafka publication is not atomic. Use the optional outbox
adapter when durable asynchronous publication is required. Neither PostgreSQL
transactions nor Kafka producer transactions provide end-to-end exactly-once
delivery across both systems.

## Verification

The real-database suite follows the
[upstream support window](https://www.postgresql.org/support/versioning/) and
covers the versions supported there on 2026-07-27: PostgreSQL 14.23, 15.18,
16.14, 17.10, and 18.4. Each image is pinned by manifest digest. The matrix
runs serially so failover networks and database resources remain bounded:

```sh
make integration
```

Run one pinned major version while developing:

```sh
make integration-version POSTGRES_VERSION=18
```

The equivalent-work append benchmark compares the public event-store boundary
with direct application code over `pgx` in the same migrated PostgreSQL 18
database. Both paths construct the same validated envelope, open and commit
one transaction, create and lock a new stream, allocate a global position,
insert one message, and advance the stream head. The direct path is a baseline
cost floor: it does not provide the event store's reusable validation, error
classification, defensive result construction, or adapter contract.

The same harness separately measures a stale exact-version append against an
existing stream. Every timed attempt must return the typed not-committed
concurrency conflict, and the fixture verifies that neither the stream head nor
message count changed.

```sh
make benchmark-postgres BENCH_TIME=250ms BENCH_COUNT=20
```

The command starts a version-selected database through the integration harness;
each sample uses distinct stream and message identities. Record the resolved
image digest, Go and dependency versions, database settings, hardware, raw
output, and `benchstat` analysis before publishing a result.

The module is licensed under the [MIT License](LICENSE).
