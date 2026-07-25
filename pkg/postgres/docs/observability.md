# Observability and redaction

`Observation` contains only fixed operation, outcome, duration, classification,
SQLSTATE, and pool gauges. It has no field for SQL, arguments, DSNs, raw errors,
details, hints, arbitrary labels, or query names. Observer panics are recovered
so telemetry cannot change database behavior.

Transaction outcomes are `success`, `error`, `panic`, or `aborted`. The
`aborted` value identifies a callback that terminated its goroutine with
`runtime.Goexit`, including test helpers such as `testing.T.FailNow`.

`HasPoolStats` marks observations that carry an actual pool snapshot. Adapters
omit connection gauges for transaction and savepoint observations rather than
overwriting the last pool sample with zero values.

`NewSlogObserver` emits bounded fields at debug level for success and error
level for failures. It works with standard `slog` and therefore with `log`.
Logging remains synchronous with the configured handler; use the bounded
handler facilities in `log` if exporter latency must be isolated.

`otelpostgres.New` accepts a standard OpenTelemetry `metric.MeterProvider` and
records:

- `db.client.operation.duration`
- `db.client.operation.count`
- `db.client.connection.count` with fixed states `acquired`, `idle`, `total`,
  and `max`

For query spans, configure
`telemetry/instrumentation/gopostgres.Tracer` through `Config.Configure`:

```go
tracer, _ := gopostgres.New(gopostgres.Config{
    TracerProvider: runtime.TracerProvider(),
    MeterProvider: runtime.MeterProvider(),
    Operations: []string{"users.by_id", "jobs.claim"},
})

config.Configure = func(native *pgxpool.Config) error {
    native.ConnConfig.Tracer = tracer
    return nil
}

queryCtx := gopostgres.ContextWithOperation(ctx, "users.by_id")
user, err := queries.UserByID(queryCtx, userID)
```

The adapter uses an allow-list and collapses unknown names. It records no SQL,
arguments, or raw database error text. Keep operation sets finite and static.
The independently version-aligned `gopostgres` package test is part of the
compatibility audit; it remains optional and is not pulled into this module's
dependency graph.
