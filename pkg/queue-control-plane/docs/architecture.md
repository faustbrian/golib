# Architecture and trust boundaries

## Responsibility split

`queue` is the data plane. It owns delivery, acknowledgement, retry,
reclaim, pending recovery, dead letters, backend adapters, queue serialization,
worker concurrency, graceful drain enforcement, and command enforcement.

`queue-control-plane` is the control plane. It owns authenticated
administrative workflows, authorization, durable idempotency, desired state,
audit history, bounded fleet presentation, HTTP and CLI surfaces, and the
optional Kubernetes Deployment integration.

The control plane must never issue raw Redis or Valkey queue commands or
reimplement queue semantics. Workers must continue normal delivery while the
control plane is unavailable, except for durable state such as a pause that a
worker already knows how to enforce.

## Mutation flow

Every mutation follows one sequence:

1. Decode and validate a bounded command envelope.
2. Authenticate the caller and authorize the action, tenant, resource type,
   and resource ID.
3. Atomically persist the command, initial audit event, and applicable desired
   state under the tenant and idempotency key.
4. Dispatch through either the `queue` management adapter or the narrowly
   scoped Kubernetes scale adapter.
5. Atomically persist the terminal result and completion audit event.

A repeated idempotency key returns the stored result and is not dispatched
again. Reusing a key for a different command returns an idempotency conflict.
The service allocates a distinct UUID command ID before the first journal
write. That identifier is preserved through PostgreSQL, queue dispatch,
desired-state attribution, audit reads, API responses, clients, and CLI output;
the idempotency key remains a separate caller-owned deduplication contract.
If dispatch fails before reaching a tenant controller, the public result is
`failed` with the redacted code `dispatch_failed`. Published `queue`
pending, dispatched, acknowledged, failed, unsupported, timed-out, canceled,
partial, succeeded, and unknown results remain distinct. A lost acknowledgement is
`outcome_unknown`; callers must inspect the command before retrying. Malformed
adapter output also fails safely without exposing its contents.

## Persistence boundary

PostgreSQL stores command envelopes and results, desired state, chained audit
events, and audit retention anchors. The initial schema is embedded in the
binary and applied through `migrations`. Database access is provided through
`postgres` and pgx.

Audit entries form a tenant-local SHA-256 chain. Retention advances a durable
anchor before deleting a bounded contiguous prefix, allowing verification of
the retained suffix. The one-shot retention process then removes only old
terminal commands with no retained audit or current desired-state reference.
Serving replicas never schedule cleanup internally.

Control state must not be placed in an evictable cache. Ephemeral heartbeat or
coordination storage may be added only when it is part of the published
`queue` management protocol.

## Kubernetes boundary

The Kubernetes adapter is namespace-scoped by an immutable tenant mapping. It
can list Deployments and read or update only the Deployment `scale`
subresource. It does not create workloads, watch arbitrary resources, manage
pods, supervise processes, or replace HPA/KEDA.

Scaling is explicitly authorized as action `scale`, resource type `workload`,
and the Deployment name. Scaling to zero additionally requires confirmation.
The server uses in-cluster Kubernetes credentials when this adapter is enabled.

## Protocol and failure isolation

Fleet models preserve explicit `stale` and `unknown` states and negotiate a
worker protocol range and capability intersection. Unsupported or disconnected
workers must never be presented as healthy or compatible by inference.

The `dataplane` package translates every control command, including bounded
bulk retry and guarded replay, through a tenant-resolved
`management.Controller`. It never acquires a Redis or Valkey client. The
production process starts with an unavailable data-plane dispatcher and
replaces it only when the authenticated tenant management document is
configured. The remote endpoint must provide native command enforcement; the
control plane only transports validated commands and acknowledgements. Without
that configuration it continues to serve probes, audit and command history,
and configured Kubernetes workload operations.

The administrative desired-state endpoint reads only PostgreSQL and returns
the stable `queue` record contract. A worker binds the typed client to its
tenant and calls `DesiredStateReconciler.Reconcile` from an
application-supervised, cancellation-aware loop. The managed root queue owns
admission, in-flight accounting, pause, drain, and termination. Neither the
control plane nor its client starts worker polling goroutines or opens a Redis
or Valkey connection.

When explicitly enabled, the process owns and shuts down a non-global
`telemetry` runtime. Standard OpenTelemetry providers are passed directly to
HTTP and command instrumentation; Collector reachability never changes queue
delivery because workers remain independent of the control plane.

## Package map

- Root package: commands, permissions, validation, and public result models.
- `alerts`: bounded platform-alert inputs from validated current snapshots and
  a fixed-label exporter using the deployment's `telemetry` meter.
- `apihttp`: versioned administrative API and transport protections.
- `authz`: authentication-principal to authorization-request mapping.
- `control`: orchestration, desired state, dispatch routing, and telemetry.
- `dataplane`: tenant-scoped `queue` command and result adaptation.
- `fleet`: bounded worker registry and protocol compatibility.
- `history`: tamper-evident audit model and verification.
- `postgres`: migrations and durable repositories.
- `kubernetes`: namespace-scoped Deployment status and scale adapter.
- `client` and `cli`: typed automation client and command workflows.
- `server`: authentication composition and bounded HTTP lifecycle.
- `cmd/queue-control-plane`: deployable server process.
- `cmd/queue-control`: administrative CLI.
