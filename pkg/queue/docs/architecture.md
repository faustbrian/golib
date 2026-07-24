# Architecture

The module has a small coordinator at its root and explicit backend packages.
It deliberately avoids a universal transport API that would erase correctness
differences.

## Components

- `Queue` owns concurrency, retries, cancellation, handler execution, metrics,
  observation, and final delivery settlement.
- `core.Worker` is the compatibility contract used by the coordinator.
- `job.Message` carries payload, timeout, retry policy, and optional settlement
  callbacks.
- `Ring` is the in-memory worker.
- Backend packages own connection, publish, receive, and transport-specific
  settlement behavior.
- `internal/streamqueue` owns only shared Streams command semantics, delivery
  envelopes, validation, group state, and transport-neutral transitions.
  `redisstream` converts those semantics through `go-redis/v9`; `valkeystream`
  converts them through `valkey-go`. Neither native client crosses the public
  queue API.
- `core.WorkerMetadata` enriches every lifecycle event with backend and logical
  queue identity without importing backend packages into the coordinator.
- Retry and settlement failure events carry stable classification and safe-code
  fields so exporters do not need to parse or label arbitrary error text.

The processing path is:

```text
producer -> Worker.Queue -> backend -> Worker.Request -> Queue.handle
         -> retry/backoff -> Ack on success | Nack on final failure
```

## Lifecycle

`Start` launches the scheduling loop. The loop requests work only when a worker
slot is available. Each handler runs with a timeout context. `Shutdown` prevents
new work, asks the backend worker to stop, and signals the scheduler. `Release`
also waits for owned goroutines.

The complete ownership and transition contract is in
[lifecycle and state ownership](lifecycle.md).

## Compatibility boundary

The root and backend option names remain close to upstream. Intentional v1
divergences are limited to error-returning constructors, correct post-handler
settlement, bounded backend waits, malformed-delivery rejection, usable metric
injection, and optional structured observation. See [migration.md](migration.md).

## Dependency boundary

All backends live in this module and share one version. Transport clients remain
external protocol implementations, but consumers no longer select separately
released `golang-queue` adapter modules.

Valkey topology is deliberately standalone-only. A generic client interface
combining Redis and Valkey command sets is prohibited because it would obscure
native errors, topology, command support, and lifecycle ownership.
