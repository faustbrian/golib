# Laravel Horizon failed-job migration

`queue` provides Horizon-like failed-job operations without reproducing
Laravel internals or promising cross-runtime wire compatibility. Migrate the
operator workflow and semantics, not Horizon's database schema or serialized
PHP jobs.

## Concept mapping

| Horizon concept | `queue` contract | Intentional difference |
| --- | --- | --- |
| Failed job | `management.JobRecord` failure or dead letter | A failed attempt and a terminal record are distinct kinds |
| Retry one | `CommandRetry` for a named record | Enqueue succeeds before source deletion; partial outcomes are explicit |
| Retry all or batch | `CommandBulkRetry` with bounded selection | No unbounded all-record scan |
| Retry to another queue | `CommandReplay` with allowlisted destination | Requires an explicit idempotency policy |
| Forget | `CommandDelete` | Backend result can be not found or unknown |
| Flush failed jobs | Confirmed record `CommandPurge` | Queue purge is separate and unsupported by stream controllers |
| Trim recent/failed | `WithRecordRetention` or broker policy | Package count retention is opt-in; time/byte support is not implied |
| Tags | Bounded `job.Metadata.Tags` | At most 32 bounded key/value pairs; never metric labels by default |
| Metrics | Status measurements and observer events | Unsupported gauges remain unsupported, not fabricated zeroes |

## Migration procedure

1. Choose a durable backend. Ring, Redis Pub/Sub, and Core NATS cannot replace
   Horizon's persistent failed-job store.
2. Assign a stable original job ID and idempotency key in `job.Metadata`; keep
   payload schema, handler type, tenant, trace, and producer version bounded.
3. Configure Redis Streams or Valkey Streams failure and dead-letter streams,
   terminal attempt policy, reclaim policy, management identity, and deliberate
   count retention. For NSQ or RabbitMQ, configure broker-owned terminal
   retention and accept that package list/mutation operations are unsupported.
4. Deploy readers with hidden payload visibility and negotiate protocol and
   capabilities across all worker versions before enabling mutations.
5. Recreate Horizon roles in `queue-control-plane`. Authorization, audit,
   destination policy, and privileged payload disclosure do not belong in the
   worker library.
6. Run canaries for retry, replay reject/replace, forget, purge, poison input,
   destination outage, and an ambiguous append/ack boundary.
7. Freeze Horizon retries, drain its active queues, retain its failed-job store
   for the required audit window, then enable bounded `queue` redrive.

## Payload conversion

Do not copy Horizon's serialized PHP payload into a Go handler and deserialize
it during listing. Produce a new bounded Go job envelope or enqueue a reference
to data in an authorized external store. Listing remains metadata-only and
payload-hidden. Revealed inspection is a privileged exception, not the normal
retry path.

## Operational differences to teach

Horizon's single failed-job row can hide the distinction between a retryable
attempt and completed terminal settlement. `queue` exposes both records and
the backend crash window. Duplicate terminal records and duplicate processing
are possible under at-least-once delivery. Operators must reconcile lineage and
idempotency rather than interpret a successful UI request as exactly-once
execution.

There is no universal `horizon:purge`, automatic time pruning, or portable
queue-depth metric. Every operation is capability-negotiated, every bulk
selection is bounded, and unsupported behavior fails explicitly.
