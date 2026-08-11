# outbox queue adapter

`outboxqueue` is the canonical synchronous adapter from `outbox.Envelope` to the
first-party `queue` contract. It maps one persisted envelope to one owned JSON
task and returns only after the queue reports its enqueue result. It owns no
worker, retry loop, dead-letter policy, scheduler, transaction, or queue
lifecycle.

## Quick start

```go
publisher, err := outboxqueue.New(queue)
if err != nil {
    return err
}

worker, err := relay.New(store, publisher, relay.Config{
    Owner:         "outbox-relay-1",
    ClassifyError: outboxqueue.ClassifyError,
})
```

The supplied queue must implement the synchronous `Queue` interface. A
`*queue.Queue`, a Valkey Streams producer, and compatible caller-owned
producers satisfy that interface. The caller constructs, starts, drains, and
closes those resources.

## Task mapping

The encoded task has a fixed field order and deterministic map encoding.
`Content` is a byte slice and therefore uses standard JSON base64 encoding.

| Task field | Envelope source | Rule |
|---|---|---|
| `task_id` | `ID` | Required and unchanged across relay attempts. |
| `idempotency_key` | `IdempotencyKey`, then `ID` | Always non-empty and stable for consumer deduplication. |
| `ordering_key` | `OrderingKey` | Preserved when present; it does not create a backend ordering guarantee. |
| `content` | `Payload` | Copied before enqueue. |
| `content_type` | `Metadata["es.content_type"]` | Defaults to `application/json`. |
| `event_name` | `Metadata["es.event_name"]`, then `Topic` | Required. |
| `schema_version` | `PayloadVersion` | Must be non-zero. |
| `metadata` | `Metadata` | Copied exactly; reserved event metadata remains present. |

Relay attempt counters and timestamps are not task identity and are excluded,
so a retry produces the same bytes. Queue operational metadata also receives
the task ID, event name, content type, and decimal schema version. The adapter
does not set any queue retry, timeout, or scheduling option.

## Bounds and unsupported values

`DefaultLimits` bounds identities, content, metadata, and the final encoded
task. `WithLimits` replaces all bounds. Empty task/event identity, a zero
schema version, invalid UTF-8 identity or metadata, blank metadata keys, and
oversized values are rejected before the queue is called. Arbitrary content
bytes, including non-JSON payloads, are supported because content type and
schema are explicit.

The defaults are 255 bytes per identity, 1 MiB of content, 64 metadata entries,
16 KiB of metadata, and 2 MiB for the owned task. The adapter additionally
encodes the first-party `job.Message` and rejects it above the queue contract's
1 MiB limit. That final envelope check can make the effective content limit
smaller because JSON base64 and job metadata add bytes.

## Acceptance and at-least-once behavior

`OutcomeOf` keeps queue acceptance separate from retry disposition:

| Acceptance | Meaning |
|---|---|
| `AcceptanceAccepted` | The synchronous queue call returned success. |
| `AcceptanceRejected` | The task is known not to have been accepted. |
| `AcceptanceUnknown` | The backend may have accepted the task before losing its response. |

Failed outcomes are retryable, permanent, or canceled. First-party capacity
and closed-queue errors are known retryable rejections. Valid queue management
failures preserve their declared classification. An unclassified backend
error, infrastructure failure, or recovered queue panic has unknown
acceptance. `ClassifyError` maps permanent publication errors to the relay's
permanent class and keeps every other failure transient.

An accepted enqueue can still be delivered more than once. If the process
crashes after enqueue and before `MarkDelivered`, the outbox row remains
eligible and the same task is enqueued again. Consumers **must** durably
deduplicate `idempotency_key` (or `task_id`) before applying side effects. The
adapter neither claims exactly-once delivery nor stores consumer deduplication
state.

## Backend differences and scheduling

Redis Streams and Valkey Streams preserve the task bytes through their durable
append paths, but their retention, capacity, consumer-group, failover, and
ordering settings remain backend policy.

| Setting | Redis Streams default | Valkey Streams default | Adapter integration proof |
|---|---:|---:|---:|
| Source stream capacity | Unbounded (`0`) | 10,000 records | Both set to 16 records. |
| Command timeout | 5 seconds | 5 seconds | Backend-owned. |
| Request timeout | 6 seconds | 6 seconds | 5 seconds for the proof. |
| Blocking read | 60 seconds | 1 second | Backend-owned. |
| Reclaim minimum idle | 30 seconds | 30 seconds | Backend-owned. |

The integration proof uses Redis 8.6.4, Valkey 9.1.0, and PostgreSQL 18.4. It
publishes, closes the producer, reconstructs it, republishes the same outbox
identity, appends a later task, and then observes the stable duplicate followed
by the later task through both stream backends. It abandons an
acknowledgement-required delivery and verifies reclaim by another consumer
with unchanged bytes. Two relay instances also compete for one PostgreSQL
outbox while the backend receives each claimed task once.

A subprocess relay exits immediately before enqueue, after accepted enqueue
and before `MarkDelivered`, and immediately after the durable mark. Recovery
from the persisted PostgreSQL state leaves one, two, and one total backend
tasks for those respective windows, with stable bytes in the duplicate window.
A closed producer reports a known retryable rejection. Response loss,
disconnects, cancellation, and panics are injected at the adapter's queue seam
because the generic queue contract exposes only a synchronous error, not a
backend fault-injection control.

The proof does not restart Redis or Valkey servers and does not claim a
particular fsync, replication, or failover policy. Operators must configure
RDB/AOF persistence, replication, eviction, retention, and recovery objectives
for their deployment. The configured backend command timeout bounds the
synchronous append, but a caller cancellation cannot interrupt the generic
queue interface after that call begins.

Ordering keys are data for a compatible backend or consumer; the generic queue
interface has no universal partition or ordering operation. Within the single
configured Redis or Valkey stream, the proof observes append order. Multiple
streams, partitions, producers, failover, and consumer parallelism can change
observable order. Other queue backends can provide weaker or different
durability and redelivery semantics.

`Envelope.AvailableAt` is enforced by the outbox store before a relay claim.
The adapter does not translate it into a delayed queue job. Once `Publish` is
called, enqueue happens immediately according to the selected backend.

## API and adoption

- `New(queue, options...)` validates a caller-owned synchronous queue.
- `WithLimits(limits)` configures publication bounds.
- `Task` is the versioned consumer payload.
- `OutcomeOf(err)` exposes acceptance and retry disposition.
- `ClassifyError(err)` plugs into `relay.Config.ClassifyError`.

Adopters should freeze their accepted task schema, route by `event_name`,
select a decoder by `schema_version`, authenticate any metadata used for
authorization, and commit consumer deduplication with the business side effect.
Benchmark mapping separately from backend latency because `Publish` includes
the synchronous backend call.

## Compatibility and migration

This pre-v1 release replaces the previous queued `Envelope.CanonicalJSON`
bytes with `Task`. Consumers must switch from envelope fields such as `id`,
`topic`, and `payload` to `task_id`, `event_name`, and `content`; `content` is
still JSON base64 for raw bytes. Producers should deploy compatible consumers
before enabling the new adapter. Old and new payloads need separate decoding
during a rolling migration; the adapter does not add a dual-write mode.

The module follows the repository Go compatibility policy and publishes under
directory-prefixed semantic-version tags. Public API changes are checked
against `api/baseline.txt`.

## Security notes

Validation occurs before allocation into backend-owned job state. Returned
publication errors retain causes for `errors.Is` and `errors.As`, while their
own text omits payload, metadata, backend diagnostics, endpoints, credentials,
and panic values. Queue errors may still contain backend-specific diagnostics
when explicitly unwrapped by trusted code; do not unwrap them into untrusted
logs or responses. Do not place credentials or authorization secrets in
envelope payloads or metadata.

## FAQ

### Does a nil `Publish` error mean exactly once?

No. It means only that the synchronous queue call reported acceptance.

### Should the outbox retry an unknown-acceptance failure?

Yes, when at-least-once delivery is required. The stable idempotency identity
makes the resulting duplicate detectable by the consumer.

### Does the adapter preserve global order?

No. It preserves the ordering key as data. Backend configuration and consumer
parallelism determine observable order.

### Where are worker retries and dead letters configured?

On the queue worker or backend. The adapter deliberately supplies no worker
retry, dead-letter, timeout, or scheduling policy.

### How is a non-JSON event published?

Put the raw bytes in `Envelope.Payload` and set `es.content_type` plus the
appropriate payload schema version.

See [CHANGELOG.md](CHANGELOG.md) for release notes and [LICENSE](LICENSE) for
the MIT license.
