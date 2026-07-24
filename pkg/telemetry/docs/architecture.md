# Architecture

## Boundaries

Applications own `Config` and call `Init`. The root runtime owns the resource,
SDK providers, batch processor, periodic reader, propagator, optional global
registration, flush, and shutdown. Callers receive standard OpenTelemetry
interfaces and can use normal OTel instrumentation alongside this package.

```text
application config
       |
       v
resource + propagation policy
       |
       +--> trace sampler --> batch processor --> OTLP exporter
       |
       +--> metric views --> periodic reader --> OTLP exporter
       |
       +--> optional process globals
```

The `otlp`, `trace`, `metric`, and `propagation` packages build standard SDK
components. Instrumentation packages depend on standard APIs, not the root
runtime, so they can use custom providers and cannot create cycles.

## Initialization

Configuration validation precedes construction. If a later component fails,
already-created exporters and providers are shut down and all cleanup failures
are joined. Global registration occurs last. Only one runtime may own globals;
independent runtimes remain available with `RegisterGlobal = false`.

## Shutdown

The first `Shutdown` call creates a child timeout bounded by both caller
context and `ShutdownTimeout`. Globals are restored only when they still point
to this runtime. Metrics and traces are flushed, providers are stopped, and
exporter failures captured across supported SDK versions are joined. Every
later call returns the same result.

## Failure isolation

Queues, retry elapsed time, per-export timeout, metric cardinality, propagation
bytes, baggage item count, and shutdown duration are finite. Instrumentation
does not block on an exporter; trace batching and metric readers isolate
application work from Collector availability.

## Logs

Logs are not part of the stable runtime dependency graph. An experimental log
module may be added later without changing stable trace and metric packages.
