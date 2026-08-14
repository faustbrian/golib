# scheduler

`scheduler` is a code-defined application scheduler for Go services running
on Kubernetes. Multiple scheduler replicas coordinate through fenced leases,
while durable business work is dispatched to `queue` workers.

The module is pre-v1. It does not claim exactly-once execution: leases reduce
duplicate dispatch, and jobs must remain idempotent.

## Requirements

- Go 1.26.6 or later
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

Laravel-style frequency helpers are available as interval constructors and
recurring constraints are schedule options:

```go
schedule, err := scheduler.NewSchedule(
    "weekday-sync",
    "accounts.sync",
    scheduler.EveryTenMinutes(),
    scheduler.WithWeekdays(),
    scheduler.WithBetween("8:00", "17:00"),
    scheduler.WithTimezone("America/Chicago"),
)
```

Laravel-compatible execution controls compose with those options:

```go
schedule, err := scheduler.NewSchedule(
    "weekday-sync",
    "accounts.sync",
    scheduler.Hourly(),
    scheduler.WithWeekdays(),
    scheduler.WithBetween("8:00", "17:00"),
    scheduler.WithTimezone("America/Chicago"),
    scheduler.WithoutOverlapping(10),
    scheduler.OnOneServer(),
    scheduler.RunInBackground(),
)
```

`WithoutOverlapping()` defaults to 1,440 minutes, while `OnOneServer()` uses an
independent one-hour occurrence lease. The `lease.Store` supplied to
`NewRunner` is the explicit equivalent of Laravel's `useCache`; all replicas
must receive the same PostgreSQL or Valkey store. Use CLI `clear-cache` only
after isolating any old executor that may still be performing side effects.

Custom cron expressions accept five fields or an optional leading seconds
field. See the [API reference](docs/api.md#frequency-and-constraints) for every
frequency helper and its Laravel mapping.

Applications own pause and resume triggers instead of invoking scheduler
commands. `PauseState` is suitable for one process; multi-replica deployments
should supply a shared persistent implementation of the narrow interfaces:

```go
pause := scheduler.NewPauseState()
runner, err := scheduler.NewRunner(
    registry,
    leases,
    executor,
    scheduler.WithOwner(podName),
    scheduler.WithPauseSource(pause),
)

// An authenticated endpoint, backpressure controller, or application command
// may call these idempotently.
_ = pause.Pause(ctx)
_ = pause.Resume(ctx)
```

Use `EvenWhenPaused()` only for operational schedules that must keep running.
Cancel the context passed to `Run`, then call `Drain`, to implement an external
deployment interrupt. `Registry.Overview(after)` provides deterministic list
data, including next runs, for any caller-owned CLI, HTTP, or admin surface.

## Packages

- root: definitions, immutable registry, occurrences, runner, hooks, and events
- `cron`: parser integration and explicit IANA time-zone compilation
- `lease`, `memory`, `postgres`, `valkey`: fenced ownership contracts and stores
- `queue`: `queue` occurrence envelopes
- `idempotency`: optional `idempotency` dispatch guard
- `schedulerhttp`, `schedulercli`: inspection and fenced recovery controls
- `schedulerservice`: `service` lifecycle, drain ordering, and scheduled-work
  correlation composition
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

## Ecosystem

Use the [Golib documentation portal](https://github.com/faustbrian/golib/blob/main/docs/index.md)
to choose companion packages, supported stacks, recipes, and operations guidance.
