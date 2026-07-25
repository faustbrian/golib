# Traces

## Provider configuration

Tracing uses a standard SDK tracer provider and bounded batch processor.
`Runtime.TracerProvider` and `Runtime.Tracer` can be passed anywhere that
expects OpenTelemetry interfaces.

```go
config.Traces.Enabled = true
config.Traces.Batch.MaxQueueSize = 2048
config.Traces.Batch.MaxExportBatchSize = 512
config.Traces.Batch.BatchTimeout = 5 * time.Second
config.Traces.Batch.ExportTimeout = 10 * time.Second
```

Batch size must not exceed queue size. Every duration and queue value must be
positive. When the queue is full, prefer dropping telemetry over blocking
application work; monitor drops and fix Collector capacity rather than making
the application queue unbounded.

## Creating spans

```go
ctx, span := runtime.Tracer("orders").Start(ctx, "orders.create")
defer span.End()
```

Use fixed operation names. Put controlled enums and bounded state on spans.
Avoid payloads, credentials, raw URLs, SQL, customer identifiers, and raw
errors. The supplied instrumentation adapters enforce this by construction.

## Export failures

Export is asynchronous for normal spans. `ForceFlush` and `Shutdown` return
joined failures. They must receive deadlines and should be called during drain,
not on each request. Collector outages must not trigger application retries or
failed business operations.

## Parent behavior

The default sampler preserves remote and local parent decisions. A child of an
unsampled parent remains unsampled; a child of a sampled parent remains
sampled, even when the local root ratio differs. See [sampling](sampling.md).
