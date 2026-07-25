# Collector deployment

`telemetry` emits standard OTLP and expects a Collector to own routing,
tail-sampling, enrichment, transformation, buffering, and vendor export.

## Minimal local configuration

```yaml
receivers:
  otlp:
    protocols:
      grpc: {endpoint: 0.0.0.0:4317}
      http: {endpoint: 0.0.0.0:4318}

processors:
  memory_limiter:
    check_interval: 1s
    limit_mib: 256
  batch: {}

exporters:
  debug:
    verbosity: basic

extensions:
  health_check: {}

service:
  extensions: [health_check]
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [debug]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [debug]
```

Replace `debug` with the backend exporter in Collector configuration, not a
vendor SDK in application code. Apply authentication, retry queues, redaction,
and tenant routing at the Collector boundary.

## Application endpoints

gRPC uses `host:port` and defaults to port 4317. HTTP/protobuf uses `host:port`
and defaults to port 4318, with `/v1/traces` and `/v1/metrics` unless custom
paths are configured. Endpoint strings do not include a scheme when
`TLS.Insecure` selects plaintext.

## Production checks

- Require Collector health checks and resource limits.
- Monitor refused spans/metrics, queue capacity, exporter failures, and memory.
- Bound Collector sending queues and persistent storage.
- Test Collector restart, backend throttling, malformed responses, and network
  loss before rollout.
- Keep application and Collector retry horizons finite to avoid retry storms.

The in-process interoperability suite covers both transports, signals,
compression, headers, retries, timeouts, and malformed responses. Run
`make integration` after changing any Collector-facing setting.
