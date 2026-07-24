# clock

`clock` is a small, production-oriented clock foundation for Go 1.26 and
later. It keeps `time.Time` and `time.Duration` as public values, separates wall
time from elapsed time, and provides deterministic timers, tickers, sleeps, and
callbacks without changing the process-wide clock.

Use the standard `time` package directly when no dependency seam is needed. Use
`testing/synctest` when a complete test can live inside one fake-time bubble.
Use this module when business timestamps, explicit wall jumps, package
contracts, or selectively controlled time require dependency injection.

## Install

```sh
go get github.com/faustbrian/golib/pkg/clock@v1
```

The module has no runtime dependencies.

## Five-minute quickstarts

### System clock

Depend on only the capability an operation needs:

```go
func stamp(clock interface{ Now() time.Time }) time.Time {
    return clock.Now()
}

createdAt := stamp(clock.System{})
```

`System.Now` returns `time.Now()` unchanged, including its location and
process-local monotonic reading. `System.Sleep` owns and releases its timer when
the context is canceled.

### Fixed clock

```go
fixed := manual.NewFixed(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
fmt.Println(fixed.Now().Format(time.RFC3339))
// 2026-01-02T03:04:05Z
```

### Manual clock

```go
start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
manualClock, _ := manual.New(start)
timer, _ := manualClock.NewTimer(time.Minute)

waiter, _ := manualClock.Advance(time.Minute)
_, _ = waiter.Wait(context.Background())
fmt.Println((<-timer.C()).Format(time.RFC3339))
// 2026-01-02T03:05:05Z
```

Events fire by deadline and then registration order. A ticker has a one-value
buffer and drops backpressured ticks. Always stop resources that remain active,
and call `Shutdown` when the manual clock's owner is done.

### `testing/synctest`

```go
clocktest.SystemBubble(t, func(t *testing.T, system clock.System) {
    started := system.Now()
    require.NoError(t, system.Sleep(t.Context(), time.Hour))
    require.Equal(t, time.Hour, system.Since(started))
})
```

The helper delegates fake time and goroutine quiescence to the standard
library. It does not install another scheduler.

## Capability map

| Need | Interface |
| --- | --- |
| Business timestamp | `Clock` |
| Monotonic elapsed measurement | `ElapsedClock` |
| Cancelable bounded delay | `Sleeper` |
| Owned one-shot event | `TimerFactory` and `Timer` |
| Owned periodic event | `TickerFactory` and `Ticker` |
| Owned callback | `CallbackClock` and `Callback` |

`FullClock` is a convenience only. Libraries should accept the narrowest row
that meets their contract.

## Semantics at a glance

- `Advance` never accepts negative elapsed movement; use `Jump` for wall-clock
  rollback or forward correction.
- `Mark`, `SinceMark`, and `Measure` use manual monotonic progress and are not
  affected by `Jump`.
- Callbacks never run while an internal lock is held. They may create, stop, or
  reset work. A callback waiting for future work must issue and wait on a nested
  `Advance`; same-instant work wakes the active coordinator automatically.
- Callback panics are recovered by the manual clock and counted without keeping
  the payload. The system clock retains standard `time.AfterFunc` panic policy.
- Active objects and work per advancement are bounded. Invalid durations,
  overflow, closure, and exhausted budgets return documented errors.
- Observers receive bounded lifecycle metadata, never callback values, panic
  payloads, contexts, or timestamps.

## Documentation

- [API and ownership](docs/api.md)
- [State machines and ordering](docs/state-machines.md)
- [Deterministic concurrency](docs/concurrency.md)
- [Wall and monotonic time](docs/wall-and-monotonic.md)
- [`testing/synctest`](docs/synctest.md)
- [Observations](docs/observations.md)
- [Integration and migration](docs/integration.md)
- [Compatibility](docs/compatibility.md)
- [Security model](docs/security-model.md)
- [Performance](docs/performance.md)
- [Hardening evidence](docs/hardening.md)
- [FAQ](docs/faq.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Changelog](CHANGELOG.md)

## Local release gates

```sh
make install-tools
make check staticcheck lint nilaway vuln benchmark mutation
```

The exact commands are implemented by the Makefile. Production statement
coverage is required to be 100.0% for the root and `manual` packages. The
`clocktest` package is test infrastructure and is exercised separately.

## Scope

This module does not implement calendars, date-only values, timezone data,
interval algebra, cron, scheduling, distributed ordering, or a timestamp
oracle. `calendar`, `temporal`, `scheduler`, and `lease` own those
concerns.

## License

MIT. See [LICENSE](LICENSE).
