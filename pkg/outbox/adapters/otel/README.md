# Outbox OpenTelemetry adapter

`outboxotel` adds optional OpenTelemetry spans and metrics to
[`github.com/faustbrian/golib/pkg/outbox`](../..). The core outbox module stays
independent of OpenTelemetry, and exporter lifecycle remains caller-owned.

## Quick start

```go
instrumentation, err := outboxotel.New(telemetryRuntime)
if err != nil {
    return err
}

publisher, err := instrumentation.WrapPublisher(kafkaPublisher)
if err != nil {
    return err
}

relay, err := outboxrelay.New(store, publisher, outboxrelay.Config{
    Observer: instrumentation,
})
```

`telemetryRuntime` implements `outboxotel.Runtime` with standard
OpenTelemetry tracer, meter, and text-map propagation providers. Use
`Inject` while creating an envelope when publication spans should continue an
ambient producer trace. Register the same `Telemetry` as the relay or store
observer to emit operation metrics. Call `RecordBacklog` after collecting
`outbox.BacklogStats` when backlog gauges are required.

## Instruments and attributes

The instrumentation scope name is
`github.com/faustbrian/golib/pkg/outbox`; its schema version is the exported
`InstrumentationVersion` constant.

| Signal | Name | Unit | Attributes |
| --- | --- | --- | --- |
| producer span | `outbox.publish` | — | `outbox.operation`, `outbox.outcome`, `outbox.retry.state` |
| counter | `outbox.operations` | `{operation}` | `outbox.operation`, `outbox.outcome`, `outbox.retry.state` |
| histogram | `outbox.operation.duration` | seconds | `outbox.operation`, `outbox.outcome`, `outbox.retry.state` |
| gauge | `outbox.backlog.depth` | `{message}` | `outbox.state` |
| gauge | `outbox.backlog.oldest_pending_age` | seconds | none |

Operations are restricted to the ten values declared by `outbox.Operation` or
`unknown`. Outcomes are `success`, `failure`, or `unknown`. Retry state is
bucketed as `none`, `first`, `repeated` (attempts 2–5), or `many` (attempt 6 or
later). Backlog state is exactly `pending`, `leased`, or `dead`. These fixed
sets bound metric cardinality; numeric counts, durations, depths, and ages are
measurements rather than dimensions.

## Privacy and destination policy

The adapter never records envelope payload, metadata, topic or queue identity,
envelope or event ID, idempotency or ordering keys, SQL, error text, panic
values, or credentials. Failure spans use a fixed status description.

Destination labels are denied entirely. There is no destination-label option,
so no caller-controlled identity can become an attribute. Trace propagation is
also allowlisted: only the exact lowercase `traceparent` key is passed to or
copied from the configured propagator. Caller-controlled `tracestate` is not
forwarded because its arbitrary vendor value could carry sensitive data. All
other envelope metadata remains outside the telemetry provider.

## Publication semantics and failure isolation

The wrapper invokes the downstream publisher exactly once with the unchanged
envelope. It preserves the exact returned error or panic value. Extracted trace
identity may be attached to the downstream context, while the caller's
cancellation and deadline remain authoritative. If the downstream publisher
implements `Health(context.Context) error`, the wrapper preserves that optional
relay readiness contract unchanged.

Provider panics during propagation, span start, status updates, span completion,
or metric recording are contained. Sampling decisions, canceled contexts, and
SDK shutdown therefore cannot turn publication success into failure or failure
into success. Telemetry calls are synchronous at the OpenTelemetry API boundary;
the API accepts contexts but does not guarantee that every synchronous provider
method honors cancellation. Treat providers as trusted cooperative process
dependencies and configure bounded SDK queues and exporter timeouts because the
adapter cannot preempt an in-process call that blocks indefinitely.

The adapter starts no goroutines, owns no exporter, and never flushes or shuts
down a provider. The application must flush and shut down its providers after
the relay has stopped.

## Adoption and API

1. Construct the application-owned OpenTelemetry SDK and exporters.
2. Call `New` once and retain the returned concurrency-safe `Telemetry`.
3. Wrap the publisher before passing it to the relay.
4. Register `Telemetry` as `relay.Config.Observer` and any compatible store
   observer.
5. Optionally call `Inject` before persisting an envelope and
   `RecordBacklog` after a bounded backlog query.
6. Stop the relay before flushing and shutting down the SDK.

The public API consists of `Runtime`, `Publisher`, `Telemetry`, `New`,
`Inject`, `Observe`, `RecordBacklog`, `WrapPublisher`, the two dependency
errors, `ErrInstrumentCreation`, and `InstrumentationVersion`. Constructors
validate required providers and synchronously create instruments.

## Compatibility and migration

This independently versioned pre-v1 module supports the Go and OpenTelemetry
versions pinned in [`go.mod`](go.mod). The wrapped `Publisher` matches the
outbox relay contract, and the optional health method is retained when present.
Instrumentation names, attribute sets, and retry buckets are versioned schema;
changes are documented in [`CHANGELOG.md`](CHANGELOG.md).

Upgrading the core outbox dependency requires reviewing every declared
`outbox.Operation` and `outbox.Outcome` against the adapter's explicit mapping
test. Newly introduced values remain `unknown` until they are deliberately
mapped and the instrumentation schema version is updated when the exported
convention changes.

Earlier builds exported raw topic, envelope ID, and attempt count on publish
spans. Remove dashboards or alerts that depend on those forbidden attributes.
Replace attempt-number grouping with `outbox.retry.state`; destination-specific
telemetry belongs in an application-owned, explicitly reviewed allowlist layer.

## Security notes

Treat OpenTelemetry providers and exporters as trusted process dependencies.
Use bounded SDK batch queues, redact resource attributes independently, and do
not add envelope values to exporter configuration. Propagation does not provide
authentication or integrity; validate incoming trace context at the system
boundary and never use it for authorization.

## FAQ

### Does instrumentation change delivery guarantees?

No. Publication remains synchronous and at least once. Settlement, retry,
dead-letter, and deduplication behavior remain owned by the outbox relay and
consumer.

### Can I label metrics by topic or message ID?

No. Those values are intentionally unavailable because they create privacy and
cardinality risk.

### Does the adapter start an exporter or background worker?

No. Provider construction, flushing, shutdown, and exporter lifecycle belong
to the application.

### What happens after SDK shutdown?

Standard SDK providers become inert. The wrapper still calls the publisher and
returns its exact result; defensive containment also handles provider panics.

## Verification

From the repository root, run:

```sh
make check MODULES=pkg/outbox/adapters/otel
```

The module contract includes tests, race detection, exact statement coverage,
fuzzing, mutation, security, API, documentation, and benchmark gates.
