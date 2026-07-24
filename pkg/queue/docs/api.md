# Public API reference

Go package documentation remains authoritative for exact signatures. This page
maps the stable concepts new adopters need.

## Root package

- `NewQueue(options...) (*Queue, error)` creates a coordinator around a worker.
- `NewPool(size, options...) *Queue` creates an in-memory queue.
- `NewRing(options...) *Ring` creates an in-memory worker.
- `Queue.Start`, `Shutdown`, `Release`, and `Wait` control lifecycle.
- `Queue.Queue` submits byte-backed messages; `Queue.QueueTask` submits local
  functions.
- `WithWorkerCount`, `WithQueueSize`, `WithRetryInterval`, `WithLogger`,
  `WithMetric`, `WithObserver`, and `WithAfterFn` configure coordination.
- `Metric` exposes busy, submitted, success, failure, and completed counters.
- `Observer` receives `Event` values for lifecycle transitions.
- Failure events include only package-owned `Classification` and `FailureCode`
  dimensions suitable for bounded aggregation; `Err` remains for protected
  diagnostic handling and must not be used as a metric label.
- `core.WorkerMetadata` lets workers add backend and queue identity to every
  lifecycle event.

## Job package

- `job.Message` is the wire envelope.
- `job.AllowOption` configures retries, backoff, jitter, timeout, and optional
  operational metadata.
- `job.Metadata` carries optional original job identity, payload schema and
  content type, enqueue time, retry-policy identity, handler and job type,
  tags, trace, tenant, and producer version. Unknown fields remain empty.
- Metadata identities and tag keys/values are limited to 256 bytes, and each
  message is limited to 32 tags. Constructors clone metadata so later caller
  mutation cannot change the queued envelope.
- `Metadata.Validate` lets adapters reject untrusted metadata before storing or
  exposing it.
- `job.Int64`, `Float64`, `Time`, and `Bool` build pointer options.
- `job.DecodeE(data, maxBytes)` returns classifiable decode, size, and metadata
  errors. `Decode` remains a legacy panic wrapper.
- `job.DefaultMaxMessageBytes` is one mebibyte and `job.MaxRetryCount` is 100.
- `Message.Validate` checks execution metadata before enqueue or delivery.
- `Message.SetAcknowledgement` is intended for backend implementers.

## Backend constructors

Every backend provides `NewWorkerE(options...) (*Worker, error)` for production
startup and a compatibility `NewWorker(options...) *Worker` wrapper. New code
should use the error-returning form.

Backend-specific options are documented in [backend setup guides](backends/redis.md)
and in Go doc comments beside each option.

All network backends provide `WithRequestTimeout`. Redis Pub/Sub and Redis
Streams provide `WithConnectTimeout`; NATS and NSQ provide the same startup
bound, while RabbitMQ uses `WithReconnectConfig`. NSQ also provides
`WithTouchInterval`, `WithDeadLetter`, and `DecodeDeadLetter` for converting a
terminal envelope into a hidden-by-default `management.JobRecord`. RabbitMQ
provides `WithPublishTimeout`, `DeadLetterConfig`, and `WithDeadLetter` for
confirmed publish and terminal policy.

Redis Streams exposes `Worker.Stats(context.Context)`. Its result reports
consumer-group `Depth`, `Pending`, `Lag`, whether lag is known, and
`OldestJobAge`. `Depth` is `-1` when Redis reports indeterminate lag.
`WithRecordRetention` deliberately enables exact maximum-count retention for
failure and dead-letter streams; it is independent of source `WithMaxLength`.

Valkey Streams provides a package-owned API in `valkeystream`: `NewWorkerE`,
`NewWorker`, `Worker`, `Option`, `ConfigurationError`, and `Stats`. Connection
options cover address, ACL authentication, database, cloned TLS configuration,
client identity, dial/command/request/block/shutdown timeouts, and the bounded
blocking pool. Queue options cover stream, group, consumer, approximate source
length, exact record retention, read batch, reclaim policy, dead-letter policy,
logger, and handler.
No `valkey-go` request, response, option, error, or connection type appears in
these signatures.

`valkeystream.Worker.Stats(context.Context)` reports `Depth`, `Pending`, `Lag`,
`LagKnown`, `OldestPendingAge`, and monotonic local `Enqueued`, `Delivered`,
`Reclaimed`, `Retries`, `Acknowledged`, `DeadLettered`, and
`SettlementFailures` counters. See the [complete Valkey guide](backends/valkey-streams.md).

## Management package

- `management.NewFailure` preserves its cause for `errors.Is` and `errors.As`,
  but its `Error` string contains only the stable classification and safe code.
- `management.ResolveFailure` selects both classification and code across
  wrapped or joined errors. Safety precedence is infrastructure, canceled,
  malformed, permanent, then retryable; equal-rank codes use lexical order.
- `management.ProtocolVersion` and `management.ProtocolRange` define the
  rolling-upgrade compatibility window.
- `management.Capability` constants identify optional visibility and control
  operations without exposing a backend client.
- `CapabilityRetentionCount`, `CapabilityRetentionTime`, and
  `CapabilityRetentionBytes` report independently negotiable retention modes.
  Manual purge continues to use `CapabilityPurge`; absence means unsupported.
- `management.Negotiate` classifies older, newer, missing, and compatible
  workers and enables only capabilities both peers report.
- `management.Compatibility` preserves worker-only and control-plane-only
  capabilities for truthful diagnostic presentation.
- `management.WorkerStatus` reports bounded identity, version, lifecycle,
  queue, concurrency, drain, backend, protocol, and capability fields.
- `management.QueueStatus` exposes point-in-time queue metrics through
  `management.Measurement`, whose `Supported` field distinguishes an honest
  zero from an unavailable backend measurement. Supported gauges and rates
  are validated as finite, non-negative observations.
- `management.Command` validates protocol, actor, reason, idempotency,
  deadline, action/target compatibility, purge confirmation, and replay
  safeguards before an adapter may enforce it.
- `management.CommandResult` exposes acknowledged, rejected, failed,
  unsupported, timed-out, partial, and unknown outcomes through stable
  redacted failure codes.
- `management.Controller` is the backend-neutral interface implemented by
  `queue` adapters; control planes must not bypass it with native clients.
- `management.DesiredStateReconciler` applies authoritative queue, worker, and
  worker-group lifecycle revisions monotonically without owning a scheduler.
- `management.NewWorkerLifecycle` creates bounded queue admission and
  in-flight accounting for pause, resume, drain, and terminate enforcement.
- `queue.WithWorkerLifecycle` binds that lifecycle to a `Queue`. The worker
  must implement `management.StatusProvider`; the resulting queue implements
  `management.Controller`, `management.DesiredStateApplier`, and
  `management.StatusProvider`.
- `management.RecordReader` lists and inspects failures and dead letters using
  validated bounded pages, cursors, searches, sorts, and payload visibility.
- `management.Payload` defaults to hidden; redacted and privileged revealed
  forms reject unexpected or oversized data.
- `managementhttp.NewHandler` and `managementhttp.NewClient` provide a bounded,
  bearer-authenticated remote `management.StatusReader`,
  `management.RecordReader`, and `management.Controller` with an explicit
  snake-case wire contract.

See the [management protocol guide](management.md) for upgrade and failure
semantics.
