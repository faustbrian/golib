# Transactions and savepoints

`RunTransaction` accepts any native pgx `BeginTx` implementation and embeds
`pgx.TxOptions`, including isolation, access mode, and deferrability.

| Dimension | Native pgx values | PostgreSQL behavior |
| --- | --- | --- |
| isolation | `ReadUncommitted`, `ReadCommitted`, `RepeatableRead`, `Serializable` | PostgreSQL accepts all four; read-uncommitted behavior is read committed |
| access | `ReadWrite`, `ReadOnly` | read-only rejects writes to non-temporary tables |
| deferrability | `NotDeferrable`, `Deferrable` | deferrable affects only serializable read-only transactions |

The integration suite crosses all four isolation levels with both access modes
and both deferrability modes on every supported PostgreSQL major. Native
`BeginQuery` and `CommitQuery` remain advanced pgx escape hatches owned by the
caller; this package does not rewrite them.

| Path | Finalization |
| --- | --- |
| callback returns nil | commit once |
| callback returns error | rollback once; join rollback failure |
| callback panics | rollback once; re-panic original value |
| callback terminates its goroutine | rollback once; observe `aborted` |
| callback context canceled and returns error | rollback with a bounded context derived using `context.WithoutCancel` |
| begin fails | callback is not invoked |
| commit returns an error | preserve commit error; pgx defines the transaction as closed or failed |
| commit panics | attempt bounded rollback once; re-panic original value |

On callback panic or goroutine termination, rollback is still attempted once.
A panic from cleanup cannot replace a callback error or panic, or convert
`runtime.Goexit` into a different panic. Returned callback errors continue to
join ordinary rollback errors so both remain inspectable. A panic raised by
trusted native code during commit or savepoint release also triggers bounded
rollback before the original panic propagates.

The closure runs exactly once. The package never retries it because HTTP calls,
messages, file writes, or other external effects cannot be rolled back with
PostgreSQL. Use `IsSerializationFailure`, `IsDeadlock`, and `IsRetryable` to
drive an application-owned retry around a closure proven to contain only safe
effects.

`RunSavepoint` calls `pgx.Tx.Begin`, which creates a pseudo-nested transaction
using `SAVEPOINT`, `RELEASE SAVEPOINT`, and `ROLLBACK TO SAVEPOINT`.
`RunSavepointWithOptions` adds bounded observation without changing those
semantics. A savepoint can recover part of a transaction, but it does not
create independent commit durability and cannot undo external effects.
Real PostgreSQL evidence nests two levels, rolls back the inner level, releases
the outer level, and verifies only the outer write persists with its parent
transaction.

Keep transactions short. Never wait for user input or remote services while
holding locks. Always pass the operation context to every pgx call.
