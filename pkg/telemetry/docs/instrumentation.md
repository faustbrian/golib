# Instrumentation

All adapters accept standard providers. Pass runtime providers explicitly when
`RegisterGlobal` is false.

## net/http server

```go
handler, err := nethttp.NewHandler(next, nethttp.ServerConfig{
    Operation: "orders.show",
    Route: "/orders/{order}",
    TracerProvider: runtime.TracerProvider(),
    MeterProvider: runtime.MeterProvider(),
    Propagator: runtime.Propagator(),
})
```

Operation and route are fixed at construction. Raw request target, host,
headers, body, and client address are never attributes. Trusted baggage is an
explicit per-handler option described in [propagation](propagation.md).

## net/http and http-client clients

```go
transport, err := nethttp.NewTransport(http.DefaultTransport, nethttp.ClientConfig{
    Operation: "payments.request",
    TracerProvider: runtime.TracerProvider(),
    MeterProvider: runtime.MeterProvider(),
    Propagator: runtime.Propagator(),
})
client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
```

`instrumentation/gohttpclient.NewTransport` exposes the same contract at the
RoundTripper seam used by `http-client`. Outbound propagation replaces stale
headers on a cloned request.

## postgres

```go
tracer, err := gopostgres.New(gopostgres.Config{
    TracerProvider: runtime.TracerProvider(),
    MeterProvider: runtime.MeterProvider(),
    Operations: []string{"users.by_id", "users.create"},
})
pgxConfig.Tracer = tracer

ctx = gopostgres.ContextWithOperation(ctx, "users.by_id")
```

Unknown operation names collapse to `postgresql.query`. SQL and arguments are
ignored. Errors expose only bounded outcome and valid SQLSTATE.

## cache

```go
ctx, end := cacheTelemetry.Start(ctx, gocache.OperationGet)
result, err := cache.Get(ctx, key)
end(gocache.OutcomeHit, err)
```

Map the target cache's result state to a finite `Outcome`. The adapter API has
no key or value parameter, preventing accidental recording.

## queue

```go
instrumented := goqueue.WrapHandler(queueTelemetry, handler)
queue.WithFn(instrumented)
```

The generic wrapper preserves the target message type but never inspects it.
Backend is a finite construction-time enum; raw errors and panics are preserved
for application behavior but replaced with a fixed telemetry status.
