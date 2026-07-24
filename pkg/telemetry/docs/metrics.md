# Metrics

## Provider and export

Metrics use a standard SDK meter provider and periodic reader. The default
interval is one minute with a 30-second export timeout.

```go
counter, err := runtime.Meter("orders").Int64Counter("orders.created")
counter.Add(ctx, 1)
```

Instrument names begin with a letter and use stable dotted names. Units follow
OpenTelemetry/UCUM conventions such as `s`, `By`, and `{request}`. Names, units,
and descriptions are compatibility contracts.

## Views and boundaries

```go
config.Metrics.Views = []telemetrymetric.ViewConfig{{
    Name: "http.server.request.duration",
    Unit: "s",
    AllowedAttributes: []attribute.Key{
        "http.request.method",
        "http.route",
        "http.response.status_code",
    },
    Boundaries: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
    NoMinMax: true,
}}
```

Boundaries must be finite and strictly increasing. Attribute lists are
allow-lists; an empty list records no attributes for that view. Views should be
reviewed with dashboards because changing boundaries or attributes changes the
time-series contract.

## Cardinality

`CardinalityLimit` is a hard per-instrument bound and defaults to 1,000. The
SDK collapses overflow rather than allocating without limit. A cardinality cap
is a final safety net, not permission to use IDs as attributes. See
[cardinality](cardinality.md).

## Failures

Metric export occurs outside request execution. `ForceFlush` collects and
exports synchronously for controlled lifecycle points. Do not call it from a
hot path. Export failure must not change application results.
