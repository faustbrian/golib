# Integration Map

Golib modules compose inward through standard-library types and narrow
interfaces. Provider adapters depend on the owning contract; core contracts do
not import provider implementations.

## Ownership

| Concern | Owning module | Common adapters or companions |
| --- | --- | --- |
| Process lifecycle and probes | `service` | config, log, telemetry |
| HTTP dispatch | `router` | `http-middleware` |
| Identity establishment | `authentication` | JWT, OIDC, HTTP adapters |
| Access decisions | `authorization` | PostgreSQL, Valkey, HTTP/RPC adapters |
| RPC protocol | `jsonrpc` | `openrpc`, service |
| Resource protocol | `jsonapi` | `api-query`, service |
| HTTP description | `openapi` | router and middleware |
| Database connectivity | `postgres` | migrations, outbox, settings |
| Durable jobs | `queue` | backend adapters, control plane |
| Transactional publication | `outbox` | queue and broker relays |
| Duplicate execution control | `idempotency` | PostgreSQL and Valkey adapters |
| Scheduling | `scheduler` | lease and queue |
| Outbound HTTP | `http-client` | retry, rate limit, circuit breaker, cache |
| Files and object storage | `filesystem` | local, SFTP, S3-compatible/R2 adapters |
| Logs and telemetry | `log`, `telemetry` | slog and OpenTelemetry integrations |

## Dependency Direction

```text
composition root
  -> provider adapters and transports
      -> application-facing contracts
          -> standard-library and immutable domain values
```

The composition root selects concrete PostgreSQL, Valkey, broker, filesystem,
or telemetry adapters. Application code receives the smallest contract it
needs. Transport packages translate inputs and outputs but do not own business
transactions. Cross-cutting policies wrap their actual boundary and do not
become a service locator.

Avoid circular convenience adapters. If two modules need each other's concrete
types, move the shared contract inward or keep the integration in a separate
adapter module owned by the higher-level concern.

See [design language](design-language.md), [module dependencies](module-dependencies.md),
and [recommended stacks](recommended-stacks.md).

Return to the [documentation index](index.md).
