# scheduler

`scheduler` is a code-defined application scheduler for Go services running
on Kubernetes. Multiple scheduler replicas coordinate through fenced leases,
while durable business work is dispatched to `queue` workers.

The module is pre-v1. It does not claim exactly-once execution: leases reduce
duplicate dispatch, and jobs must remain idempotent.

## Requirements

- Go 1.26.5 or later
- PostgreSQL or Valkey 9 for multi-replica deployments
- `queue` with a durable backend for long-running business work

## Five-minute quickstart

```go
schedule, err := scheduler.NewSchedule(
    "nightly-report",
    "reports.generate",
    scheduler.Daily(),
    scheduler.WithTimezone("Europe/Helsinki"),
    scheduler.WithOneServer(5*time.Minute),
)
if err != nil {
    return err
}

registry, err := scheduler.Compile(schedule)
if err != nil {
    return err
}

dispatcher, err := schedulerqueue.New(durableQueue)
if err != nil {
    return err
}

runner, err := scheduler.NewRunner(
    registry,
    postgresLeases,
    dispatcher,
    scheduler.WithOwner(podName),
)
if err != nil {
    return err
}

return runner.Run(ctx)
```

Compile the immutable registry during startup so invalid expressions, duplicate
names, and unavailable time zones fail before the pod becomes ready. On
shutdown, cancel `Run` and call `Drain` with a deadline.

## Packages

- root: definitions, immutable registry, occurrences, runner, hooks, and events
- `cron`: parser integration and explicit IANA time-zone compilation
- `lease`, `memory`, `postgres`, `valkey`: fenced ownership contracts and stores
- `queue`: `queue` occurrence envelopes
- `idempotency`: optional `idempotency` dispatch guard
- `schedulerhttp`, `schedulercli`: inspection and fenced recovery controls
- `history`: bounded operational event history
- `telemetry`: `log` compatible structured logging and `telemetry`
- `schedulertest`: deterministic fake clock

## Documentation

Start with the [documentation index](docs/README.md), [API reference](docs/api.md),
[Laravel migration guide](docs/laravel-migration.md), and
[Kubernetes architecture](docs/kubernetes.md). Release history is in
[CHANGELOG.md](CHANGELOG.md). Compileable integrations are in
[examples](examples/README.md).

Security vulnerabilities should be reported through the private process in
[SECURITY.md](SECURITY.md). The project is available under the [MIT License](LICENSE).

## Development

Run `make check`. PostgreSQL and Valkey conformance require the environment
variables described in [CONTRIBUTING.md](CONTRIBUTING.md).
