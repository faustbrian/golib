# Recommended Stacks

These compositions are explicit starting points, not framework presets. Each
service should import only the modules required by its observable contract.

## Construction Rules

Initialize configuration and secrets first, then logs and telemetry, durable
clients, application services, transport adapters, and the process lifecycle.
Start accepting work only after dependencies and migrations are ready. During
shutdown, stop admission first, drain in-flight work, flush delivery and
telemetry, then close infrastructure clients.

| Scenario | Required modules | Optional modules | Primary owner |
| --- | --- | --- | --- |
| Minimal HTTP service | `service`, `router` | `http-middleware`, `config`, `log`, `telemetry` | `service` owns process lifecycle; handlers own no infrastructure construction |
| Internal JSON-RPC | `service`, `jsonrpc` | `openrpc`, authentication, authorization, idempotency | JSON-RPC adapter maps transport to application use cases |
| External JSON:API | `service`, `jsonapi` | `api-query`, authentication, authorization, cache | JSON:API adapter owns document and query projection |
| OpenAPI-described HTTP | `service`, `router`, `openapi` | middleware, authentication, validation | Handlers implement HTTP; OpenAPI describes and validates the contract |
| Durable queue worker | `queue` | control plane, idempotency, telemetry, PostgreSQL | Worker owns claim, acknowledgement, retry, and drain lifecycle |
| Scheduled singleton | `scheduler`, `lease` | queue, PostgreSQL or Valkey lease adapter | Scheduler computes runs; lease owns cross-replica exclusion |
| Transactional service | `postgres`, `migrations`, `outbox` | idempotency, queue relay | Application transaction writes business state and outbox atomically |
| Vendor API client | `http-client` | retry, rate limit, circuit breaker, cache, telemetry | Client contract owns remote semantics; resilience wraps only safe operations |
| File ingestion | `filesystem`, `tabular` or `wire` | queue, external sort, telemetry | Ingest use case owns limits and checkpoints; filesystem owns transport |
| Authenticated service | `authentication`, `authorization` | password, capability, tenancy | Authentication establishes principal; authorization evaluates action |

## Request Lifecycle

For HTTP and RPC services, apply correlation and request limits before parsing
large bodies. Authenticate before authorization. Keep protocol validation at
the adapter boundary, invoke one application use case, then project the result.
Retries and circuit breakers belong around outbound failure boundaries, not the
whole inbound request by default.

## Job Lifecycle

Claim durable work, establish idempotency or fencing before side effects,
propagate cancellation, record terminal outcome, then acknowledge. Retriable,
terminal, and unknown outcomes must remain distinct. Operator control belongs
in `queue-control-plane`; worker supervision remains part of the deployed
runtime.

## Persistence Lifecycle

Run migrations as a controlled deployment job. Build pools with bounded
timeouts, expose readiness only after required dependencies are usable, and
close admission before draining transactions. Use an outbox when database state
and asynchronous publication must share one commit boundary.

Package-specific guarantees and examples remain authoritative. See the
[integration map](integration-map.md), [architecture](architecture.md), and
[package catalog](package-catalog.md).

Return to the [documentation index](index.md).
