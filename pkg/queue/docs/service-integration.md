# Service integration

`queueservice` connects concrete queue resources to
`github.com/faustbrian/golib/pkg/service` without hiding backend APIs or moving
retry, ordering, acknowledgement, dead-letter, or business-handler policy out
of `queue`.

## Producer ownership

`NewProducer` accepts a concrete producer, a publish callback, and an optional
shutdown callback. Omitting shutdown keeps the producer shared. Providing it
transfers close ownership to the adapter.

The producer component has no implicit startup network check. Construct and
validate the concrete client before building the plan, or perform a bounded
check in a separately declared component. During stop, the adapter:

1. rejects new `Publish` calls;
2. waits for calls already accepted by the adapter;
3. invokes the owned shutdown callback only after those calls return.

The service stop context bounds the drain and shutdown callback. A canceled
drain does not close the transport underneath an active publish; a later stop
call may resume cleanup. Publish and shutdown retry policy belongs to the
concrete client and callbacks.

`Publish` requires correlation values in its context. An application beginning
independent work must call its `correlation.Factory.Start` and attach the
result with `correlation.WithValues`. The adapter uses
`correlation/queue.Send` to create the child message hop and stores its bounded
carrier in cloned `job.Metadata`. The concrete publish callback receives that
child correlation in its context, so transport logs describe the message hop
rather than its parent request.

Set `TracePropagator` to an explicitly constructed OpenTelemetry
`propagation.TextMapPropagator` to inject the publish context into the separate
bounded `job.Metadata.TraceContext` carrier. Nil disables trace propagation.
The adapter never consults OpenTelemetry global state and replaces stale
caller-supplied trace carrier fields with the context selected for this
publish. The propagator owns W3C parsing and baggage policy; use the bounded
owned telemetry propagation policy when accepting baggage.

## Worker ownership

`NewWorker` accepts and exposes an exact `*queue.Queue`. Its component calls
`Start` and transfers release ownership to the service plan. On stop,
`Queue.ReleaseContext` withdraws queue admission, lets an accepted publish
return, stops new delivery requests, joins active handlers, and only then
releases the concrete worker.

Backend request and transport operations retain their own configured timeout
bounds. The service stop context bounds coordinator drain and join work.
`ReleaseContext` returns a concrete worker shutdown failure without formatting
its potentially sensitive text.

Construct the backend handler with `NewHandler` before constructing its worker.
The wrapper uses `correlation/queue.Receive` for every delivery attempt, places
the resulting values in the handler context, and always creates a new request
ID. Set `TrustedMetadata` only when the immediate queue boundary is
authenticated; the default starts a new workflow and ignores inbound identity.
When `TracePropagator` is configured, the wrapper separately validates and
extracts the bounded trace carrier before application code. Correlation trust
does not imply baggage trust; that policy remains explicit in the propagator.

The worker has no generic readiness opinion because backend capabilities and
role admission policy differ. Declare a separate bounded readiness check only
when the concrete backend must be available before accepting new work.
`Queue.Start` has no error result, so all fallible backend construction and
validation must finish before the service plan starts. If a later component
fails, normal reverse-order rollback calls the worker stop path safely.

Adapter construction failures use `ErrInvalidOptions`. Correlation codec and
job metadata validation preserve their owning package classifications.
Concrete publish and shutdown failures pass through unchanged for
`errors.Is`/`errors.As`; applications must render them through their safe error
policy. Retry, acknowledgement, and shutdown transport retry remain owned by
the concrete queue implementation.

## Component order

When a producer shares the worker's concrete queue, declare the worker
component first and the producer component second:

```text
start: worker -> producer
stop:  producer drain -> worker drain and release
```

This keeps the producer available only while the worker-owned queue transport
is active. Independent producer and worker transports may instead use separate
plans or facilities with their own ownership.

Repeated stop calls are safe. Shared producer resources are never closed by
the adapter, and an owned producer shutdown callback runs once. Worker
ownership always transfers to the service component; the same `*queue.Queue`
must not be released independently or shared across competing lifecycle plans.
