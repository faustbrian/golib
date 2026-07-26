# Event sourcing outbox adapter

`gooutbox` composes the public event-sourcing PostgreSQL writer and the public
outbox PostgreSQL writer. Neither core imports the other. The adapter provides:

- `Store`, a committed `eventsourcing.EventStore` that owns one short
  PostgreSQL transaction per append; and
- `Stager`, a lower-level writer for an existing caller-owned `pgx.Tx`.

Both paths write event rows and one outbox envelope per event through the same
transaction. They never publish to Kafka, dispatch consumers, or claim
exactly-once delivery.

## Quick start

Use `Store` with the ordinary aggregate repository:

```go
limits := gooutbox.DefaultLimits()
outboxWriter, err := outboxpostgres.NewWriter(
	outboxpostgres.WriterConfig{
		Limits:       limits,
		MaxBatchSize: eventsourcing.MaxAppendMessages,
	},
)
if err != nil {
	return err
}
codec, err := gooutbox.NewEnvelopeCodec(
	gooutbox.FixedTopic("account-events"),
	limits,
)
if err != nil {
	return err
}
store, err := gooutbox.NewStore(
	pool,
	eventpostgres.Config{},
	outboxWriter,
	codec,
)
if err != nil {
	return err
}

// Supply store as RepositoryConfig.Store. AggregateRepository.Save appends
// events and outbox rows atomically, acknowledges only after commit, and then
// runs the explicitly configured post-commit dispatcher.
```

The safe durable-publication composition uses a no-op synchronous dispatcher
for external publication, an outbox relay after commit, and an idempotent
consumer. A dispatcher can still update in-process consumers after commit, but
it is independent of the outbox transaction.

Use `Stager` only when an application already owns the transaction:

```go
tx, err := pool.Begin(ctx)
if err != nil {
	return err
}
defer tx.Rollback(cleanupCtx)

stager, err := gooutbox.NewStager(
	tx,
	eventpostgres.Config{},
	outboxWriter,
	codec,
)
if err != nil {
	return err
}
messages, err := stager.Stage(ctx, stream, expected, pending)
if err != nil {
	return err // roll back; nothing was committed by Stager
}
if err := tx.Commit(ctx); err != nil {
	// The durable outcome is ambiguous. Reconcile every message ID against
	// both stores before retrying or acknowledging aggregate changes.
	return err
}
_ = messages // now durable; acknowledge through the owning lifecycle.
```

`Stager` deliberately does not implement `eventsourcing.EventStore`: a
successful stage is not a committed append. It never commits, rolls back,
dispatches, or acknowledges aggregate lifecycle state.

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

## Transactions, crashes, and recovery

`Store.Append` returns:

- `CommitNotCommitted` when begin, event staging, envelope mapping, or outbox
  insertion fails; and
- `CommitUnknown` when PostgreSQL commit returns an error.

For an unknown commit, do not retry with new message IDs. Read the event store
and outbox by the prepared message IDs, compare the complete expected batch,
and choose acknowledge, retry, or repair through an application-owned,
audited recovery procedure.

The relay publishes committed envelopes at least once. A crash after broker
acknowledgement but before the outbox delivered transition can publish a
duplicate. Kafka producer idempotence or transactions do not make the
PostgreSQL commit atomic with Kafka and do not provide end-to-end exactly once.

## Replay and retention

Event replay only uses `ReadStream` or `ReadGlobal`; reads never create outbox
records. There is intentionally no replay-to-outbox method. A future explicit
republish operation must be separately named, authorized, audited, and marked
as replay before it can create external side effects.

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
or publisher lifecycle. Run the relay under an application-owned bounded
context with explicit shutdown and retry policy.

