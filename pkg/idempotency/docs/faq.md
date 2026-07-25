# Frequently asked questions

## Does this package provide exactly-once execution?

No. It provides durable ownership and replay for an idempotency record. An old
process can continue after its lease expires, so the business effect needs a
fence, unique constraint, transaction, provider idempotency key, or
reconciliation policy.

## Can two callers both complete the same retained record?

Not as the current owner. Completion atomically checks the owner token, fencing
token, state, and live lease. A stale caller is rejected. This does not prove
that the rejected caller performed no external side effect before completion.

## Why does a key have five fields?

Namespace, tenant, operation, caller, and value make collision domains
explicit. A raw client header alone rarely identifies the tenant and business
operation safely. Every field is part of the physical digest and persisted
semantic identity.

## What belongs in a fingerprint?

Every stable business input whose change should make key reuse a conflict.
Exclude trace IDs, connection data, retry counters, JSON whitespace, map order,
and other transport noise. Version the canonicalization policy and never
change its meaning in place.

## Should the idempotency key be secret?

No. Treat it as sensitive identifying data, but not as authentication or
authorization. Authenticate first, derive tenant and caller from trusted
identity, and never let possession of a key grant access to another caller's
result.

## How long should the lease be?

Longer than measured normal work plus tail jitter, and short enough for the
required recovery time. Long-running handlers should heartbeat well before
expiry. A lease is not a handler timeout and expiry does not stop work.

## How long should retention be?

At least the longest retry, redelivery, offline-client, reconciliation,
rolling-deployment, and business-fence window. Retention is also a privacy and
capacity cost. Deleting a record resets its fencing domain, so do not reuse the
logical key while another system still compares its numeric fences.

## What should happen when the backend is unavailable?

Fail closed by default. The package returns `unavailable` and does not
authorize execution. `AvailabilityAllowUntracked` is only for operations where
duplicate execution is explicitly acceptable; it provides no durable record,
ownership proof, heartbeat, completion, or replay.

## What if a completion call times out?

Its result is unknown. The backend may have committed before the response was
lost. Reconnect, inspect the record, and reconcile the business effect. Do not
immediately run the handler again based only on the transport error.

## When should I call `Fail` instead of `Release`?

Call `Fail` for a terminal outcome that future retries must replay. Call
`Release` only when no terminal result should be retained and the current
handler has stopped all side effects, allowing a later acquisition to start a
new fenced attempt. Transient handler errors are integration-specific: broker
redelivery or caller retry may be appropriate, but only after safe release or
lease recovery.

## Should I manually call `Expire`?

Usually no. Normal acquisition can take over an elapsed active lease directly.
`Expire` is useful when an operator or maintenance process needs the explicit
audit state. It cannot stop the old owner and must not replace reconciliation.

## Can I delete a stuck record?

Not as routine recovery. Deletion erases conflict, replay, ownership, and fence
history and can authorize duplicate execution. Diagnose the owner, lease,
backend, and business effect; use normal takeover or a reviewed reconciliation
procedure.

## Which adapter should I use?

Use PostgreSQL when it is the durable source of truth or when completion and a
business/outbox write can share a transaction. Use Valkey when low-latency
single-key transitions justify a dedicated Valkey 9 `noeviction` deployment.
Use memory only for tests and single-process tooling where restart loss is
acceptable.

## Can I share one Valkey deployment with an evictable cache?

Not safely under the adapter contract. An evicted unexpired record is
indistinguishable from a new key and may authorize duplicate execution. Use a
deployment or policy that guarantees `noeviction` and maintain capacity
headroom.

## Does Valkey failover preserve every acknowledged record?

Not by the adapter alone. One-key scripts are atomic on the executing primary,
and the failure suite proves that an ownership record synchronized to a replica
survives promotion. Asynchronous replication can still lose an acknowledged
write that did not reach the promoted replica. Configure persistence and
replica acknowledgement for the required durability window, and inspect after
failover before retrying an unknown result.

## What happens when the memory store reaches capacity?

Acquisition of a new key fails with `limit_exceeded`; the store does not evict
records. Existing keys can still be inspected, replayed, heartbeated, or
transitioned. `MaxRecords` defaults to 10,000 and is limited to 1,000,000. The
memory adapter remains process-local and is not a durable multi-process store.

## Does the package wait or retry while another owner is running?

No. `in_progress` is returned immediately. Any application polling or retry
loop must have a deadline, maximum attempts, and capped backoff. An unknown
backend result must be inspected before retry, and a possible business effect
must be reconciled first.

## Does response replay store every response?

No. Results and metadata are bounded by the semantic core, and integrations
apply smaller envelopes where needed. HTTP middleware buffers the handler
response and persists only configured replay headers. Oversized responses fail
terminally rather than being partially replayed.

## How do I instrument the package?

Record bounded outcomes, reason codes, latency, result sizes, cleanup, and
backend health at the application boundary. Never place raw keys or other
high-cardinality identity in metric labels. `idempotencylog` binds the standard
logger returned by `log`; `idempotencytelemetry` binds the meter provider
returned by `telemetry.Runtime` without using correlation as a metric label.

## How do I upgrade persisted formats?

Use versioned readers and a staged rolling deployment. Readers for every
record version still inside retention must be live before new writes begin.
See [migrations and compatibility](migrations-and-compatibility.md).
