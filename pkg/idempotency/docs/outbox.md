# Transactions and outbox records

`postgres.Store.CompleteTx` joins idempotency completion to an
application-owned pgx transaction. Use it when the business effect or an outbox
row lives in the same PostgreSQL database.

```go
begin, err := service.Begin(ctx, idempotency.BeginRequest{
	Acquire: idempotency.AcquireRequest{
		Key: key,
		Fingerprint: fingerprint,
		Lease: 30 * time.Second,
	},
})
if err != nil {
	return err
}
if !begin.Execute {
	return handleExistingOutcome(begin)
}

tx, err := pool.Begin(ctx)
if err != nil {
	return err
}
defer tx.Rollback(context.WithoutCancel(ctx))

if err := updateBusinessRow(ctx, tx, begin.Record.FencingToken); err != nil {
	return err
}
record, err := idempotencyoutbox.InsertAndComplete(
	ctx,
	tx,
	outboxWriter,
	envelope,
	store,
	idempotency.CompleteRequest{
		Ownership: begin.Record.Ownership(),
		Result: encodedResponse,
		Metadata: map[string]string{"content-type": "application/json"},
	},
)
if err != nil {
	return err
}
if err := tx.Commit(ctx); err != nil {
	return err
}
return replay(record.Result)
```

`outboxWriter` is a `*outbox/postgres.Writer`, and `envelope` is a bounded
`outbox.Envelope`. `InsertAndComplete` calls `Writer.Insert` before
`Store.CompleteTx` using the exact same transaction. The completion takes the
same advisory and row locks as ordinary completion and rechecks ownership,
fencing, state, and lease. A rollback removes the business effect, envelope,
and completion; a commit makes all three visible together.

Do not call `InsertAndComplete` inside HTTP, JSON-RPC, queue, or command wrappers
that already complete automatically after their handler returns. Use the
direct service flow above. The helper does not commit or roll back: callers
must return on either insert or completion failure so the deferred rollback
runs, and commit only after every transaction-bound business write succeeds.

The root module does not import `outbox`. A pinned Go 1.26 compatibility
module proves that `outbox/postgres.Writer` satisfies the generic writer
contract at every CI and release gate.

## Boundaries

- Acquisition commits before application work, so concurrent retries observe an
  active owner.
- The application transaction must finish before the lease expires. A stale
  transaction fails completion and must roll back its business effect.
- A commit response can be lost after PostgreSQL commits. Treat that as unknown,
  reconnect, and inspect; do not rerun based only on the transport error.
- External HTTP calls, other databases, and brokers cannot join the PostgreSQL
  transaction. Use fencing, provider idempotency keys, reconciliation, and the
  outbox publisher's own retry contract.
- Outbox delivery deduplication is separate from producer transaction atomicity.
  Consumers still need stable delivery identities or business constraints.
