# Propagation

## Default policy

W3C `traceparent` and `tracestate` are accepted within an 8 KiB combined
bound. Baggage is disabled. Outbound injection replaces stale propagation
headers rather than appending to them.

```go
config.Propagation = propagation.DefaultConfig()
```

Malformed or oversized headers are ignored; they do not fail the request.

## Trusted baggage

Enable baggage only for authenticated, controlled peers and define a finite
allow-list:

```go
config.Propagation.BaggageEnabled = true
config.Propagation.TrustedBaggageKeys = []string{"tenant.tier"}
config.Propagation.MaxBaggageItems = 4
config.Propagation.MaxHeaderBytes = 1024
```

Public HTTP handlers remain untrusted. Mark a fixed internal handler only when
the network/authentication boundary proves the peer is trusted:

```go
handler, err := nethttp.NewHandler(next, nethttp.ServerConfig{
    Operation: "internal.orders.list",
    Route: "/internal/orders",
    TrustedInbound: true,
    Propagator: runtime.Propagator(),
})
```

`TrustedInbound` has no effect on propagators that do not implement trusted
extraction; they use the standard interface. Unknown baggage keys are always
dropped. Never allow user IDs, request IDs, emails, tokens, or arbitrary tenant
input.

## Manual extraction

`Policy.Extract` is untrusted and clears baggage. `Policy.ExtractTrusted`
filters trusted baggage. Use the explicit method at messaging or RPC boundaries
that are not covered by the HTTP adapter.

Outbound baggage uses the same allow-list and bounds. If the serialized value
exceeds the byte budget, the baggage header is cleared.
