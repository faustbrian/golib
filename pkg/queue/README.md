# queue

`queue` is a consolidated worker queue with owned implementations for
in-memory, Redis Pub/Sub, Redis Streams, Valkey Streams, NATS, NSQ, and RabbitMQ. It preserves
the recognizable `golang-queue` programming model while owning correctness,
operations, and releases in one module.

## Status

The package is pre-v1 and undergoing hardening. Production code is held to
meaningful 100% coverage; durable delivery claims require backend-specific
integration evidence.

## Requirements

- Go 1.26.5 or later
- a supported broker for non-memory backends

## Installation

```sh
go get github.com/faustbrian/golib/pkg/queue
```

Backend packages ship in the same module and are imported explicitly.

## Quickstart

```go
worker, err := redisdb.NewWorkerE(
    redisdb.WithAddr("127.0.0.1:6379"),
    redisdb.WithChannel("jobs"),
    redisdb.WithRunFunc(func(ctx context.Context, task core.TaskMessage) error {
        return handle(ctx, task.Payload())
    }),
)
if err != nil {
    return err
}

q, err := queue.NewQueue(queue.WithWorker(worker), queue.WithWorkerCount(8))
if err != nil {
    return err
}
q.Start()
defer q.Release()
```

Redis Pub/Sub is low-latency and non-durable. Use Redis Streams or Valkey
Streams when work must remain pending until settlement. They are independent
native backends; adopting Valkey does not require removing Redis. Read
[delivery semantics](docs/delivery-semantics.md) before selecting a backend.

External control planes should depend on the backend-neutral contracts in
[`management`](docs/management.md). Incompatible workers remain visible, but
management capabilities are enabled only when both peers report support.
The [`managementhttp`](docs/management.md#authenticated-http-transport)
package makes those contracts remotely callable without exposing backend
clients.

## Package Guarantees

- explicit retry, acknowledgement, redelivery, cancellation, and shutdown
  behavior
- safe failure classification and codes that preserve `errors.Is` while
  redacting arbitrary handler, panic, and settlement text
- durable Redis Streams, Valkey Streams, NSQ, and RabbitMQ paths with explicit settlement
- observable lifecycle events, metrics, and backend identity
- stable management-protocol version and capability negotiation for external
  control planes
- bounded worker and queue status contracts that distinguish unsupported
  backend measurements from measured zero values, with paginated readers
- bounded authenticated HTTP transport for remote status, records, and control
  commands
- backend-neutral command enforcement contracts with explicit confirmation,
  bounded bulk retry, acknowledgement, timeout, partial, and unknown outcomes
- revisioned desired-state reconciliation with monotonic per-target application,
  retry-safe failures, and caller-owned scheduling
- queue-owned pause, resume, drain, and terminate enforcement that stops
  admission at safe boundaries and reports in-flight work honestly
- bounded failed-job and dead-letter inspection with payloads hidden by
  default and privileged content capped at one mebibyte
- one module and release unit for all maintained backends
- backend-specific guarantees documented without abstraction leakage

## Documentation

Start with the [documentation index](docs/README.md), [quickstart](docs/quickstart.md),
[adoption guide](docs/adoption.md), and [API reference](docs/api.md). Review the
[backend matrix](docs/backend-support.md), [failure model](docs/failure-model.md),
and [integration evidence](docs/integration-evidence.md) before production use.
Valkey adopters should use the [Valkey 9 Streams guide](docs/backends/valkey-streams.md)
and [runnable example](examples/valkey).

AI tools can use [llms.txt](llms.txt) and [llms-full.txt](llms-full.txt).
Release history is maintained in [CHANGELOG.md](CHANGELOG.md).

## Development

Run `make check` before submitting a change. Backend changes must also pass
`make integration` with the services documented in
[CONTRIBUTING.md](CONTRIBUTING.md).

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) and follow the
[code of conduct](CODE_OF_CONDUCT.md). Every backend change must document its
delivery and settlement impact.

## Security

Report vulnerabilities privately according to [SECURITY.md](SECURITY.md).
Review [docs/security.md](docs/security.md) before processing untrusted jobs.

## License

`queue` is available under the [MIT License](LICENSE). Fork provenance and
third-party attribution are recorded in [NOTICE](NOTICE) and
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
