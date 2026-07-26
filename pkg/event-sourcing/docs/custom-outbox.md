# Custom outbox boundary

Building a general transactional outbox is not an event-sourcing core
responsibility. The independently releasable
`github.com/faustbrian/golib/pkg/outbox` module owns envelope construction,
PostgreSQL persistence, leasing, retry, dead letters, replay, pruning, and the
bounded relay. It works without this library and does not import it.

The optional `adapters/gooutbox` module is the bridge for applications that
need both packages. It translates persisted event messages into public outbox
envelopes and stages both rows through the same PostgreSQL transaction.

## EventSauce migration

An EventSauce application that builds its own outbox should split the PHP
message-repository concern into explicit Go boundaries:

| Concern | Go boundary |
| --- | --- |
| Durable publishable value | `outbox.Envelope` |
| Same-transaction insertion | `outbox/postgres.Writer` with the caller's `pgx.Tx` |
| Leasing and state transitions | `outbox/relay.Store` |
| Broker acceptance | `outbox/relay.Publisher` |
| Event-message conversion | optional `event-sourcing/adapters/gooutbox.EnvelopeCodec` |
| Event plus outbox transaction | optional committed `gooutbox.Store` or caller-owned `gooutbox.Stager` |

Non-event-sourced applications use the outbox writer directly inside their
application transaction. They do not need any event-sourcing package:

```go
tx, err := pool.Begin(ctx)
if err != nil {
	return err
}
defer tx.Rollback(cleanupCtx)

if _, err := tx.Exec(ctx, updateApplicationState, arguments...); err != nil {
	return err
}
if err := writer.Insert(ctx, tx, envelope); err != nil {
	return err
}

return tx.Commit(ctx)
```

## Custom persistence

A custom relay store implements the small consumer-owned `relay.Store`
interface from the outbox module. It must prove:

- bounded atomic claims with opaque lease generations;
- one active owner for every state transition;
- lease extension and expiry recovery;
- delivered, retry, dead-letter, and release transitions;
- retry scheduling against a trustworthy storage clock;
- cancellation and rollback on every incomplete transition;
- duplicate publication after acknowledgement/settlement crashes;
- bounded hostile identifiers, errors, attempts, and allocations; and
- writable-primary readiness rather than connection success alone.

A custom publisher implements `Publish(context.Context, outbox.Envelope) error`.
Returning `nil` means the broker accepted the record under that adapter's
documented acknowledgement policy. It never means end-to-end exactly once.

The outbox module's [API reference](../../outbox/docs/api.md),
[architecture and crash matrix](../../outbox/docs/architecture.md), and
[verification audit](../../outbox/docs/audit.md) are authoritative for these
external contracts. Event-sourcing tests deliberately do not duplicate or
weaken those guarantees.

## Custom event/outbox bridge

If the first-party PostgreSQL writer or event-envelope mapping is unsuitable,
build a separate adapter that depends on both public modules. Keep both cores
independent. The bridge must:

1. stage the event batch with the exact expected stream version;
2. derive one immutable outbox envelope per persisted message;
3. write both batches through the exact same caller-owned transaction;
4. return no committed-store success before commit;
5. classify commit ambiguity without retrying under new message IDs; and
6. keep replay and reads side-effect free unless a separately named audited
   republish operation is explicitly invoked.

Do not import either package's internal schema or infer event identity from Go
type names. Run the public event-store conformance suite for the committed
event side and the outbox module's storage, relay, crash, race, and hostile-
input gates for the delivery side.
