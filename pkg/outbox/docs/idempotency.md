# Consumer Idempotency With idempotency

Outbox delivery is at least once. Consumer deduplication reduces repeated
effects but does not change that delivery contract or create a distributed
exactly-once guarantee.

## Identity and fingerprint

Use the envelope ID as the final component of a fully scoped idempotency key.
Use canonical envelope bytes as the fingerprint so reuse of an ID with
different content is a conflict rather than a silent acknowledgement.

```go
key, err := idempotency.NewKey(
    "outbox-consumer", tenantID, envelope.Topic, consumerName, envelope.ID,
)
if err != nil {
    return err
}

fingerprint, err := idempotency.NewFingerprint(
    "outbox-envelope-v1", envelope.CanonicalJSON(),
)
if err != nil {
    return err
}
```

The tenant and consumer components must come from trusted configuration or
authenticated context, not untrusted envelope metadata.

## Acquire before executing

Acquire durable ownership with fail-closed availability. A storage outage must
not silently permit an untracked side effect.

```go
begin, err := service.Begin(ctx, idempotency.BeginRequest{
    Acquire: idempotency.AcquireRequest{
        Key: key, Fingerprint: fingerprint, Lease: 30 * time.Second,
    },
    Availability: idempotency.AvailabilityFailClosed,
})
if err != nil {
    return retry(err)
}

switch begin.Outcome {
case idempotency.OutcomeAcquired,
    idempotency.OutcomeStaleOwnerTakeover:
    // This attempt owns execution. Continue below.
case idempotency.OutcomeReplayed:
    return acknowledge(begin.Record.Result)
case idempotency.OutcomeInProgress:
    return retryLater()
case idempotency.OutcomeConflict,
    idempotency.OutcomeTerminalFailure:
    return incident(begin.Outcome)
default:
    return retryUnexpected(begin.Outcome)
}
```

The handler must extend the idempotency lease when work can outlive it. A stale
owner must stop when heartbeat or fenced completion reports lost ownership.

## Commit the effect and completion together

With the PostgreSQL `idempotency` store, persist the business effect and
idempotency completion in the same caller-owned `pgx.Tx`. If the consumer also
produces an outbox envelope, pass that same transaction to `Writer.Insert`.

```go
tx, err := pool.Begin(ctx)
if err != nil {
    return err
}
defer tx.Rollback(ctx)

if _, err := tx.Exec(ctx, applyBusinessEffectSQL, args...); err != nil {
    return err
}
if produced != nil {
    if err := outboxWriter.Insert(ctx, tx, *produced); err != nil {
        return err
    }
}
if _, err := idempotencyStore.CompleteTx(
    ctx,
    tx,
    idempotency.CompleteRequest{
        Ownership: begin.Record.Ownership(),
        Result:    boundedReplayResult,
    },
); err != nil {
    return err
}

return tx.Commit(ctx)
```

Never call the non-transactional `Complete` method before committing the
business effect. That can record success while the effect rolls back. Never
commit the effect before durable completion unless the effect has an
independent fencing invariant; that creates the usual crash window between
the two commits.

## Broker acknowledgement

Acknowledge the broker only after the transaction commits. On an ambiguous
commit result, inspect durable business and idempotency state before forcing a
retry or acknowledgement. Replayed results, disaster recovery, retention
expiry, and operator intervention can still lead to another delivery, so the
business invariant must remain safe under repetition.
