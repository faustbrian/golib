# Examples And Integrations

## pgx and sqlc

Begin one `pgx.Tx`, bind sqlc queries with `WithTx(tx)`, and pass that exact
transaction to `Writer.Insert`. Commit only after both writes succeed. The
[quickstart](quickstart.md) contains the complete pattern. Queries still bound
to the pool break atomicity.

## queue

```go
queuePublisher, err := goqueue.New(queue)
worker, err := relay.New(store, queuePublisher, relay.Config{Owner: hostname})
```

The adapter sends canonical envelope JSON. Nil means broker acceptance, not
consumer completion; a later delivered-state failure can publish it again.
The compiled adapter example wires this publisher into a relay and runs in the
adapter test suite.

## idempotency consumers

Use envelope ID or a stable application key as delivery identity and
fingerprint canonical envelope bytes. Treat `OutcomeReplayed` as
acknowledgement, `OutcomeInProgress` as retryable, conflicts as incidents, and
storage failure as fail-closed. Commit business effects and deduplication in
one transaction or use a fencing invariant. If producing another outbox
message, pass the same `pgx.Tx` to the business write, `outbox`, and
`idempotency` `CompleteTx`. None of these measures changes relay delivery
from at least once. See the [complete integration guide](idempotency.md).

The root executable example deliberately delivers one envelope twice and
asserts that the consumer applies its effect once by stable envelope ID.
