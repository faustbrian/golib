# Delivery Guarantees

## Transaction boundary

`postgres.Writer` accepts only a caller-owned `pgx.Tx`. Atomic persistence is
promised only if the application mutation and every outbox insert execute on
that exact transaction and its commit succeeds. The writer does not begin,
commit, roll back, or hide a second transaction.

Any writer error requires the caller to roll back the complete transaction.
PostgreSQL evidence covers an already-aborted transaction, canceled writer
context, caller panic with deferred rollback, connection loss before the
writer call, connection loss after the outbox insert but before commit,
serialization failure, and deadlock victim selection. In every losing case
neither the application row nor the envelope becomes durable; the successful
isolation competitor commits both.

This works with `sqlc` when generated queries and `postgres.Writer` receive the
same `pgx.Tx`. Calling generated application queries through a pool while the
outbox uses a transaction, or the reverse, is not atomic.
The real PostgreSQL suite deliberately commits the outbox through a different
transaction and rolls back the application transaction; the resulting 0/1
row mismatch proves that this caller error is outside the package guarantee.

The writer revalidates every envelope against its configured limits and
new-record invariants before executing SQL. Exported struct construction
therefore cannot bypass ID, payload, metadata byte/entry, topic, ordering-key,
idempotency-key, attempt, version, or timestamp bounds. Insert batches default
to 100 and cannot exceed PostgreSQL's parameter ceiling. The embedded schema
also enforces absolute payload, encoded-metadata, identity, replay, and
diagnostic ceilings so direct SQL cannot create an unbounded relay record.
Attempts are constrained to 0–10,000 and claim increment saturates at 10,000;
a boundary record remains claimable so the relay can deliver or dead-letter it.
Every persisted message and replay-audit timestamp must be finite, preventing
PostgreSQL infinity values from becoming undecodable relay or operator rows.
Those timestamps also share the envelope's canonical year range of 0000–9999,
so direct SQL cannot create non-RFC3339 publisher data.

## Delivery

Delivery is at least once:

1. PostgreSQL commits the application write and outbox envelope.
2. A relay leases the envelope.
3. The publisher accepts it.
4. The relay marks it delivered with the current lease token.

If step 3 succeeds and step 4 fails or has an ambiguous result, the lease
eventually expires and another relay can publish the envelope again. The
library cannot distinguish that case from publisher failure without a
distributed transaction, which is intentionally out of scope.

The PostgreSQL integration suite injects that exact delivered-update failure,
asserts the accepted row remains leased, expires it, and proves a second relay
publishes it again before marking it delivered.

Publisher success means only that the configured adapter reported acceptance.
It does not prove consumer processing or exactly-once delivery.

## Duplicates and consumers

Consumers must deduplicate using a stable application key or envelope ID.
`IdempotencyKey` can enforce uniqueness among outbox inserts when non-empty,
but it does not make publishing or consumer side effects exactly once.

Replay is an explicit duplicate-producing operation. It resets attempts and
makes delivered or dead-lettered records pending again while writing an
operator audit row. `Store` denies replay unless `ReplayAuthorizer` approves
the complete request. Hook errors and panics return fixed
`ErrReplayUnauthorized` without database mutation or policy-detail leakage.

## Ordering

Default claims provide no ordering guarantee beyond deterministic candidate
selection within a single PostgreSQL statement.

Optional serialization modes provide:

- ordering-key mode: at most the earliest non-terminal record for each
  non-empty ordering key can be leased; empty keys remain unordered;
- topic mode: at most the earliest non-terminal record for each topic can be
  leased.

Ordering is scoped, never global. A future-scheduled earlier record blocks
later records in its scope. A dead-lettered record is terminal and stops
blocking later records. Publisher or consumer infrastructure can still apply
its own ordering semantics after acceptance.

## Leases and ownership

Each claim receives an opaque generation token. Delivery, retry, dead-letter,
extension, and release require the current token and fail with `ErrLeaseLost`
after expiry, reclaim, or another terminal transition. This prevents a paused
or late relay from acknowledging a newer owner's lease.

During publication the relay renews the lease at a configured interval shorter
than its lease duration. Renewal failure cancels the publisher context and
prevents a state transition with uncertain ownership. A publisher that ignores
context can still delay shutdown. Adapters whose upstream accepts context must
propagate it. The pinned `queue` producer is synchronous and has no context
parameter, so its worker-specific request and network timeouts must provide the
in-flight bound.

PostgreSQL `clock_timestamp()` is authoritative for claim eligibility, lease
deadlines, retry delays, delivery timestamps, replay defaults, and lease
release. `Retry` persists a bounded duration relative to the database clock;
relay host clock skew cannot extend that delay or decide lease ownership.

## Retention

`PruneDelivered` deletes only rows already in `delivered` state whose
`delivered_at` is older than the caller's cutoff. Pending, leased, and dead
records are never deleted by that primitive. Batches are bounded and locked
with `SKIP LOCKED`.

`ArchiveAndPruneDelivered` holds row locks and the database transaction open
while its archive hook runs. A hook failure rolls back without deleting the
selected records. If archival succeeds but the database commit fails or is
ambiguous, the records can be selected and archived again. Archive storage
must therefore be idempotent by envelope ID.

`PruneDelivered` intentionally bypasses archival. Use it only when permanent
deletion is the retention policy; use `ArchiveAndPruneDelivered` whenever an
archive is mandatory.

Dead letters follow the parallel `PruneDead` and `ArchiveAndPruneDead`
contracts. Neither path can select pending, leased, or delivered records.
Deleting a dead letter removes the ability to replay it through the library.

Both terminal timestamps use a strict `< cutoff` comparison. Concurrent
operator locks are skipped, archive and replay serialize on the same row, and
a long repeatable-read snapshot can continue seeing a pruned row until that
transaction ends. Long transactions therefore delay VACUUM reclamation even
though they do not restore a deleted row for new transactions.
