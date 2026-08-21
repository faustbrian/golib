# Telemetry Collector Operations Evidence

Observed at `2026-08-13T10:13:51Z` on `darwin/arm64` with Go `1.26.5`,
Docker Engine `29.6.2`, and OpenTelemetry Collector Contrib `0.157.0` at
`sha256:f2f01157055a9b2aab9df7118e1f1c9abf345e99b23bc7a2bc791db374a7d0f6`.

## Executed Proof

- The current public telemetry service example was built with `GOWORK=off`,
  cold task-owned Go caches, and the standard URL form
  `OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:<port>`.
- A task-owned Collector exposed a health-checked OTLP/gRPC receiver, a
  64-MiB memory limiter, bounded batching, and a file exporter. The example
  started without the prior misleading endpoint parse errors.
- A sampled request exported a `health.show` server span. The Collector output
  contained only the bounded service, deployment, HTTP method, route, status,
  and OpenTelemetry SDK attributes expected by the instrumentation contract.
- The Collector was stopped while the service remained live. A sampled request
  completed with HTTP 200 in `1.120 ms`; after the Collector restarted on the
  same endpoint, the queued span was exported without restarting the service.
- A sampled request carried a credential-shaped authorization value and the
  same marker in its query string. The marker was absent from the complete
  exported payload.
- Graceful interrupt exited the service with status zero and flushed traces
  plus `http.server.request.count` and `http.server.request.duration` metrics.
  The final output contained all three sampled trace identities.
- The complete current `pkg/telemetry` canonical gate passed after the endpoint
  correction, including exact 100% coverage for every production package and
  259 of 259 viable mutants killed.

The Collector container, image, output, binaries, module downloads, Go caches,
and temporary request payloads were task-owned and removed after the evidence
was captured.

## Claim Boundary

This proves local real-Collector trace and metric interoperability, bounded
business-path behavior during a short Collector outage, recovery on the same
endpoint, graceful flush, and omission of the injected sensitive marker. It
does not prove production TLS or authentication, Better Stack export, the log
signal, Collector persistent queues, a prolonged backend outage, multi-replica
behavior, ECS, dashboards, alerts, SLOs, high-cardinality production traffic,
or an operator incident drill. The affected assurance scenarios remain
pending.
