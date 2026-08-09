# Changelog

All notable changes follow Keep a Changelog. This module uses semantic
versioning once released.

## Unreleased

### Changed

- Publish the service lifecycle adapter as an independently versioned optional
  module so core queue consumers do not inherit service or correlation runtime
  dependencies.
- Classify producer results as accepted, not accepted, or unknown so canceled
  and ambiguous publishes cannot be retried as if they were equivalent.
- Share and cache owned-resource cleanup results so repeated and concurrent
  stop calls never close the resource more than once.
- Close producer and typed-worker admission during service drain so readiness
  and new intake become unavailable before supervised work is canceled.
- Close concrete `*queue.Queue` admission during service drain without
  releasing its backend before admitted handlers settle.
- Classify contained concrete queue release panics as secret-safe shutdown
  callback panics with a deterministic terminal result.
- Preserve callback error causes behind operation-specific diagnostics that do
  not disclose backend endpoints, credentials, or task payloads.
- Report an unexpected successful worker-run exit as a supervised failure while
  treating exit after cancellation as normal shutdown.
- Reject concurrent or repeated typed-worker run calls so exactly one backend
  intake loop owns deliveries and settlement.

### Added

- Correlation-aware producer, delivery-handler, and worker lifecycle adapters
  for the owned queue and service modules.
- Hostile-input fuzz and deterministic lifecycle coverage proving transport
  metadata never aliases caller-owned state and asynchronous failures cannot
  hang the verification suite.
- Optional producer startup and readiness callbacks with rollback-safe owned
  cleanup and secret-safe panic recovery.
- Typed supervised worker plans that bind startup, readiness, run, handler,
  drain, and shutdown callbacks to one stable service identity.
- Service drain admission hooks that reject new producer and worker work before
  shutdown cancellation begins.
- Module lifecycle, Kubernetes, scaling, duplicate-window, backend, adoption,
  migration, security, and FAQ documentation plus a package-scoped backend
  integration gate.
- An explicit lifecycle transition model and adversarial drain campaign that
  races readiness, cancellation, duplicate signals, and shutdown ownership.
- Lifecycle-aware HTTP liveness and readiness coverage during concurrent
  worker drain, cancellation, repeated signals, and backend failure.
- Independent producer, handler, and typed-worker adapter benchmarks that keep
  broker and application work outside the measured lifecycle overhead.
- Redis Streams and Valkey Streams composition coverage through the concrete
  queue worker adapter before the full durable-backend recovery suite runs.
- Abrupt process-termination coverage before handler effects, after effects,
  and after settlement, including expired-lease recovery by competing workers.
- Real Redis Streams and Valkey Streams adapter coverage for disconnect and
  reconnect, handler timeout, lease-expiry redelivery, dead-letter outage,
  shutdown, scale-up/down, and rolling replacement without lost settlement.
- An isolated Kind end-to-end gate that force-deletes Redis- and Valkey-backed
  worker pods at each effect/settlement boundary and verifies competing pod
  recovery against durable backend state.
