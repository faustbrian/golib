# Event sourcing outbox adapter

`eventoutbox` composes the public event-sourcing PostgreSQL writer and the public
outbox PostgreSQL writer. Neither core imports the other. `Stager` writes event
rows and one outbox envelope per event through a savepoint inside an existing
caller-owned `pgx.Tx`. It releases that savepoint only after both batches stage
and rolls it back after any error. The adapter never commits or publishes from
the outer transaction and never claims exactly-once delivery.

## Quick start

Construct the outbox writer and envelope codec with the same limits:

```go
limits := eventoutbox.DefaultLimits()
outboxWriter, err := outboxpostgres.NewWriter(
	outboxpostgres.WriterConfig{
		Limits:       limits,
		MaxBatchSize: eventsourcing.MaxAppendMessages,
	},
)
if err != nil {
	return err
}
codec, err := eventoutbox.NewEnvelopeCodec(
	eventoutbox.FixedTopic("account-events"),
	limits,
)
if err != nil {
	return err
}
```

Prepare an aggregate save, stage it with the transaction sequence below, then
confirm the aggregate only after the caller observes a successful commit. The
safe durable-publication composition uses a no-op synchronous dispatcher for
external publication, an outbox relay after commit, and an idempotent consumer.
A dispatcher can still update in-process consumers after commit, but it is
independent of the outbox transaction.

## Relay setup

Relay lifecycle and publication policy remain owned by the independently
usable outbox module. Compose the committed rows with any outbox publisher:

```go
func runRelay(
	ctx context.Context,
	pool *pgxpool.Pool,
	publisher relay.Publisher,
) error {
	store, err := outboxpostgres.NewStore(
		pool,
		outboxpostgres.StoreConfig{},
	)
	if err != nil {
		return err
	}
	worker, err := relay.New(store, publisher, relay.Config{
		Owner:         "account-events",
		BatchSize:     100,
		Workers:       8,
		LeaseDuration: 30 * time.Second,
		MaxAttempts:   10,
		PollInterval:  time.Second,
	})
	if err != nil {
		return err
	}

	return worker.Run(ctx)
}
```

The relay claims only committed envelopes. Publisher success marks a lease
delivered; transient failure durably schedules the configured bounded backoff;
permanent or exhausted failure becomes a dead letter. The application owns the
bounded context, goroutine, shutdown, publisher, classifier, and operational
monitoring. The outbox Kafka publisher is the production Kafka boundary; a
direct event-store dispatcher is not part of this transaction.

## Transaction staging

The application must own the transaction and aggregate lifecycle:

```go
tx, err := pool.Begin(ctx)
if err != nil {
	return err
}
defer tx.Rollback(cleanupCtx)

stager, err := eventoutbox.NewStager(
	tx,
	eventpostgres.Config{},
	outboxWriter,
	codec,
)
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
messages, err := stager.StagePlan(ctx, plan)
if err != nil {
	return err // this call's savepoint was rolled back; caller should roll back
}
if err := tx.Commit(ctx); err != nil {
	_, unknownErr := repository.MarkCommitUnknown(
		aggregate,
		plan,
		messages,
		err,
	)
	return unknownErr
}
result, err := repository.ConfirmCommitted(aggregate, plan, messages)
if err != nil {
	return err
}
_, err = repository.DispatchCommitted(ctx, result)
return err
```

`Stager` deliberately does not implement `eventsourcing.EventStore`: a
successful stage is not a committed append. It owns only an internal
savepoint; it never commits the outer transaction, dispatches, or acknowledges
aggregate lifecycle state. If savepoint release or rollback itself fails,
`Stager` marks the outer transaction failed so a partially staged batch cannot
later be committed; the caller still owns and must perform its rollback.
Applications that use the ordinary repository `Save` method must migrate to
`PrepareSave`, `StagePlan`, and `ConfirmCommitted` as shown above.

`StagePlan` accepts the adapter-owned `AppendPlan` contract, which the core
`eventsourcing.SavePlan` implements. The lower-level `Stage` method remains
available to custom repositories that already own stable stream, expectation,
and pending-message values.

## Envelope mapping

The envelope ID and idempotency key are the event message ID. The raw encoded
event payload remains the outbox payload, and payload version `1` identifies
this adapter layout. Stable `es.*` metadata carries message ID, aggregate type
and ID, stream version, event name and schema version, content type, recorded
time, correlation, causation, tenant, partition, optional global position,
canonical JSON application metadata, and the `live` delivery marker.

The aggregate ID is the ordering key when it fits PostgreSQL outbox bounds.
Longer valid aggregate IDs use `sha256:<hex>` as a deterministic bounded
fallback, preserving per-aggregate ordering without truncation or collision-
prone prefixes. The original aggregate ID remains in the encoded envelope.

`EnvelopeCodec` defensively copies payloads and metadata. `Decode` rejects
unknown reserved metadata, malformed values, mismatched message,
idempotency, or ordering identities, and replay markers. Applications must
configure the outbox writer with the exact same limits as the codec.

Custom topic resolvers receive `eventoutbox.TopicMessage`, which exposes only
immutable fields available before persistence. Routing therefore completes
before a stream row is locked and cannot depend on store-assigned stream
versions or global positions. Migrate resolvers declared with
`eventsourcing.Message` by changing their parameter type to
`eventoutbox.TopicMessage`; the stable ID, stream, event, metadata, recorded time,
correlation, causation, tenant, and partition accessors remain available. A
resolver must be deterministic for identical input, bounded, side-effect-free,
concurrency-safe, and free of blocking I/O so exact retries retain canonical
envelope bytes.

## Transactions, crashes, and recovery

Dependency, context, stream, expectation, message-batch validation, and topic
resolution happen before the savepoint and therefore before any PostgreSQL
lock. Event staging, envelope construction from the resolved topics, and outbox
insertion happen inside one savepoint. Any such failure rolls back that
savepoint, so even a later caller commit cannot persist only the event or only
the envelope. A savepoint lifecycle failure marks the outer transaction failed,
returns `ErrTransactionStaging`, and requires caller rollback without performing
that rollback on the caller's behalf. The adapter cannot return `CommitUnknown`
because it never commits the outer transaction. If the caller's PostgreSQL commit returns
an error, the caller must classify the outcome as ambiguous and must not
acknowledge the aggregate.

The complete state sequence is:

1. validate immutable stream, expectation, messages, codec, and limits;
2. resolve every topic without holding a database lock;
3. open a nested savepoint;
4. lock and validate the stream version;
5. allocate global positions and stage every event row;
6. derive canonical envelopes and stage them in one outbox statement;
7. release the savepoint, leaving both batches pending in the outer transaction;
8. let the caller commit or roll back the outer transaction;
9. on a lost commit response, reconcile the complete prepared event and outbox
   identities before confirmation or exact retry;
10. confirm aggregate state only after a known or reconciled commit;
11. let the independent relay publish at least once, with consumer idempotency
    keyed by the envelope ID.

For an unknown commit, do not retry with new message IDs. Read the event store
and outbox by the prepared message IDs, compare the complete expected batch,
and choose acknowledge, retry, or repair through an application-owned,
audited recovery procedure.

An exact staging retry is safe only after rollback is known: the prepared IDs,
recorded times, payloads, metadata, stream versions, and derived envelope bytes
remain identical, while rolled-back sequence allocation leaves no durable row.
Identity reuse against a committed event or envelope is rejected, including
when payload bytes or metadata differ. After an ambiguous commit, reconcile by
message and envelope identity before deciding whether the exact plan may be
retried.

The relay publishes committed envelopes at least once. A crash after broker
acknowledgement but before the outbox delivered transition can publish a
duplicate. Kafka producer idempotence or transactions do not make the
PostgreSQL commit atomic with Kafka and do not provide end-to-end exactly once.

## Replay and retention

Event replay only uses `ReadStream` or `ReadGlobal`; reads never create outbox
records. There is intentionally no replay-to-outbox method. A future explicit
republish operation must be separately named, authorized, audited, and marked
as replay before it can create external side effects.

The real PostgreSQL integration scenario commits one event and envelope,
durably retries one transient relay failure, delivers on the second claim, and
then performs stream and global replay reads while proving the outbox row count
does not change.

Outbox envelopes are derived delivery records, but event history remains
authoritative. Backup and restore event rows and pending outbox rows to one
consistent PostgreSQL recovery point. Retention of delivered or dead outbox
rows must not delete event history.

## Operational limits

The PostgreSQL outbox schema bounds payloads at 1 MiB and encoded metadata at
128 KiB. Event metadata is JSON-encoded inside one metadata value, so hostile
escaping can expand it. Oversized values fail staging and require caller
rollback; they are never silently truncated. Keep events materially below the
absolute bounds and include adapter serialization overhead in capacity tests.

The adapter starts no goroutines and does not own the pool, transaction, relay,
or publisher lifecycle. A `Stager` and its transaction must be used serially;
concurrent writers use independent caller-owned transactions and `Stager`
instances. Run the relay under an application-owned bounded context with
explicit shutdown and retry policy.

## Performance evidence

The integration benchmark compares PostgreSQL appends in atomic batches of 1,
10, 100, and 1000 through the event store alone and through caller-owned
transaction staging with one same-transaction outbox row per event. Both paths
use the same validated message shapes, new-stream expectation, PostgreSQL
schema, connection pool, and commit boundary. It verifies exact durable row
counts and the stream-version and outbox-identity index plans after each
workload. Envelope encoding and insertion are deliberately measured only on
the adapter path because they are its production overhead. Run with:

```console
make benchmark BENCH_COUNT=10 POSTGRES_BENCH_TIME=3x
```

The canonical gate performs one untimed warm-up per mode, captures ten fresh
samples with the mode order balanced five-to-five, and measures at least three
committed appends per large-batch sample. It prints the pinned PostgreSQL image,
actual durability settings, hardware and Go environment, then reports both the
raw measurements and `benchstat` distribution analysis. These local
transactions do not measure relay publication or Kafka delivery.

## Adoption, compatibility, and migration

This pre-v1 adapter requires Go 1.26.6, pgx v5, the event-sourcing PostgreSQL
schema, and the outbox PostgreSQL schema declared by the sibling modules. The
codec and outbox writer must share exactly the same limits. PostgreSQL 18 is
the pinned integration target; support for another major version requires its
own current integration evidence.

Earlier pre-release revisions exported a committed `Store`. Migrate callers to
an application-owned `pgx.Tx`, `NewStager`, and `StagePlan`, then call
`Commit`, `MarkCommitUnknown`, `ConfirmCommitted`, and `DispatchCommitted` in
the explicit order shown above. There is no database migration for this API
change. Existing event and outbox rows remain compatible because the envelope
format and schemas are unchanged.

## FAQ

### Does staging mean the events are committed?

No. A successful `Stage` or `StagePlan` only means both row batches were
accepted by the caller's transaction. Durability begins only after the caller
observes a successful PostgreSQL commit.

### Can the adapter publish directly to Kafka?

No. The independently operated relay claims committed outbox envelopes and may
publish duplicates. Consumers must be idempotent by envelope identity.

### Can an ambiguous commit be retried immediately?

No. Reconcile the prepared message IDs and envelope IDs first. Blind retry can
turn a successful but unacknowledged commit into a duplicate-identity or stream
version conflict.

### Does replay enqueue historical events?

No. Replay and read APIs remain outside this staging adapter and have no outbox
side effects.
