# Event sourcing queue adapter

`goqueue` is the independently versioned adapter between event-sourcing
deliveries and compatible backends in `github.com/faustbrian/golib/pkg/queue`.
The event-sourcing core does not import queue.

The adapter maps complete persisted deliveries to canonical JSON, enqueues
them synchronously in input order, and decodes queue tasks for explicit
delivery consumers. Backend conformance work remains follow-up, so the
first-release adapter matrix remains partial.

The integration suite proves successful and failed event handling through the
repository queue and its in-memory worker. Durable backend guarantees remain
the responsibility of each selected queue backend and its own conformance
evidence.

## Dispatch and handling

`NewDispatcher` constructs the safe live-only publisher. It stops on the first
encoding or queue error and reports exact attempted, enqueued, failed, and
total counts through `DispatchError`. `NewReplayDispatcher` is the separately
named opt-in for replay publication.

`NewTaskHandler` decodes live tasks and calls an
`eventsourcing.ConsumerFunc`. `NewReplayTaskHandler` is the separately named
replay entry point. Both are synchronous, contain callback panics, and return
redacted errors. They never call `Ack`, `Nack`, or `NackFailure`; the owning
queue settles the task only after the handler result is known.

The compatible queue producer has no `context.Context` parameter. Dispatch
checks cancellation before every enqueue, but cancellation cannot interrupt a
backend `Queue` call already in progress. Queue acceptance does not mean a
consumer processed the event.

## Wire format

`Codec` emits `golib.event-sourcing.queue.v1`. It includes message and stream
identity, event name and schema version, content type, encoded payload,
metadata, recorded time, correlation and causation IDs, tenant and partition
values, optional global position, and live or replay mode.

The default one-mebibyte bound matches `queue/job.DefaultMaxMessageBytes`.
Applications may choose a smaller positive bound. A valid core message can
still exceed a backend's queue envelope limit because the envelope adds stable
identity fields and base64 encoding; encoding then fails explicitly with
`ErrEnvelopeTooLarge`.

Decoding accepts only the exact canonical encoding emitted by this version.
Unknown, duplicate, reordered, non-canonical, malformed, or oversized input
fails without partially constructing a delivery. The codec starts no
goroutines and is safe for concurrent use.

## Guarantees

The adapter does not implement retries, acknowledgement, rejection, or
settlement. Queue backend durability and delivery guarantees remain observable
backend-specific behavior. It does not claim exactly-once delivery.

## Development

Run `make check`.
