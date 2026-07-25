# telemetry

`telemetry` is a vendor-neutral OpenTelemetry runtime for Go services. It
owns resource identity, trace and metric providers, OTLP exporters,
propagation, sampling, global registration, flush, and shutdown while returning
the standard OpenTelemetry APIs.

The package targets an OpenTelemetry Collector. It does not wrap vendor SDKs,
replace OpenTelemetry types, read configuration implicitly, or add log-signal
stability promises.

## Requirements

- Go 1.25 or 1.26
- OpenTelemetry Go 1.43.x or 1.44.x
- an OTLP-compatible Collector for production export

## Quick start

```go
package main

import (
	"context"
	"log"

	telemetry "github.com/faustbrian/golib/pkg/telemetry"
)

func main() {
	config := telemetry.DefaultConfig("orders", "1.2.3")
	config.Environment = "production"
	config.Traces.Exporter.Endpoint = "otel-collector:4317"
	config.Metrics.Exporter.Endpoint = "otel-collector:4317"

	runtime, err := telemetry.Init(context.Background(), config)
	if err != nil {
		log.Fatal(err)
	}

	ctx, span := runtime.Tracer("orders").Start(context.Background(), "orders.list")
	defer span.End()
	_ = ctx

	if err := runtime.Shutdown(context.Background()); err != nil {
		log.Fatal(err)
	}
}
```

`DefaultConfig` only builds a value. Network clients, providers, globals, and
goroutines are created by `Init`. `Runtime` exposes standard tracer, meter, and
propagator interfaces. `Shutdown` is idempotent, bounded by the configured
timeout, restores only globals still owned by the runtime, and joins provider
and exporter failures.

## Safe defaults

| Setting | Default |
| --- | --- |
| signals | traces and metrics enabled |
| transport | OTLP/gRPC to `localhost:4317` |
| transport security | explicit insecure local Collector connection |
| compression | gzip |
| trace sampling | parent-based 10% ratio |
| span queue / batch | 2,048 / 512 |
| metric cardinality | 1,000 points per instrument |
| baggage | disabled |
| shutdown timeout | 10 seconds |

Every default is represented in `Config` and can be inspected or overridden.
Production clusters should configure TLS or deliberately retain an
authenticated cluster-local insecure connection.

## Packages

- root: configuration, resources, provider lifecycle, globals, and errors
- `otlp`: explicit OTLP/gRPC and OTLP/HTTP exporter construction
- `trace`: always-on, always-off, ratio, and parent-based samplers
- `metric`: views, histogram boundaries, attribute allow-lists, and cardinality
- `propagation`: bounded W3C trace context and trusted baggage policies
- `instrumentation/nethttp`: private-by-default `net/http` server and client
- `instrumentation/gohttpclient`: `http-client` RoundTripper adapter
- `instrumentation/gopostgres`: pgx query tracer for `postgres`
- `instrumentation/gocache`: dependency-neutral `cache` observations
- `instrumentation/goqueue`: dependency-neutral `queue` handler wrapper
- `testtelemetry`: deterministic in-memory providers and snapshots

Instrumentation never records raw URL paths, queries, hosts, headers, client
addresses, SQL, query arguments, database error text, cache keys or values,
queue messages, raw handler errors, or panic values by default.

## Documentation

Start with the [documentation index](docs/README.md) for the public contract,
architecture, compatibility, upgrade, contribution, and security material.

Runnable commands are in [`examples/service`](examples/service) and
[`examples/worker`](examples/worker).

## Development

```sh
make check
make race
make fuzz
make benchmark
```

CI also runs linting, vulnerability scanning, examples, Collector protocol
tests, and the supported Go/OpenTelemetry matrix. Library packages enforce
meaningful 100% statement coverage.

## Stability

Trace and metric APIs use stable OpenTelemetry interfaces. The log signal is
intentionally absent from the stable runtime; see [log stability](docs/logs.md).
Pre-v1 releases may refine configuration, but changes are documented in
[`CHANGELOG.md`](CHANGELOG.md) and follow semantic versioning.

## License

MIT. See [`LICENSE`](LICENSE).
