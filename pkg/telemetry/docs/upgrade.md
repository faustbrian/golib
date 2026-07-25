# Upgrade guide

## Before upgrading

1. Read `CHANGELOG.md` for changed defaults, metrics, attributes, and errors.
2. Run `go test`, `go test -race`, and protocol integration tests.
3. Compare benchmark and cardinality baselines.
4. Deploy through a canary Collector pipeline and watch exporter failures,
   queue pressure, process memory, and backend ingestion.

## OpenTelemetry dependencies

Upgrade all OTel API, SDK, and OTLP exporter modules together. Run the
compatibility script in a disposable checkout:

```sh
./scripts/test-otel-version.sh v1.44.0
```

The script modifies `go.mod`; do not run it over unrelated uncommitted module
changes. CI runs each matrix entry in an isolated checkout.

## Configuration changes

Treat endpoint, TLS, retry, timeout, sampling, metric views, cardinality,
propagation allow-lists, and instrumentation operation names as operational
contracts. Roll them out independently from application behavior where
possible.

## Rollback

Restore the previous module version and configuration together. OpenTelemetry
wire data remains OTLP, so no vendor SDK migration is needed. When metric views
or names change, keep dashboards compatible with both versions during the
rollout window.
