# Operations, Attempts, Tracing, And Metrics

Observability is optional. `Client` emits no logs, spans, metrics, correlation
headers, or propagation headers unless `Config.Telemetry` is configured.

The lifecycle has two scopes:

- one `TelemetryOperation` around the complete logical `Client.Do` call; and
- one numbered `TelemetryAttempt` for every physical `RoundTrip`.

Retries, redirects, authentication challenges, and cache revalidation create
new attempt lifecycles beneath the same operation context. An operation cache
hit has no physical attempt. The resolved policy profile and operation ID stay
stable across the operation; attempt numbers increase from one.

## Observer and propagation ports

`TelemetryObserver.Start` may return a derived context containing a span.
`Finish` receives that same context and a closed outcome category. Observer or
propagator panics are contained and cannot fail the HTTP operation.

`TelemetryPropagator.Inject` receives only a cloned physical-attempt header.
The caller's `http.Request` remains unchanged. The default correlation field is
`X-Request-ID` and carries the validated logical operation ID. A different
valid HTTP field name can be selected with `CorrelationHeader`.

```go
client, err := httpclient.New(httpclient.Config{
	Telemetry: &httpclient.TelemetryOptions{
		Observer:          observer,
		Propagator:        propagator,
		BaggageAllowlist:  []string{"locale"},
		SensitiveHeaders:  []string{"X-Vendor-Token"},
		CorrelationHeader: "X-Request-ID",
	},
})
```

Observers and propagators can be adopted independently. Inputs are snapshotted
at client construction. Empty baggage allowlists strip all baggage. The core
filters baggage both before and after adapter injection, so a propagator cannot
bypass the allowlist.

When an attempt crosses an origin boundary, the client removes Authorization,
Proxy-Authorization, Cookie, configured sensitive fields, Traceparent,
Tracestate, and Baggage before authentication or signing middleware runs. It
does not invoke the trace propagator for that crossed-boundary attempt. The
redirected destination therefore receives trace state only if later explicit
destination policy adds new state.

## W3C Trace Context

`WithW3CTraceContext` validates a strict W3C Trace Context v00 `traceparent`
and bounded `tracestate`. `W3CTraceContextPropagator` injects those values on
trusted attempts:

```go
ctx, err := httpclient.WithW3CTraceContext(ctx, traceparent, tracestate)
client, err := httpclient.New(httpclient.Config{
	Telemetry: &httpclient.TelemetryOptions{
		Propagator: httpclient.W3CTraceContextPropagator{},
	},
})
```

Trace baggage is deliberately separate and always subject to
`BaggageAllowlist`.

## OpenTelemetry and telemetry

The ports use standard `context.Context` and `http.Header`, so adapters do not
require a core dependency on an exporter or SDK. A `telemetry` runtime
already exposes the standard OpenTelemetry tracer, meter, and propagator
interfaces needed by an adapter:

```go
type propagator struct {
	value propagation.TextMapPropagator
}

func (adapter propagator) Inject(ctx context.Context, header http.Header) {
	adapter.value.Inject(ctx, propagation.HeaderCarrier(header))
}
```

An observer starts `http.client.operation` and `http.client.attempt` spans from
the context passed to `Start`, stores no request object, and ends the span from
`Finish`. It records metrics from `event.MetricLabels()` only. This preserves
the operation-to-attempt parent relationship while leaving provider,
exporter, sampling, and shutdown ownership with `telemetry`.

## Low-cardinality contract

`TelemetryEvent` never contains a URL, host, path, query, header, body, tenant,
credential, cursor, vendor message, or error text. `OperationID` exists for
trace and correlation use and must not become a metric label.

`MetricLabels()` is the enforced metric projection. It excludes operation ID,
classifies extension methods as `OTHER`, and returns only:

- operation or attempt scope;
- a standard HTTP method or `OTHER`;
- a named versioned profile;
- a closed success, HTTP, transport, cancellation, limiter, breaker, retry, or
  failure outcome;
- a bounded status class; and
- a closed cache miss, hit, revalidated, stale, or none category.

## slog and log

`NewSlogTelemetryObserver` logs the same fixed safe fields at debug level. It
never logs URL, headers, payloads, scope values, or error text. `log`
constructors return a standard `*slog.Logger`, so they plug into this adapter
directly without adding a mandatory `log` dependency:

```go
logger := log.JSON(os.Stdout, nil)
observer, err := httpclient.NewSlogTelemetryObserver(logger)
```
