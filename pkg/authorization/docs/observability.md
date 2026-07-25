# Observability and audit

`authorization.NewInstrumented` decorates any authorizer without changing its
decision or error. Instrumentation panics and nil derived contexts are isolated
from authorization behavior. Events contain bounded decision metadata only:
outcome, reason, revision, bounded matched policy IDs, trace counts, duration,
and failure state. They never contain subjects, tenants, resources, attributes,
or policy documents.

## Structured audit events

`authlog` accepts the standard `*slog.Logger` returned by `log`:

```go
audit, err := authlog.New(logger, slog.LevelInfo)
if err != nil {
    return err
}
authorizer, err := authorization.NewInstrumented(engine, audit,
    authorization.InstrumentationConfig{MaxPolicyIDs: 100})
```

One `authorization decision` record is emitted per call. Matched policy IDs are
bounded and explicitly marked when truncated. Applications should apply their
normal log access controls because policy identifiers may describe internal
business rules.

## Metrics and traces

`authotel` accepts standard OpenTelemetry providers, including providers owned
by a `telemetry` runtime:

```go
instrumenter, err := authotel.New(authotel.Config{
    TracerProvider: telemetryRuntime.TracerProvider(),
    MeterProvider:  telemetryRuntime.MeterProvider(),
})
```

Metrics use only the closed results `allow`, `deny`, `not-applicable`, and
`error`. Subject IDs, policy IDs, reasons, tenants, actions, resources, and
revisions are not metric labels. Spans include bounded counts, reason, and
revision for diagnosis but do not include request or attribute values.
