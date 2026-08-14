# Runnable Recipes

Golib recipes use maintained integration modules instead of copied snippets.
Each module compiles against public package APIs and owns executable behavior
tests. Run recipes through the root command surface so module isolation and
catalog policy match CI.

| Recipe | Composition | Executable fixture |
| --- | --- | --- |
| [HTTP and JSON-RPC service](service.md) | lifecycle, routing, authentication, authorization, tenancy, correlation, validation, telemetry, audit | `pkg/service/integration/reference-http` |
| [Durable command and worker flow](durable-worker.md) | PostgreSQL, migrations, idempotency, outbox, Valkey Streams, recovery | `pkg/service/integration/reference-durability` |
| [External dependency integration](external-integration.md) | HTTP client, resilience, webhooks, filesystem, secret envelopes | `pkg/service/integration/reference-external` |
| [Linux container lifecycle](service.md#container-and-load-proof) | non-root containers, read-only filesystem, probes, limits, TLS, DNS, SIGTERM | `pkg/service/integration/reference-platform` |
| [Track, Postal, and Location adoption](service.md#adoption-fixtures) | explicit service roles and package construction without a private framework | `pkg/service/integration/adoption` |

The fixtures prove only their documented boundaries. Production deployment,
managed failover, public releases, and the overall readiness verdict remain in
[operational assurance](../operational-assurance.md).

Return to the [documentation index](../index.md).
