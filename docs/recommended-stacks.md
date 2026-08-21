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
| Mixed-protocol service | `service` plus each selected transport module | `jsonrpc`, `jsonapi`, `router`, `openrpc`, `openapi`, `webhook` | Application use cases are shared; every adapter owns its transport models and compatibility |
| Durable queue worker | `queue` | control plane, idempotency, telemetry, PostgreSQL | Worker owns claim, acknowledgement, retry, and drain lifecycle |
| Queue producer, ingester, and processor | `queue`, `service` | filesystem, tabular, wire, idempotency, outbox, telemetry | Ingress owns bounded decoding and checkpoints; workers own settlement and durable progress |
| Scheduled singleton | `scheduler`, `lease` | queue, PostgreSQL or Valkey lease adapter | Scheduler computes runs; lease owns cross-replica exclusion |
| Transactional service | `postgres`, `migrations`, `outbox` | idempotency, queue relay | Application transaction writes business state and outbox atomically |
| Webhook sender and receiver | `webhook`, `http-signature` | queue, idempotency, audit, telemetry | Sender owns durable attempts; receiver owns verification, replay rejection, and idempotent handling |
| Vendor API client | `http-client` | retry, rate limit, circuit breaker, cache, telemetry | Client contract owns remote semantics; resilience wraps only safe operations |
| File ingestion from local, SFTP, S3, or R2 | `filesystem`, `tabular` or `wire` | queue, external sort, telemetry | Ingest use case owns limits and checkpoints; the selected filesystem adapter owns transport |
| Kubernetes configuration and secrets | `config` | Infisical platform injection, runtime refresh adapters | Deployment injects secrets; the application validates typed snapshots and owns safe replacement |
| Authenticated service | `authentication`, `authorization` | password, capability, tenancy | Authentication establishes principal; authorization evaluates action |
| Observable Kubernetes deployment | `service`, `log`, `telemetry`, `correlation` | Better Stack export, audit, queue diagnostics | Service exposes bounded lifecycle signals; deployment owns collection, routing, retention, and alerts |

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

## Mixed Protocol Lifecycle

Construct shared application use cases before transport adapters. Register each
adapter independently, apply only middleware valid for that transport, and
start all listeners under one explicit service lifecycle. During shutdown,
close admission on every transport before draining shared work. A JSON-RPC
request, JSON:API resource, conventional HTTP payload, and webhook event must
not share transport models merely because they call the same use case.

## Ingestion Lifecycle

Open the source through `filesystem`, apply compressed and decoded size limits,
stream records through `tabular` or `wire`, and persist a durable checkpoint
before acknowledging upstream work. Dispatch expensive processing only after
the source position and idempotency identity are durable. On shutdown, stop
new reads, finish or abandon the current record according to the source
contract, flush checkpoints and outbox records, then close the source.

## Configuration And Observability Lifecycle

Load and validate local `.env` or file configuration before constructing
clients. In Kubernetes, inject Infisical-managed values at the platform
boundary; enable runtime refresh only for consumers that can atomically swap,
overlap, and retire credentials. Construct logging and telemetry before other
components, propagate correlation at admission, and flush bounded exporters
after traffic and workers drain.

Package-specific guarantees and examples remain authoritative. See the
[integration map](integration-map.md), [architecture](architecture.md), and
[package catalog](package-catalog.md). Maintained executable compositions are
listed in [runnable recipes](recipes/index.md); deployment ownership and limits
are in [operations](operations/index.md) and [limitations](limitations.md).

Return to the [documentation index](index.md).
