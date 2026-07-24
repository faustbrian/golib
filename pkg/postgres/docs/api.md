# API reference

The authoritative signature reference is `go doc`; this page describes the
stability and behavior of each public surface.

## Configuration

- `Config` owns DSN, finite timeouts, pool sizes, connection lifetimes, startup
  policy, typed TLS override, per-connection initialization, a native
  `*pgxpool.Config` hook, and an optional bounded observer.
- `Config.Configure` is a trusted native extension boundary. Returned errors
  are preserved safely; panics propagate and must not represent expected hook
  rejection.
- `ParseConfig` returns the native `*pgxpool.Config`. It catches malformed-input
  parser panics and never includes the DSN in its own error string.
- `ConfigError` identifies a field. `Unwrap` exposes only safe hook causes; DSN
  parser causes are deliberately withheld because upstream text may change.
- `PoolConfig` is an alias, not a wrapper, for `pgxpool.Config`.

## Pool and health

- `New` returns `*Pool`; startup ping is the default.
- `Raw` returns the exact native `*pgxpool.Pool`.
- `Acquire`, `Ping`, and `Close` honor the earlier caller or configured
  deadline. `Close` starts native shutdown once and may return while shutdown
  continues until borrowed connections are returned.
- `Readiness` contacts PostgreSQL. `Liveness` only reports whether shutdown has
  begun. `Stats` copies native pgxpool counters and gauges.

## Transactions

- `Beginner` matches pgx values with `BeginTx`.
- `TransactionOptions` embeds `pgx.TxOptions` and adds cleanup timeout and
  observation.
- `RunTransaction` invokes the closure once, never retries it, commits success,
  rolls back error, panic, or goroutine termination with an uncanceled bounded
  cleanup context, joins callback and rollback failures, and re-panics the
  original panic value.
- `RunSavepoint` uses pgx pseudo-nested transactions, which issue explicit
  savepoint SQL. `RunSavepointWithOptions` adds cleanup and observation options.

## Errors

- `Classify` returns `ErrorInfo` while preserving the exact input in `Cause`
  and the native `*pgconn.PgError` in `Postgres`.
- `SQLState`, `IsKind`, constraint predicates, context timeout/cancellation,
  ambiguous server query/lock predicates, connectivity, pool exhaustion, and
  `IsRetryable` are inspection helpers. `IsRetryable` is advice only, requires
  safe-before-send evidence for connectivity, and never executes work.
- `Detail` and `Hint` may contain values and are unsafe to log by default.

## Observability

- `Observer`, `ObserverFunc`, and `Observation` carry fixed operation, outcome,
  duration, SQLSTATE, classification, and optional pool gauges only.
- `NewSlogObserver` emits those bounded fields through standard `slog`.
- `otelpostgres.New` builds standard OpenTelemetry duration, count, and pool
  connection instruments.

## Testing

- `postgrestest.Start` owns a real PostgreSQL container, waits for readiness,
  runs an optional setup hook once, and exposes the DSN and native container.
  Setup error, panic, and goroutine-termination paths perform bounded cleanup,
  preserving the original error or panic even if cleanup itself panics.
  `HostPort` supports stable-endpoint
  stop/restart tests.
- `Database.Close` bounds termination, permits retry after a failed attempt,
  and becomes idempotent after successful termination.
- `RunIsolated` executes a test callback once in a real pgx transaction that
  always rolls back with bounded cleanup. Callback and rollback errors remain
  inspectable, panic values are preserved, and `testing.T.FailNow` cannot skip
  rollback. A cleanup panic cannot replace a returned callback error or a
  terminal callback path; after a successful callback it propagates rather
  than falsely reporting successful isolation.
