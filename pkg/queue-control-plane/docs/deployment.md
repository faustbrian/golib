# Deployment and configuration

## Server environment

The server fails startup with a secret-safe error when required values are
missing or invalid.

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `DATABASE_URL` | yes | none | PostgreSQL DSN. |
| `QUEUE_CONTROL_ACCESS_FILE` | serving | none | Static API-key and ACL JSON file. |
| `QUEUE_CONTROL_LISTEN_ADDRESS` | no | `:8080` | TCP listen address. |
| `QUEUE_CONTROL_ACCESS_MAX_BYTES` | no | `1048576` | Access-file bound, from 1 byte to 1 MiB. |
| `QUEUE_CONTROL_ALLOWED_ORIGINS` | no | none | Comma-separated exact browser origins. |
| `QUEUE_CONTROL_RUN_MIGRATIONS` | no | `false` | Apply embedded migrations at startup. |
| `QUEUE_CONTROL_UI_ENABLED` | no | `false` | Serve the embedded console at `/ui/`. |
| `QUEUE_CONTROL_MIGRATE_ONLY` | no | `false` | Apply migrations and exit without serving. |
| `QUEUE_CONTROL_RETENTION_ONLY` | no | `false` | Apply one bounded audit-retention plan and exit. |
| `QUEUE_CONTROL_RETENTION_FILE` | with retention | none | Strict tenant retention-policy JSON path. |
| `QUEUE_CONTROL_RETENTION_MAX_BYTES` | no | `1048576` | Retention-file bound, from 1 byte to 1 MiB. |
| `QUEUE_CONTROL_KUBERNETES_TENANTS_FILE` | no | none | Tenant-to-namespace JSON mapping. |
| `QUEUE_CONTROL_KUBERNETES_TENANTS_MAX_BYTES` | no | `1048576` | Mapping-file bound, from 1 byte to 1 MiB. |
| `QUEUE_CONTROL_MANAGEMENT_TENANTS_FILE` | no | none | Tenant-to-worker management endpoint JSON mapping. |
| `QUEUE_CONTROL_MANAGEMENT_TENANTS_MAX_BYTES` | no | `1048576` | Management mapping bound, from 1 byte to 1 MiB. |
| `QUEUE_CONTROL_TELEMETRY_ENABLED` | no | `false` | Enable owned `telemetry` OTLP export. |
| `QUEUE_CONTROL_OTLP_ENDPOINT` | with telemetry | none | Collector host and port. |
| `QUEUE_CONTROL_OTLP_PROTOCOL` | no | `grpc` | `grpc` or `http/protobuf`. |
| `QUEUE_CONTROL_OTLP_INSECURE` | no | `false` | Explicitly use plaintext OTLP. |
| `QUEUE_CONTROL_OTLP_CA_FILE` | no | system roots | Custom collector CA PEM path. |
| `QUEUE_CONTROL_OTLP_CERTIFICATE_FILE` | no | none | mTLS client certificate path. |
| `QUEUE_CONTROL_OTLP_PRIVATE_KEY_FILE` | with certificate | none | mTLS private-key path. |
| `QUEUE_CONTROL_OTLP_SERVER_NAME` | no | endpoint host | TLS verification server name. |
| `QUEUE_CONTROL_TELEMETRY_ENVIRONMENT` | no | none | Deployment environment resource value. |
| `QUEUE_CONTROL_TELEMETRY_INSTANCE` | no | none | Stable service-instance resource value. |
| `QUEUE_CONTROL_TRUST_INBOUND_TRACE_CONTEXT` | no | `false` | Trust approved inbound trace context. |

An origin must be an exact `http` or `https` origin without user information,
path, query, or fragment. When no origins are configured, requests carrying an
`Origin` header are rejected. Non-browser clients without an `Origin` header
are unaffected.

The optional UI is served only when `QUEUE_CONTROL_UI_ENABLED=true`; one-shot
migration and retention modes reject that setting because they never open an
HTTP listener. The console uses same-origin public API requests and does not
add private endpoints. See the [web UI guide](ui.md) for its operator model.

## Telemetry export

Telemetry is disabled unless `QUEUE_CONTROL_TELEMETRY_ENABLED=true`. Enabling
it requires an explicit Collector endpoint. TLS with system roots is the
default; plaintext requires `QUEUE_CONTROL_OTLP_INSECURE=true`, while custom CA
and mTLS files must be mounted read-only. A client certificate and private key
must be configured together, and TLS settings cannot accompany plaintext mode.

The process owns a non-global `telemetry` runtime, supplies its standard
OpenTelemetry providers directly to HTTP and command instrumentation, and
performs bounded flush and shutdown when the server exits. Inbound trace
context is ignored unless the deployment explicitly enables trust after its
ingress strips untrusted propagation headers.

## Static access document

The access file is strict JSON: unknown fields, duplicate trailing values, an
invalid key, or an invalid ACL entry fail startup. Keep it on a read-only secret
volume and restrict filesystem access.

```json
{
  "keys": [
    {"id": "operations", "key": "secret-from-a-vault", "subject": "operator-1"}
  ],
  "acl": [
    {
      "id": "pause-critical",
      "subject": "operator-1",
      "tenant": "tenant-1",
      "action": "pause",
      "resource_type": "queue",
      "resource_id": "critical",
      "effect": "allow"
    }
  ]
}
```

The request headers are `X-Queue-Control-Key-ID` and
`X-Queue-Control-Key`. ACL evaluation is tenant scoped and deny overrides
allow. The ACL subject is the key's `subject`, not its public key ID. Supported
actions are `view`, `record_list`, `record_inspect`, `payload_view`,
`diagnostics_view`, `audit_view`, `pause`, `resume`, `drain`, `terminate`,
`retry`, `bulk_retry`,
`delete`, `purge`, `replay`, `scale`, and the reserved
`retention_configure`. Resource types are
`queue`, `worker`, `worker_group`, `failure`, `dead_letter`, and `workload`.

Diagnostic endpoints use `view` on these synthetic workload resources:

| Endpoint | Resource ID |
| --- | --- |
| workers | `fleet` |
| queues | `queues` |
| Kubernetes workloads | `kubernetes` |
| command result | `commands` |

Audit history uses `audit_view` on `workload/audit` rather than generic
`view`. Failure and dead-letter lists use `record_list`; exact-record
inspection uses `record_inspect`; payload bytes additionally require
`payload_view`; privileged diagnostics independently require
`diagnostics_view`.

Retention status uses the existing `view` decision on `workload/fleet` because
it is a projection of worker status. `retention_configure` is reserved for a
future mutating API and MUST NOT be granted as a substitute for any current
permission. Runtime retention changes remain unavailable until queue owns
the versioned command and backend behavior.

## PostgreSQL migrations

For a local evaluation, setting `QUEUE_CONTROL_RUN_MIGRATIONS=true` applies the
embedded schema before opening the runtime pool. In production, run the same
server image as a one-shot migration Job with only `DATABASE_URL` and
`QUEUE_CONTROL_MIGRATE_ONLY=true`. This mode applies every pending embedded
migration under the migration backend's lock and exits without reading the
access document, opening the runtime pool, binding a listener, or serving HTTP.
Start serving instances only after that Job succeeds, with both migration flags
disabled.

## Audit retention Job

Run the server image as a one-shot Job or CronJob with `DATABASE_URL`,
`QUEUE_CONTROL_RETENTION_ONLY=true`, and a read-only
`QUEUE_CONTROL_RETENTION_FILE`. Serving credentials, listeners, Kubernetes
access, migrations, and telemetry are disabled in this mode. The strict policy
document has one entry per tenant:

```json
{
  "tenants": [
    {
      "id": "tenant-1",
      "retention_seconds": 7776000,
      "batch_size": 1000,
      "max_batches": 100,
      "legal_hold": false
    }
  ]
}
```

Retention ranges from one hour to ten years. A run processes at most 1,000
tenants, 100 audit batches and 100 command batches per tenant, and 1,000 rows
per batch. Legal-hold tenants perform no database calls. Every active tenant
chain is verified before and after audit retention. Only after that proof does
the job delete old terminal commands that no retained audit event or current
desired state references. Accepted commands and commands backing current
desired state are never eligible. If a final permitted audit or command batch
is full, the process exits with `retention incomplete`; rerun the same policy
until partial batches prove both eligible sets are exhausted.

Deleting a terminal command releases its idempotency key for future reuse.
Choose `retention_seconds` to exceed the maximum retry, incident, and operator
replay window, and never treat an expired key as evidence that its old effect
did not occur.

Back up the PostgreSQL database consistently. It contains command outcomes,
desired operational state, audit events, and retention anchors. A restore must
restore all four together; restoring only audit events or only anchors breaks
chain verification. `make disaster-recovery-postgres` exercises that complete
restore contract against isolated real databases.

## Worker and queue status transport

Worker and queue status are enabled only when
`QUEUE_CONTROL_MANAGEMENT_TENANTS_FILE` is set. The strict document accepts at
most 1,000 unique tenants and references a separate bearer-token file for each
HTTPS worker management endpoint:

```json
{
  "tenants": [
    {
      "id": "tenant-1",
      "base_url": "https://queue-worker.tenant-1.svc:8443",
      "token_file": "/run/secrets/queue-worker-tenant-1-token"
    }
  ]
}
```

Unknown fields, duplicate tenants, non-HTTPS endpoints, endpoint paths or
credentials, empty tokens, and oversized documents or token files fail
startup with a stable error. Tokens are capped at 4,096 bytes, trimmed only at
their outer whitespace, never accepted inline, and never included in errors.
Mount the mapping and token files read-only. Use a high-entropy token per
tenant and rotate the worker handler and control-plane file together.

The configured runtime advertises `workers` and `queues` from
`/v1/capabilities` and enables both tenant status routes. Fleet reads follow at
most five validated 200-item pages, preserve request cancellation, derive
stale state at request time, and fail closed rather than returning an
incomplete fleet. The same authenticated tenant client also enables command
dispatch and failure/dead-letter reads through `management.RecordReader`.

On each worker endpoint, configure Redis Streams or Valkey Streams with
`WithManagementStatus`, construct `management.WorkerLifecycle`, and pass it to
the root queue with `queue.WithWorkerLifecycle`. For Valkey, also configure a
tenant-specific `WithFailureStream`; its worker implements
`management.RecordReader`. Compose worker and queue status providers with
`management.NewStatusReader`, then serve that reader, the Valkey worker as
`HandlerConfig.Records`, and the root queue through the authenticated
`managementhttp.NewHandler` bearer-token boundary. Redis 6.2 cannot provide
consumer-group lag and therefore reports depth and lag as unsupported rather
than fabricating zero; Valkey 9 supplies those measurements and the counters
owned by its worker.

The same tenant client dispatches control commands to `POST /v1/commands` with
protocol version 1 and a 30-second acknowledgement deadline. Configure
`managementhttp.HandlerConfig.Controller` with the managed root queue. It owns
pause, resume, drain, and terminate admission semantics; the Redis or Valkey
backend continues to own native measurements and settlement.

Give the worker a separate control-plane API key with `view` permission on its
exact queue, worker, or worker-group targets. Construct the typed administrative
client, call `DesiredStateReader(tenant)`, and supply that reader plus the root
queue to `management.NewDesiredStateReconciler`. The application must call
`Reconcile` from its own supervised, cancellation-aware loop. A missing record
is no authored change, not an instruction to resume.

Configure `managementhttp.HandlerConfig.Records` with a native backend
`management.RecordReader` before enabling failure operations. Redis Streams
and Valkey Streams workers satisfy this contract. Lists cross the transport
with hidden payloads only; inspection may return less
visibility than requested but never more. A missing native reader leaves the
worker record routes unavailable and must fail rollout verification.

## Container images

The `Dockerfile` has two targets:

```sh
docker build --target server -t queue-control-plane .
docker build --target cli -t queue-control .
```

Both images are static, run as numeric user and group `65532:65532`, and use a
scratch runtime. Run with a read-only root filesystem, all Linux capabilities
dropped, and `no-new-privileges`. Mount access, tenant mapping, and token files
read-only. The server listens on port 8080 by default.

Release metadata is injected with `VERSION`, `COMMIT`, and RFC3339 `BUILT_AT`
build arguments and is exposed by `/version`. Multi-platform builds support
`linux/amd64` and `linux/arm64`.

## Kubernetes

The optional mapping file has this strict shape:

```json
{"tenants":[{"id":"tenant-1","namespace":"queue-tenant-1"}]}
```

Enabling it requires an in-cluster service account. Grant only these namespaced
Deployment permissions:

- `get` and `list` on `apps/deployments`;
- `get` and `update` on `apps/deployments/scale`.

The adapter does not need pod, Secret, ConfigMap, create, delete, or watch
permissions. If HPA or KEDA also owns replicas, treat manual scale as a
short-lived intervention because the autoscaler can reconcile it again.

## Probes and shutdown

- `/health/live` reports process liveness.
- `/health/ready` checks PostgreSQL readiness and returns 503 on failure.

The process handles SIGINT and SIGTERM and uses bounded HTTP shutdown. A
deployment should stop routing traffic when readiness fails and allow its
termination grace period to cover server shutdown. The control plane never
supervises worker processes.
