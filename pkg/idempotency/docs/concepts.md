# Idempotency and related mechanisms

These mechanisms solve different failure modes. Production designs commonly
need more than one of them.

| Mechanism | What it establishes | What it does not establish |
| --- | --- | --- |
| Idempotency record | A stable request identity, one current leased owner, and a bounded terminal replay within retention. | That an expired owner stopped or that an external side effect happened once. |
| Deduplication | A delivery or source identity was already accepted or processed. | Ownership while processing, result replay, or atomic business effects unless those are added explicitly. |
| Lease or lock | One current holder may act for a bounded interval. | Permanent request history, replay, or safety after the holder's authority expires. |
| Fencing token | A monotonic attempt number that a protected resource can reject when stale. | Safety when the resource ignores the token or after the fencing domain is deleted and recreated. |
| Unique constraint | One business identity can be inserted once in one database. | Replay payloads, in-progress ownership, or coordination with unrelated systems. |
| Retry policy | When and how a caller makes another attempt. | Whether another attempt is safe. |
| Transaction | A set of writes to one transactional resource commits or rolls back together. | Atomicity with an unrelated datastore, broker, or HTTP service. |
| Outbox | A business write and message intent commit in one database transaction. | Exactly-once broker delivery or consumer effects; consumers still deduplicate. |

## What this package guarantees

Within one retained record, atomic store transitions elect one current owner,
reject stale ownership proofs, increment the fencing token on every new
attempt, reject a changed fingerprint, and preserve a bounded terminal result
for replay. PostgreSQL and Valkey use backend-authoritative time.

The package does not claim exactly-once execution. A process may cross a side
effect boundary, lose its lease before recording completion, and continue
running while a takeover begins. The application must protect the effect with
the fence, a business unique constraint, reconciliation, or a transaction that
also records idempotency completion.

## Choosing a composition

- For a create operation in PostgreSQL, combine the idempotency record with a
  business unique constraint and `CompleteTx`.
- For an external payment or provider API, send a stable provider idempotency
  key when supported and reconcile by that identity after unknown responses.
- For a queue handler, use durable consumer ownership, commit the business
  effect with the fence, then acknowledge according to the
  [settlement mapping](queue.md).
- For a webhook, verify authenticity before looking up delivery state, then
  deduplicate by the provider's stable delivery identifier.
- For a long-running import, identify each source record independently and
  heartbeat only while the current attempt can still stop or fence its writes.

Idempotency keys are not locks for arbitrary resources. Scope keys to a stable
business operation and keep resource concurrency rules in the business model.
