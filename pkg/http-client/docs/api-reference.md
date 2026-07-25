# Public API Reference

Every exported identifier is documented at its declaration and is available
through standard Go documentation:

```console
go doc -all github.com/faustbrian/golib/pkg/http-client
```

The CI `docs` gate verifies that complete generated reference. The stable API
is organized by these entry points. The
[production hardening audit](hardening.md) maps them to threats, defaults,
evidence, findings, and release gates.

| Area | Primary API |
| --- | --- |
| Client lifecycle | `Config`, `New`, `Client`, `TransportOwnership`, `TransportError` |
| Requests and bodies | `RequestSpec`, `RequestLayer`, query constructors, `RequestBody`, form and multipart constructors |
| Middleware | stage constructors, `MiddlewareOptions`, `Pipeline`, `PipelineInspection`, `TransportFunc` |
| Authentication and sessions | auth constructors, `AuthenticationOptions`, OAuth2 sources, `SessionConfig` |
| Identity and resilience | operation/idempotency context APIs, retry, limiter, and circuit-breaker constructors |
| Pagination and pools | typed paginator strategies, `Pool`, generators, channels, limits, and results |
| Cache | cache middleware, stores, controls, provenance, schedulers, and invalidation |
| Responses and transfers | status classification, typed decoding, drain, compression, range, copy, file, and resume APIs |
| Scope and profiles | `PolicyScope`, resource keys, built-in profiles, overrides, resolved policy and provenance |
| Egress and TLS | `EgressPolicy`, `TLSPolicy`, options, typed denial and pin errors |
| Observability | telemetry observer/propagator ports, events, W3C context, metric labels, and slog adapter |
| Testing | scripted replay, sanitized recorder, fixture schema, persistence, migrations, and failure categories |

Sentinel errors are compatibility contracts and are documented beside their
typed error families. Prefer `errors.Is` and `errors.As`; message text is not an
API. Each area links from the README to its behavioral guide, which defines
ordering, bounds, ownership, and security semantics beyond type signatures.
