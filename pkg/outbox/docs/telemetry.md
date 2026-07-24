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

The wrapper extracts producer context and creates an `outbox.publish` producer
span. It adds message ID, topic, and attempts to traces, but never payload,
metadata, or publisher error text. Downstream errors are returned unchanged
and set only a generic error status on the span.

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
