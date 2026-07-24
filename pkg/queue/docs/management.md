# Management protocol

The `management` package is the stable boundary between `queue` workers and
an external control plane. It defines protocol and capability values only. It
does not connect to Redis, Valkey, Kubernetes, or any other backend, and it does
not supervise processes.

## Rolling-version negotiation

A control plane publishes the inclusive protocol range it supports. Each
worker reports its protocol version and capabilities. `management.Negotiate`
classifies the worker as compatible, older, newer, or unknown and returns a
deterministically sorted capability view.

```go
compatibility := management.Negotiate(
    management.ProtocolRange{
        Minimum: management.ProtocolVersion{Major: 1},
        Maximum: management.ProtocolVersion{Major: 1, Minor: 2},
    },
    worker.Protocol,
    worker.Capabilities,
    controlPlaneCapabilities,
)
```

Only the intersection of worker and control-plane capabilities is enabled, and
only when the protocol version is compatible. Older, newer, missing, and
invalid versions remain diagnosable but receive no enabled control capability.
Unknown capability strings are preserved in the diagnostic sets so a rolling
upgrade does not silently misrepresent either peer.

## Worker status

`WorkerStatus` carries worker identity, software version, start and heartbeat
times, queues, goroutine concurrency, current jobs, worker and drain state,
backend identity, protocol version, and capabilities. `Validate` rejects
missing fields and bounds identities, queues, capabilities, and concurrency
before a control plane accepts the report.

The contract contains no process handle and grants no authority to spawn,
restart, or supervise a worker.

`StatusReader` exposes workers and queues through opaque-cursor pages capped at
200 observations. Requests and adapter output validate their bounds and every
nested status before a control plane accepts them.

## Desired-state reconciliation

`DesiredRecord` carries an authored queue, worker, or worker-group lifecycle
state, a positive monotonic revision, change time, and originating command ID.
The supported states are active, paused, draining, and terminating.

`DesiredStateReconciler` reads a fixed bounded target set and applies each
revision at most once. It rejects revision rollback, rejects two different
records claiming the same revision, validates source output, and advances its
local revision only after successful application. A missing record means no
authored change; it is never inferred as active. A failed read or application
leaves normal queue delivery and the last applied revision unchanged.

The reconciler starts no goroutines and owns no retry timer. The worker
application must call `Reconcile` from its supervised, cancellation-aware
loop and decide how reconciliation failures are observed and retried. This
keeps control-plane availability outside the normal delivery lifecycle.

## Queue-owned lifecycle enforcement

`management.NewWorkerLifecycle` creates the synchronized admission boundary
used by `Queue`. Configure it with one worker identity, worker group, logical
queue, protocol metadata, a bounded idempotency-result capacity, and a clock.
Pass it through `queue.WithWorkerLifecycle` when constructing the queue.

The configured backend worker must implement `management.StatusProvider`.
`Queue` then implements `management.Controller`,
`management.DesiredStateApplier`, and `management.StatusProvider`, so the same
queue instance can be supplied to `managementhttp.HandlerConfig` for status
and commands and to `DesiredStateReconciler` for durable convergence.

Pause closes admission and waits for an outstanding backend request to return;
already admitted jobs continue. Drain closes admission and waits for admitted
jobs to finish settlement. Terminate uses the same drain boundary and then
shuts down the queue. Resume reopens admission. Context cancellation or a
command deadline stops the wait but does not invent completion. Command
idempotency results are bounded and never silently evicted; exhausted capacity
fails closed.

The lifecycle owns no goroutine and does not poll desired state. Applications
must run reconciliation in an application-supervised loop. Direct commands do
not author durable revisions: after a restart or control-plane recovery, the
desired-state source remains authoritative.

Backend status remains backend-owned. The queue decorates native worker status
with its synchronized lifecycle and current-job view and forwards native queue
measurements unchanged. Backends without honest status support cannot enable
the lifecycle option.

## Queue status

`QueueStatus` identifies the backend and logical queue at an observation time.
Its metrics cover depth, lag, pending work, oldest age, throughput, runtime,
successes, failures, retries, reclaims, dead letters, and settlement errors.

Every metric is a `Measurement` with a `Supported` flag. Backends must set it
only when they can measure the value honestly. A supported zero is therefore
different from an unsupported metric; consumers must not infer support from a
numeric value. Supported gauges, durations, and throughput must be
non-negative, and throughput must be finite. Validation ignores placeholder
values for unsupported measurements.

Worker heartbeats must not precede their reported start time. This prevents a
consumer from presenting an impossible lifecycle when clocks or adapter data
are malformed.

## Authenticated HTTP transport

The `managementhttp` package exposes configured status, record, and controller services
through these worker-side endpoints:

```text
GET /v1/status/workers?limit=N&cursor=OPAQUE
GET /v1/status/queues?limit=N&cursor=OPAQUE
GET /v1/records/failures?limit=N&sort=FIELD&direction=DIR
GET /v1/records/dead-letters?limit=N&sort=FIELD&direction=DIR
GET /v1/records/failures/{id}?visibility=hidden
GET /v1/records/dead-letters/{id}?visibility=hidden
POST /v1/commands
```

`NewHandler` requires a non-empty shared bearer token and at least one status,
record, or controller service. It registers only the configured service routes and
validates every request and adapter result before writing JSON. `NewClient`
implements `management.StatusReader`, `management.RecordReader`, and
`management.Controller`, validates
before network I/O, bounds response bytes, rejects unknown or trailing JSON,
and revalidates decoded pages and command results. Command request bodies are
strict JSON capped at 16 KiB, and acknowledgements must match both the command
ID and idempotency key. Transport and adapter errors cross the boundary only as
stable secret-safe errors.

Record lists require explicit bounded sorting and return hidden payloads only.
Inspection requires `hidden`, `redacted`, or `revealed` visibility. A backend
may return a more restrictive representation, never a more revealing one.
Revealed payloads remain capped at one mebibyte by the management contract.

The wire format uses explicit snake-case fields. Queue duration measurement
values are integer nanoseconds. Cursors remain opaque and must be forwarded
unchanged. Deploy TLS or mutually authenticated TLS around the handler; the
bearer token is admission defense, not transport encryption.

Configure `HandlerConfig.Controller` only with a data-plane implementation
that owns the backend semantics. The HTTP transport does not implement pause,
drain, retry, purge, replay, or any other queue operation itself.

## Control commands

`Command` is the stable envelope that a management-capable `queue` adapter
enforces. Every command carries a bounded command ID, idempotency key, actor,
reason, protocol version, action, target, request time, and deadline.

The allowed target matrix is intentionally narrow:

| Action | Targets |
| --- | --- |
| pause, resume | queue, worker group |
| drain, terminate | worker, worker group |
| retry, delete | failure, dead letter |
| bulk retry | failure collection, dead-letter collection |
| purge | queue, failure collection, dead-letter collection |
| replay | failure, dead letter |

Purge requires explicit confirmation. Replay requires confirmation, an
explicit destination, and either reject-duplicate or replace-duplicate
idempotency policy. Replay never claims exactly-once execution.

Bulk retry requires explicit confirmation and a selection limit of at most
1,000 records. The backend adapter owns deterministic selection and must not
exceed the requested bound.

`Controller` is implemented by a data-plane adapter that can enforce these
semantics honestly. A control plane calls that interface and must not open a
native Redis, Valkey, or other backend client to emulate the operation.

When a root `Queue` has `WithWorkerLifecycle`, it keeps pause, resume, drain,
and terminate enforcement and delegates record mutations to a worker that also
implements `Controller`. Valkey Streams supports retry, bounded bulk retry,
delete, and failure/dead-letter collection purge. Queue purge and replay remain
explicitly unsupported until their native safety and destination idempotency
semantics are proven.

## Command results

`CommandResult` reports acknowledged, rejected, failed, unsupported,
timed-out, partial, or unknown outcomes. Non-success outcomes use bounded,
stable failure codes. They must not contain raw adapter errors, payloads,
credentials, tokens, or backend endpoints.

An unknown outcome means enforcement may have happened but a reliable result
was unavailable. Callers must inspect durable command state and must not assume
that retrying is safe.

## Failures and dead letters

`RecordReader` exposes backend-neutral failure and dead-letter listing plus
single-record inspection. Page requests require a limit of at most 200,
bounded opaque cursors and searches, and an explicit supported sort and
direction. Returned pages validate their own size, cursor, and every nested
record so an adapter cannot bypass the boundary with oversized output.

`JobRecord` version 0 is the legacy compatibility envelope: bounded record,
backend, and queue identifiers, occurrence time, attempt count, failure code,
and payload visibility. A version-0 record must not contain version-1 fields.

Version 1 is selected with `management.CurrentEnvelopeVersion`. It adds the
original job and source record identifiers; topic, stream, routing key, and
consumer group; enqueue, first delivery, last delivery, dead-letter, and
retention times; retry policy; terminal classification; redacted failure
summary and separately controlled diagnostics; handler and job type; tags,
trace, and tenant identity; producer and worker versions; payload schema; and
bounded replay lineage. Dead-letter records require a dead-letter time.
Failure records may omit it because they have not reached a terminal state.

Backend metadata that cannot be measured is empty or `nil`; adapters must not
fabricate it. Known identifiers are at most 256 bytes, summaries at most 1,024
bytes, tags at most 32 bounded key/value pairs, and privileged payload or
diagnostic bytes at most one mebibyte. Times must follow enqueue, delivery,
dead-letter, and retention order. A replay generation requires original and
prior dead-letter identifiers. Unknown envelope versions fail validation.

Payload and diagnostic visibility default to hidden. A redacted representation
carries only metadata. Revealed bytes require an explicit privileged inspection
request and are capped at one mebibyte. Lists require both fields to remain
hidden. Adapters must reject bytes in hidden or redacted results, and the
control plane remains responsible for authenticating and authorizing privileged
access before requesting revealed content.

Stable management errors distinguish record-not-found, unsupported capability,
temporary unavailability, malformed cursor, invalid filter, stale record,
mutation conflict, partial mutation, and unknown mutation outcome. Adapters
wrap these sentinels so callers retain `errors.Is` behavior; arbitrary broker
errors do not cross the management boundary.
Redis Streams and Valkey Streams classify broker read failures as
`ErrManagementUnavailable` with secret-safe text while retaining the native
cause for `errors.Is`/`errors.As`. Caller cancellation and deadline expiration
remain cancellation errors rather than being mislabeled as backend outages.

`managementhttp` maps these errors to bounded problem codes and appropriate
400, 404, 409, 501, or 503 responses. Its client joins the matching management
sentinel with the legacy `managementhttp.ErrRemoteFailure`, preserving existing
transport checks while allowing callers to make a specific safe decision.
Unknown status bodies and unknown problem codes remain generic remote failures.

Retry, delete, replay, and confirmed purge continue through `Controller`; the
reader cannot mutate queue state.

## Failure isolation

Negotiation is pure and process-local. It cannot pause, resume, drain, retry,
acknowledge, reject, reclaim, purge, or replay a job. Workers continue normal
delivery when a control plane is unavailable. Data-plane enforcement and
backend-specific semantics remain owned by `queue` adapters.

Future management contracts will extend this package without moving backend
clients or queue serialization into the control plane.
