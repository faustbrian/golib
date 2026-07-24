# Architecture

The package has two phases. A single-owner builder validates and flattens route
descriptors. Compilation registers package-produced patterns in fresh
`http.ServeMux` values and publishes immutable copied indexes. Dispatch is a
read-only handler call with no discovery, I/O, goroutine, cache refresh, or
registry lookup.

The root package owns route descriptors, compilation, dispatch, groups,
mounts, URL generation, errors, and introspection. `routertest` may provide
consumer test helpers. Optional adapters are added only when they can depend on
the root without introducing a reverse dependency.

Integration packages retain ownership of their concerns. `service` receives
the compiled router as an `http.Handler`; JSON-RPC, webhook, health, metrics,
and debug endpoints are ordinary handlers; authentication, authorization,
rate limiting, idempotency, and telemetry are ordinary middleware.
