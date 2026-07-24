# Tracing, logging, and metrics

OpenTelemetry owns trace and span identifiers. `telemetry.Link` requires an
already valid `trace.SpanContext` and adds separately disclosed correlation
attributes; it cannot derive or replace trace state. W3C Trace Context and
Baggage remain optional application concerns.

The `log` package returns ordinary `slog.Attr` values and therefore composes
with `log`. Disclosure defaults to `[redacted]`; keyed hashes provide a
bounded linkable token and raw values require `ExposeDisclosure`.

`MetricAttributes` emits only three booleans. Raw, hashed, and deterministic
identifier values are prohibited as metric dimensions to keep cardinality
bounded and prevent business-data disclosure.
