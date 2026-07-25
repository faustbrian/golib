# Graceful shutdown

## Order

1. Stop accepting new requests or jobs.
2. Drain in-flight application work.
3. Create a bounded shutdown context.
4. Call `Runtime.Shutdown` once from the owning lifecycle.
5. Report the joined error without retrying indefinitely.

```go
shutdownCtx, cancel := context.WithTimeout(
    context.WithoutCancel(signalCtx),
    10*time.Second,
)
defer cancel()

err := errors.Join(server.Shutdown(shutdownCtx), runtime.Shutdown(shutdownCtx))
```

The runtime applies the shorter of the caller deadline and
`Config.ShutdownTimeout`. Repeated and concurrent calls execute shutdown once
and return the same result.

## Globals

When `RegisterGlobal` is true, shutdown restores previous tracer, meter, and
propagator globals only if they still point to this runtime. If another library
replaced a global, shutdown leaves it untouched. A second globally registered
runtime is rejected and its partial providers are cleaned up.

## Kubernetes

Set `terminationGracePeriodSeconds` longer than application drain plus telemetry
shutdown. Derive the shutdown context from `context.WithoutCancel(signalCtx)`;
using the already-cancelled signal context would skip export immediately.

## Failure handling

Shutdown joins force-flush, provider, and exporter failures. The runtime records
exporter shutdown errors itself because supported OTel SDK versions differ in
whether provider shutdown returns them. Log or report the aggregate once, then
allow the process to exit when the deadline expires.
