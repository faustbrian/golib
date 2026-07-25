# Local development

## No Collector

Disable export when telemetry is irrelevant to the task:

```go
config := telemetry.DefaultConfig("orders", "dev")
config.RegisterGlobal = false
config.Traces.Enabled = false
config.Metrics.Enabled = false
runtime, err := telemetry.Init(ctx, config)
```

The runtime still returns standard no-op providers and supports the same
shutdown path.

## Deterministic tests

Use `testtelemetry` for assertions without ports, timers, or network:

```go
harness := testtelemetry.New()
defer harness.Shutdown(context.Background())

_, span := harness.TracerProvider().Tracer("test").Start(ctx, "orders.list")
span.End()
spans := harness.Spans()
```

IDs repeat from a known sequence in every harness. `Metrics` performs a manual
collection, and `ResetSpans` isolates test phases.

## Local Collector

Save the configuration from [Collector deployment](collector.md), run the
Collector container with ports 4317 and 4318 exposed, and retain the default
`localhost:4317` gRPC endpoint. For HTTP:

```go
config.Traces.Exporter.Protocol = telemetry.ProtocolHTTPProtobuf
config.Traces.Exporter.Endpoint = "localhost:4318"
config.Traces.Exporter.URLPath = "/v1/traces"
config.Metrics.Exporter.Protocol = telemetry.ProtocolHTTPProtobuf
config.Metrics.Exporter.Endpoint = "localhost:4318"
config.Metrics.Exporter.URLPath = "/v1/metrics"
```

Use the debug exporter only with non-sensitive development traffic. The same
privacy rules apply locally because copied fixtures and URLs often contain real
identifiers.
