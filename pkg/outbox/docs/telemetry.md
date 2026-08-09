# Telemetry integration

The optional `adapters/gotelemetry` module accepts the standard providers
exposed by `telemetry.Runtime`. Core and the `queue` adapter do not depend
on OpenTelemetry.

```go
runtime, err := telemetry.Init(ctx, telemetry.DefaultConfig())
if err != nil {
    return err
}
defer runtime.Shutdown(context.Background())

instrumentation, err := gotelemetry.New(runtime)
if err != nil {
    return err
}

metadata := instrumentation.Inject(ctx, applicationMetadata)
envelope, err := builder.Build(outbox.NewEnvelopeParams{
    Topic:    "orders.created",
    Payload:  payload,
    Metadata: metadata,
})
```

Injection copies the application map and writes the runtime's bounded W3C
propagation fields into the copy. Pass the result through `EnvelopeBuilder` so
the normal metadata size limit still applies.

At relay construction, wrap the concrete publisher and register the same
instrumentation as the observer:

```go
publisher, err := instrumentation.WrapPublisher(queuePublisher)
if err != nil {
    return err
}

worker, err := relay.New(store, publisher, relay.Config{
    Owner:    podName,
    Observer: instrumentation,
})
```

Pass the same observer through `postgres.StoreConfig.Observer` to record replay,
prune, and archive counts and latency.

The wrapper extracts only lowercase `traceparent` and `tracestate` metadata and
creates an `outbox.publish` producer span. Span attributes are limited to the
fixed operation, outcome, and retry-state dimensions. Message IDs, topics, raw
attempt counts, destinations, payloads, arbitrary metadata, publisher errors,
and panic values are never exported. Downstream errors and panics retain their
exact values.

The observer exports:

- `outbox.operations`, a counter by operation and outcome;
- `outbox.operation.duration`, a seconds histogram by operation and outcome.
- `outbox.backlog.depth`, a gauge by pending, leased, and dead state;
- `outbox.backlog.oldest_pending_age`, a seconds gauge.

Record a low-frequency backlog snapshot with an application-injected clock:

```go
stats, err := store.Backlog(ctx)
if err == nil {
    instrumentation.RecordBacklog(ctx, stats, clock())
}
```

Metric attributes intentionally exclude message ID and topic to bound
cardinality. A standard `*slog.Logger` can be passed directly through
`relay.Config.Logger`.

Exporter lifecycle remains application-owned. Use an OpenTelemetry batch span
processor with a bounded queue and export timeout, stop the relay before SDK
shutdown, and pass the pod's remaining termination budget to provider
shutdown. The adapter starts no goroutines and performs no provider flush or
shutdown of its own.
