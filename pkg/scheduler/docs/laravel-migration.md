# Laravel scheduler migration

Treat migration as a behavior inventory, not a syntax translation. Record each
Laravel event's command or closure, parameters, time zone, filters, maintenance
policy, mutex name and TTL, background behavior, output handling, and hooks.
Run old and new schedulers against non-production durable backends before a
rolling cutover.

## API mapping

| Laravel | scheduler | Intentional difference or action |
|---|---|---|
| named closure or command | `NewSchedule(name, task, interval)` | stable name and task are required; no anonymous production identity |
| `cron` | `Cron` | accepts five fields, an optional leading seconds field, descriptors, and `L` day-of-month |
| `everySecond` through `everyThirtySeconds` | matching `Every*Seconds` helper | wall-clock aligned seconds |
| `everyMinute` through `everyThirtyMinutes` | matching `Every*Minutes` helper | wall-clock aligned minutes |
| `hourly`, `hourlyAt` | `Hourly`, `HourlyAt` | explicit interval constructors |
| odd, two, three, four, or six hourly | matching `Every*Hour(s)` helper | optional minute argument defaults to zero |
| `daily`, `dailyAt`, `at` | `Daily`, `DailyAt`, `At` | local time uses `H:MM` or `HH:MM` |
| `twiceDaily`, `twiceDailyAt`, `daysOfMonth` | matching helper | integer hours, minutes, and month days |
| `weekly`, `weeklyOn` | `Weekly`, `WeeklyOn` | use `time.Weekday` |
| monthly helpers, including last day | matching `Monthly*`, `TwiceMonthly`, `LastDayOfMonth` helper | uses the calendar's actual last day |
| quarterly and yearly helpers | matching `Quarterly*` and `Yearly*` helper | `YearlyOn` uses `time.Month` |
| `timezone` | `WithTimezone` | IANA zone is compiled at startup; folds are physical instants |
| `weekdays`, `weekends`, or named weekdays | matching `With*days` option | filters compiled boundaries before missed-run selection |
| `days` | `WithDays` | accepts one or more `time.Weekday` values |
| `between`, `unlessBetween` | `WithBetween`, `WithUnlessBetween` | inclusive local-time windows; overnight windows are supported |
| `when` | `WithCondition` | bounded to 32 and a runner callback deadline |
| `skip` | `WithSkip` | inverts the supplied trusted condition |
| `environments` | `WithEnvironments` | runner environment is explicit configuration |
| `evenInMaintenanceMode` | `MaintenanceRun` | runner maintenance mode is supplied by the application |
| `onOneServer` | `OnOneServer()` | fenced one-hour occurrence lease; `WithOneServer(ttl)` customizes the TTL |
| `withoutOverlapping` | `WithoutOverlapping(minutes...)` | skip policy, renewable task lease, 1,440-minute default; `WithoutOverlap` exposes other policies |
| `schedule:clear-cache` | CLI `clear-cache` or `Registry.ClearCache` | bulk-clears configured overlap leases with observed fencing tokens; isolate old owners first |
| `runInBackground` | `RunInBackground()` | later due tasks start without waiting; execution remains managed and is joined by `Drain` |
| `schedule:pause`, `schedule:continue` | application trigger using `PauseController` | the application decides whether the trigger is a command, authenticated endpoint, or backpressure controller |
| `evenWhenPaused` | `EvenWhenPaused()` | bypasses pause lookup for the selected operational schedule |
| queued job or command | `queue.Dispatcher` envelope task and parameters | workers own business execution, routing, and retry policy; queue name and connection do not belong to schedule timing |
| `before`, success, failure, `after` | `WithHooks` | `After` only follows a started executor; all hooks are panic-contained and deadline-bounded |
| `pingBefore`, `pingAfter` | application hook or telemetry | core does not make network callbacks |
| output append, email, or storage | worker-owned output policy | core captures no command output |
| `schedule:list` | `Registry.Overview(after)` | caller supplies the reference instant and chooses the CLI, HTTP, or admin trigger |
| `schedule:test` | CLI `test` | calculates boundaries; control surfaces execute no shell command |
| `schedule:interrupt` | cancel `Run` context, then `Drain` | caller owns the external trigger and shutdown deadline |
| schedule groups | Go construction helpers | group defaults are application code, not mutable runtime state; no first-class group API |
| `useCache` / custom cache store | `lease.Store` passed to `NewRunner` | explicit dependency injection; every replica must share one PostgreSQL or Valkey backend and namespace |

## Identity and rollout

Every schedule must have a stable name and task identity. Parameterized
schedules should use `WithParameters`; canonical JSON produces stable parameter
identity. Bump `WithVersion` for semantic task changes even when cron is
unchanged. Version and timing affect revision identity but not coordination
identity, so matching physical occurrences remain deduplicated during rollout.

Changing the name, task, or parameters creates a new coordination identity.
Changing cron, time zone, or jitter can create distinct old and new physical
boundaries even though matching instants still deduplicate. Use the
[rolling deployment matrix](hardening.md#rolling-deployment-matrix) to choose a
drain, feature gate, or staged activation.

## Execution and overlap

Laravel commands often perform work in the scheduler process. Prefer a
`queue.Dispatcher` backed by durable `queue` storage. A successful dispatch
does not mean the job completed, and a lease does not remove the need for job
idempotency. Copy the occurrence key and fencing token into every durable
envelope and downstream protected write.

Do not copy Laravel's per-event queue name and connection fields into the
schedule definition. A Go executor or queue adapter can route by task identity
when an application has that requirement; until then, `queue.Dispatcher`
keeps timing separate from durable delivery configuration.

Pause control follows the same ownership rule. The runner depends only on
`PauseSource`, while application control surfaces depend on `PauseController`.
`PauseState` supplies both for one process. Multi-replica deployments must use
a shared persistent implementation so every runner observes the same state.
Pause and resume are idempotent, pause state has no TTL, lookup failure fails
closed, and paused occurrences emit `EventSkipped` with `ErrPaused`.

`RunInBackground` changes scheduler ordering, not execution ownership. The
runner continues processing later due tasks, tracks the background execution
under its configured timeout and capacity, emits its eventual lifecycle
result, and waits for it during `Drain`. Because `Tick` has already returned,
an asynchronous failure is observable through hooks and observers rather than
that `Tick` result. Prefer durable dispatch for long-running business work.

`EventFinished` and the `After` hook identify the narrower boundary where a
started executor has returned. They follow success or failure and precede
`EventCompleted`; skips and pre-execution failures do not emit them.
`Event.Background` supplies background metadata without inventing a second
background-only completion event. Output remains executor-owned.

`WithoutOverlapping()` and `OnOneServer()` retain independent expiration
values when combined. Stable names are mandatory for every schedule, and task
parameters participate in coordination identity, so parameterized one-server
schedules cannot silently share an anonymous mutex.

`OverlapReplace` requires a `lease.ReplacementStore`. Its `Replace` operation
must cancel the prior owner, transfer ownership atomically, and fence
downstream effects before returning. Built-in stores intentionally do not
claim that capability. Do not translate `withoutOverlapping` into forced
unlock during normal execution.

## Migration verification matrix

For every migrated schedule, record and test:

| Field | Required comparison |
|---|---|
| boundary corpus | normal day, DST gap, DST fold, month end, leap day |
| delayed tick | skip, newest-once, or bounded catch-up intent |
| identity | old mutex name versus new name, task, and parameters |
| maintenance and environment | allowed and rejected executions |
| overlap | active owner, expiry, heartbeat, stale release, manual recovery |
| dispatch | queue durability, occurrence deduplication, retry and output owner |
| crash | before dispatch, after queue submission, after completion |
| rollout | old-only, mixed old/new, new-only, rollback |

Do not remove the Laravel schedule until mixed-version evidence shows no silent
double-run and no lost intended boundary. Keep application-specific deviations
beside this matrix in the migration change set.
