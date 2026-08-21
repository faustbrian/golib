# Event sourcing queue adapter

`eventqueue` maps complete event-sourcing deliveries to the first-party
`github.com/faustbrian/golib/pkg/queue` contract. The event-sourcing core does
not import queue, and this adapter owns no worker, broker connection, retry
clock, dead-letter store, or business-idempotency state.

The adapter provides a bounded canonical payload, synchronous publication in
input order, explicit enqueue ambiguity, and queue-owned settlement. It does
not claim exactly-once delivery or broker-neutral durability and ordering.

## Quick start

```go
codec, err := eventqueue.NewCodec(eventqueue.CodecConfig{})
if err != nil {
	return err
}

handler, err := eventqueue.NewTaskHandler(codec, applyDelivery)
if err != nil {
	return err
}

workerQueue := queue.NewPool(
	1,
	queue.WithFn(handler.Handle),
	queue.WithLogger(queue.NewEmptyLogger()),
)
workerQueue.Start()
defer workerQueue.Release()

dispatcher, err := eventqueue.NewDispatcher(eventqueue.DispatcherConfig{
	Queue: workerQueue,
	Codec: codec,
	Job: job.AllowOption{
		Metadata: &job.Metadata{
			RetryPolicy: "projection-v1",
			HandlerType: "account-projector",
		},
	},
})
if err != nil {
	return err
}

return dispatcher.Dispatch(ctx, deliveries)
```

The application constructs, starts, stops, and observes `workerQueue`. The
consumer must durably deduplicate `message_id` before non-idempotent side
effects when duplicates matter.

## API

### Codec

`NewCodec(CodecConfig)` constructs an immutable concurrency-safe codec. A zero
`MaxEnvelopeBytes` selects `queue/job.DefaultMaxMessageBytes`; a smaller
positive value may be used. `Encode` and `Decode` copy payload and metadata
across ownership boundaries.

`Decode` accepts only the exact canonical encoding emitted by this version.
Malformed JSON, unknown or duplicate fields, reordered fields, non-canonical
escapes, invalid values, unsupported format or delivery mode, trailing data,
and oversized input fail before application handling.

### Dispatcher

`NewDispatcher` publishes live deliveries. `NewReplayDispatcher` is the
separately named replay opt-in. `Dispatch` checks cancellation before each
queue call, publishes synchronously in input order, and stops on the first
failure. Construction rejects invalid or unencodable queue job policy,
including non-finite retry factors even when retries are disabled.

`DispatchError` reports definite progress:

- `Enqueued` counts queue calls that returned success;
- `Attempted`, `Failed`, and `Total` describe batch progress;
- `AcceptanceNotAttempted` means the stopping delivery never reached `Queue`;
- `AcceptanceUnknown` means `Queue` returned an error or panicked and may have
  accepted the delivery before that outcome became observable.

Retrying an `AcceptanceUnknown` delivery can create a duplicate. The adapter
does not convert ambiguity into a false non-delivery claim.

The first-party producer interface has no context parameter. Cancellation can
stop the next enqueue, but cannot interrupt a `Queue` call already in progress.
Applications must select and configure a backend whose enqueue operation has
an appropriate bounded implementation.

### Task handler

`NewTaskHandler` decodes live tasks and synchronously invokes an
`eventsourcing.ConsumerFunc`. `NewReplayTaskHandler` is the separately named
replay opt-in. Decode errors, denied replay, consumer errors, cancellation,
and contained panics are returned to the owning queue.

The handler never calls `Ack`, `Nack`, or `NackFailure`. A nil result permits
the queue to acknowledge only after decode and consumer completion. Any error
leaves retry, redelivery, or terminal settlement to the selected queue policy.

## Wire format

The UTF-8 JSON object uses a fixed field order and the format identifier
`golib.event-sourcing.queue.v1`.

| Field | Meaning |
| --- | --- |
| `format` | Exact wire-format version. |
| `delivery_mode` | `live` or explicitly enabled `replay`. |
| `message_id` | Stable event-delivery and consumer-idempotency identity. |
| `aggregate_type`, `aggregate_id` | Stable stream identity and ordering key material. |
| `stream_version` | Version within the aggregate stream. |
| `event_name`, `event_schema_version` | Event identity and payload schema. |
| `content_type`, `payload` | Payload media type and canonical base64 bytes. |
| `metadata` | Sorted copied event metadata. |
| `recorded_at` | Canonical UTC RFC 3339 timestamp at microsecond precision. |
| `correlation_id`, `causation_id` | Optional message relationships. |
| `tenant`, `partition` | Optional routing and isolation identities. |
| `global_position` | Optional global event-store position. |

The stable logical ordering identifier is the aggregate stream
(`aggregate_type`, `aggregate_id`); `partition` is preserved when the event
store supplied it. These identifiers do not configure a broker partition or
create a cross-worker ordering guarantee.

Before enqueue, the dispatcher also verifies that the complete first-party
`job.Message` wrapper fits `job.DefaultMaxMessageBytes`. An inner wire envelope
can fit the codec limit while its JSON/base64 queue wrapper does not; that case
fails as `ErrEnvelopeTooLarge` with `AcceptanceNotAttempted`.

## Queue metadata, retries, and dead letters

For each delivery the dispatcher derives queue operational metadata used by
first-party failure and dead-letter records:

- `OriginalID` from `message_id`;
- `PayloadSchemaVersion`, `ContentType`, and `JobType` from the event;
- `TenantID` from the optional tenant.

Those identity fields are adapter-owned and replace conflicting static values
in `DispatcherConfig.Job.Metadata`. Caller-owned `RetryPolicy`, `HandlerType`,
tags, trace identity, correlation carriers, producer version, retry count,
backoff values, jitter, and timeout are defensively copied and passed to the
queue. The queue interprets them; this adapter does not schedule a retry or
dead-letter a task.

Retry attempts reuse the same canonical event envelope. The adapter never
wraps a failed task inside another event envelope and therefore does not create
recursive retry loops. Backend-specific dead-letter envelopes and attempt
counters remain queue-owned. Business idempotency remains application-owned.

## Guarantees and limitations

- Publication is synchronous and stops on the first observable failure.
- A successful queue call is not proof of durable persistence or consumer
  completion unless the selected backend documents that behavior.
- At-least-once handling allows duplicates after worker crashes, settlement
  failures, visibility/lease expiry, and ambiguous enqueue outcomes.
- Input order is the order of queue calls only. Durable order depends on the
  backend, partitioning, consumer concurrency, retries, and dead-letter replay.
- The adapter starts no goroutines and owns no queue lifecycle.
- Replay is denied by default to avoid accidental external side effects.

## Adoption

1. Choose a queue backend and verify its durability, capacity, retry,
   settlement, and dead-letter behavior for the deployment topology.
2. Create one codec limit that fits both the event payload policy and backend
   message limit.
3. Configure queue retry/dead-letter policy outside the adapter and use
   `DispatcherConfig.Job.Metadata` only for bounded operational identity.
4. Make the consumer transactionally or durably idempotent by `message_id`.
5. Isolate replay dispatchers and replay handlers from normal side effects.
6. Operate worker startup, drain, shutdown, metrics, and dead-letter recovery in
   the application.

## Compatibility and migration

`golib.event-sourcing.queue.v1` is exact: readers reject changed field order,
unknown fields, and unsupported versions. Deploy readers that understand a new
format before writers emit it. Do not rewrite queued v1 bytes during a rolling
deployment.

Version 1 is the first supported wire version, so there is no prior-version
reader in this release. The frozen complete and minimal golden payloads cover
both delivery modes and every present or absent optional field. Inputs labeled
v0, v2, or any other version are rejected rather than guessed.

Migrating from an application-defined queue payload requires draining or
retaining its old reader while new publishers emit v1. Preserve the old
idempotency record until every old and v1 duplicate window has closed. A future
breaking wire change will use a new format identifier and migration guidance.

| Component | Compatibility boundary |
| --- | --- |
| Go | Version declared by this module's `go.mod`. |
| Event sourcing | Exact sibling module dependency in `go.mod`. |
| Queue | Exact sibling module dependency and `QueuedMessage`/`TaskMessage` seams. |
| Durable backend | Backend-specific; Valkey Streams is exercised by integration. |

The adapter's hardening support matrix is deliberately narrower than the set of
packages that happen to implement the queue interface:

| Backend | Supported evidence | Ordering claim |
| --- | --- | --- |
| In-memory Ring | Complete encode, enqueue, handle, failure, and settlement contract. | One configured worker observes synchronous enqueue order; there is no durability claim. |
| Valkey Streams 9.1.0 | Digest-pinned persistence, restart, process-death reclaim, dead-letter failure recovery, shutdown, and ordering-identity integration. | One stream, one consumer group, and one consumer preserve stream entry order without retries. |

Redis Pub/Sub, Redis Streams, Core NATS, NSQ, and RabbitMQ may satisfy the
public `Queue` seam, but this module does not claim backend-interoperability
support for them until equivalent adapter-level hardening evidence exists.
Their queue-package tests are not evidence for this event-sourcing mapping.

## Security

Codec and handler errors are redacted and never include hostile input, event
identity, payload, metadata, or backend diagnostics. Message size, metadata,
identity, and timestamp limits are enforced before application handling.
Applications remain responsible for payload authorization, tenant isolation,
encryption, secret redaction in consumer errors, and dead-letter access.

All common `fmt` representations of `EnvelopeError`, `DispatchError`, and
`HandlerError`, including `%#v`, emit only their stable category. `errors.Is`
and `errors.As` still expose wrapped causes for deliberate programmatic
classification; applications must not print those separately when they can
contain consumer, broker, or credential diagnostics.

## FAQ

### Does this provide exactly-once delivery?

No. It provides stable identity for durable consumer deduplication and retains
at-least-once duplicate windows explicitly.

### Does input order mean durable aggregate order?

No. Input order is only synchronous call order. Configure backend partitioning
and consumer concurrency using the preserved stream identity and backend
capabilities.

### Should a queue error be retried?

It may be, but `AcceptanceUnknown` means retrying can duplicate an accepted
message. Apply the backend's error policy and deduplicate by `message_id`.

### Who owns retry and dead-letter timing?

The queue backend and application. This adapter only preserves bounded job
policy and operational metadata across the mapping boundary.

### Can replay use the normal dispatcher or handler?

No. Use the separately named replay constructors and isolate their side
effects.

## Development

Run `make check` for the complete module contract. Run `make integration` with
a Docker-compatible runtime for digest-pinned durable Valkey Streams evidence.
See [hardening evidence](docs/hardening.md) for duplicate windows, fault and
fuzz coverage, backend claim boundaries, and benchmark methodology.

## Ecosystem

Use the [Golib documentation portal](https://github.com/faustbrian/golib/blob/main/docs/index.md)
to choose companion packages, supported stacks, recipes, and operations guidance.
