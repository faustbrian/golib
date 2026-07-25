# SQLSTATE and error classification

| Classification | SQLSTATE or Go error | Retry advice |
| --- | --- | --- |
| unique violation | `23505` | fix input or idempotency decision |
| foreign-key violation | `23503` | fix ordering or input |
| check violation | `23514` | fix input |
| exclusion violation | `23P01` | resolve conflicting range/resource |
| serialization failure | `40001` | may retry a side-effect-safe transaction |
| deadlock | `40P01` | may retry; also fix lock ordering |
| timeout | `context.DeadlineExceeded` | policy dependent |
| cancellation | `context.Canceled` | normally do not retry blindly |
| query canceled | `57014` | inspect operation context; may be cancellation or `statement_timeout` |
| lock unavailable | `55P03` | inspect operation policy; may be `NOWAIT` or `lock_timeout` |
| connectivity | SQLSTATE class `08`; `57P01`, `57P02`, `57P03`, `53300`; network errors | retry only with explicit safe-before-send evidence |
| pool exhaustion | `ErrPoolExhausted` | shed load or fix saturation cause |

`Classify` uses `errors.As` and therefore works through wrapping and joined
errors. It returns the original error graph and native `*pgconn.PgError` rather
than replacing either. Constraint, schema, table, column, severity, detail, and
hint remain available.

`IsRetryable` is true for serialization failures and deadlocks, or when pgx's
`pgconn.SafeToRetry` guarantees a failure occurred before sending data. A
generic connectivity error is not retryable because commit success may be
unknown. Retrying a transaction still requires application proof that its
closure has no escaped side effects.

Only SQLSTATE and explicitly bounded classification fields are safe telemetry
defaults. PostgreSQL `Message`, `Detail`, and `Hint` can contain data values.
Constraint and table names may also reveal internal schema design; apply the
application's disclosure policy before returning or logging them.
