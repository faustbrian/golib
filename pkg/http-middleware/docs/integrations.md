# Integration cookbook

## router

Resolve this package's server chain around the compiled router. Route-local
middleware remains visible through `router.RouteInfo.Middleware`. Inject a
route-local bridge that passes a name or pattern from
`router.MatchedRoute(request)` to `observe.RecordRoute`; never record
parameters, metadata values, or raw request paths.

## service

The current default `service/serverhttp` stack owns recovery, request IDs,
and body limits. Before adding an explicit chain, call
`adapter.ValidateGoService(chain, adapter.GoServiceDefaults())`. A validation
error means one implementation must be disabled or omitted. Do not install both.
The exact audited sibling revisions and ownership table are recorded in
[ownership and sibling integration](ownership.md).

## Authentication and authorization

Wrap `authhttp.NewMiddleware` from `authentication` with
`adapter.Named(adapter.Authentication, middleware)`. Wrap the handler returned
by `authorization/authhttp` at the declared authorization position. This
package never parses credentials or decides access.

## Rate limiting and idempotency

Use `rate-limit/ratelimithttp` and `idempotency/idempotencyhttp` directly.
Local `admission` protects concurrency but is not a quota. Request IDs are not
idempotency keys. Preserve each owning package's failure and replay contracts.

## Logging and telemetry

Convert `observe.Event` in an injected observer. `log`, `log/slog`, and
`telemetry` own backends, span lifecycle, exporters, and sampling. Trace
Context propagation remains in the telemetry adapter; this core neither starts
spans nor registers a global propagator.

## Recommended integration chain

`recovery -> proxy -> request ID -> observe -> CORS -> security headers ->
admission -> body/deadline -> authentication -> rate limit -> authorization ->
idempotency -> compression -> router/application`

Change order only with a documented ownership and short-circuit analysis.
