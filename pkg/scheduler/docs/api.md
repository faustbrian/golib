# API reference

## Definitions and registry

`NewSchedule(name, task, interval, options...)` creates a validated immutable
definition. `Compile` validates cron expressions and IANA time zones, rejects
duplicate names, and returns an immutable `Registry`. Registry methods return
copies and provide `Schedules`, `Next`, and bounded `Due` calculations.

The `cron` package exposes the same five-field, optional-seconds, last-day, and
descriptor compiler used by the registry. It returns a small `Schedule`
interface and typed expression and time-zone errors without exposing the
underlying parser type. Future-boundary search covers a complete 400-year
Gregorian cycle, including the eight-year leap-day gap across non-leap
centuries such as 2100.

Schedule options cover version, timezone, parameters, environments, date
bounds, enablement, maintenance, deterministic jitter, missed-run policy,
overlap policy, one-server ownership, conditions, hooks, metadata, and runtime
deadline. Version, expression, timezone, parameters, and jitter participate in
revision identity. `CoordinationID` is stable across version and timing changes
for the same name, task, and parameters so rolling replicas share occurrence,
overlap, and idempotency keys.

## Frequency and constraints

`Cron` accepts standard five-field expressions, six-field expressions with a
leading seconds field, `L` as the day of month, and supported descriptors.
Convenience intervals cover the corresponding Laravel frequencies:

- seconds: `EverySecond`, `EveryTwoSeconds`, `EveryFiveSeconds`,
  `EveryTenSeconds`, `EveryFifteenSeconds`, `EveryTwentySeconds`, and
  `EveryThirtySeconds`;
- minutes: `EveryMinute`, `EveryTwoMinutes`, `EveryThreeMinutes`,
  `EveryFourMinutes`, `EveryFiveMinutes`, `EveryTenMinutes`,
  `EveryFifteenMinutes`, and `EveryThirtyMinutes`;
- hours: `Hourly`, `HourlyAt`, `EveryOddHour`, `EveryTwoHours`,
  `EveryThreeHours`, `EveryFourHours`, and `EverySixHours`;
- calendar periods: `Daily`, `DailyAt`, `At`, `TwiceDaily`, `TwiceDailyAt`,
  `DaysOfMonth`, `Weekly`, `WeeklyOn`, `Monthly`, `MonthlyOn`,
  `TwiceMonthly`, `LastDayOfMonth`, `Quarterly`, `QuarterlyOn`, `Yearly`, and
  `YearlyOn`.

The hour-frequency helpers accept zero or one minute argument; omitting it uses
minute zero. Local time strings use `H:MM` or `HH:MM`. `WeeklyOn` uses
`time.Weekday`, and `YearlyOn` uses `time.Month`.

Recurring constraints are applied to compiled boundaries before missed-run
policy selection. Use `WithWeekdays`, `WithWeekends`, the seven named weekday
options, or `WithDays` for weekday limits. Use `WithBetween` and
`WithUnlessBetween` for inclusive recurring local-time windows, including
windows that cross midnight. The schedule's `WithTimezone` value controls both
calendar frequencies and recurring constraints.

`WithCondition` implements Laravel's `when`; multiple conditions must all
allow the occurrence. `WithSkip` is its inverse. `WithEnvironments` restricts
the definition, and the runner selects its current environment with
`WithEnvironment`.

## Runner

`NewRunner` requires a registry, `lease.Store`, `Executor`, and owner name.
`Run` sleeps until the exact next occurrence through an injectable `Clock`.
`RunFrom` first applies each schedule's bounded missed-run policy strictly
after a caller-supplied non-zero cursor, then continues the same schedule loop
without a startup gap.
`Tick` exposes deterministic range processing. `Drain` rejects new ticks and
waits for in-flight decisions, managed executions, and callbacks until its
context ends.

`Executor` is the only work boundary. Use `queue.Dispatcher` for durable work.
`RunTimeout` bounds how long a tick waits even when an in-process executor
ignores cancellation. Such an executor remains tracked, retains its overlap
lease, and occupies one of 128 execution slots until it returns. Configure the
bound with `WithMaxConcurrentExecutions`; capacity exhaustion returns
`ErrExecutionCapacity`. Go cannot forcibly terminate arbitrary application
code, so in-process executors remain suitable only for short operational work.

Every lease backend call has a five-second default deadline, configurable with
`WithLeaseOperationTimeout`. Conditions, hooks, and observers have a one-second
default deadline and share 128 managed slots. Configure these with
`WithCallbackTimeout` and `WithMaxConcurrentCallbacks`. A condition timeout or
capacity failure fails the occurrence. Lifecycle hooks and observers are
best-effort once callback capacity is exhausted.

## Service lifecycle

`schedulerservice.New` constructs a concrete correlation-aware runner from a
caller-owned registry, lease store, executor, and runner options. `Plan`
returns one long-running task plus a final drain component. Under `service`,
task cancellation stops new scheduling, `Runner.Drain` joins retained
executions, and supplied facility components close afterward.

Every occurrence starts a new workflow through `correlation/schedule` by
default. `CorrelationTrustedMetadata` explicitly continues trusted correlation
fields embedded in application-owned schedule metadata. See
[service lifecycle integration](service-integration.md) for ownership,
readiness, failure, and repeated-shutdown semantics.

## Ownership

`lease.Store` defines `Acquire`, `Heartbeat`, `Release`, `Inspect`, `Recover`,
and `Capabilities`. Every successful takeover receives a larger fencing token.
Completion-sensitive downstream writes must reject tokens lower than the
largest token already observed.

`postgres.New` and `postgres.SchemaMigration` retain the default
`public.scheduler_leases` table. Services that own a dedicated PostgreSQL
schema use `postgres.NewWithSchema` and `postgres.SchemaMigrationFor`; the
schema must already exist and its lower-case identifier is validated before
SQL is generated or a store is constructed.

Overlap leases are renewed every third of their TTL by default while execution
is active. `WithHeartbeatInterval` selects an explicit positive cadence and
runner construction rejects an interval at or beyond any overlap lease TTL.
The runner rejects overlap schedules when the store does not advertise
heartbeat support. A heartbeat failure cancels the execution context and is
reported as an execution failure.

## Events

Runner events are `before`, `success`, `failure`, `skipped`, `overlap`, and
`completed`. Per-schedule hooks and global observers are panic-contained and
bounded by the runner callback deadline and capacity.
`history.Buffer` is a bounded observer. `telemetry.Observer` emits structured
logs, metrics, and spans.

## Control surfaces

The HTTP handler supports schedule list, registry validation, next, due,
boundary testing, and fenced recovery under `/v1`. The CLI supports `list`,
`validate`, `next`, `due`, `test`, and `unlock`/`recover`. Neither surface
executes arbitrary commands.
