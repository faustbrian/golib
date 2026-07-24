# Ownership and sibling integration

The sibling contract was audited on 2026-07-18 against `service` remote
commit `258473c234466be0958762b1275d161929754503`. Its `serverhttp.New`
constructor installs, in order, recovery, request IDs, and body limiting before
caller middleware. `adapter.GoServiceDefaults` therefore returns exactly
`Recovery`, `RequestID`, and `BodyLimit`; `adapter.ValidateGoService` rejects an
explicit descriptor with any of those names.

Only one implementation may own each concern. To adopt this package with the
current `service` default server, omit local recovery, request ID, and body
limit layers. If a later service configuration delegates one of those
concerns, pass the actual service-owned list to `ValidateGoService` rather than
assuming the historical default.

| Concern | Authority | This module's role |
|---|---|---|
| server lifecycle, listener, shutdown, default core middleware | `service` | detect named overlap only |
| routes, parameters, route-local middleware | `router` | wrap compiled handler and observe bounded route name |
| authentication and credentials | `authentication` | adapt standard middleware by name |
| authorization decisions | `authorization` | adapt standard middleware by name |
| quotas and distributed rate state | `rate-limit` | local admission remains concurrency-only |
| replay and idempotency state | `idempotency` | preserve owning handler contract |
| spans, exporters, propagators, and sampling | `telemetry` | consume a bounded completion event only |
| logging backend and sinks | `log` | consume a bounded completion event only |

The `router` contract was audited at remote commit
`3aa46322f0305594f36ba080f0fbfd7354b36989`. Its immutable router stores a
copied `RouteInfo` in request context and exposes it through `MatchedRoute`.
Route-local middleware should pass only a stable name or pattern to
`observe.RecordRoute`, never parameters, metadata values, raw paths, or
request targets. The pinned sibling integration test proves this through an
actual compiled router.

Neither `adapter` nor any other production package imports a sibling library.
That prevents cycles and ensures adapters cannot create exporters, SDKs,
stores, registries, or concern-specific state machines.
