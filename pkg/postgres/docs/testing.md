# Testing with Testcontainers

Use `postgrestest` when behavior depends on SQLSTATE, isolation, locking,
constraints, cancellation, session state, pool saturation, or connection loss.
A fake is not equivalent evidence.

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
defer cancel()

database, err := postgrestest.Start(ctx, postgrestest.Config{
    Image: "postgres:18-alpine",
    Setup: func(ctx context.Context, dsn string) error {
        pool, err := pgxpool.New(ctx, dsn)
        if err != nil {
            return err
        }
        defer pool.Close()
        _, err = pool.Exec(ctx, schemaSQL)
        return err
    },
})
if err != nil {
    t.Fatal(err)
}
t.Cleanup(func() { _ = database.Close(context.Background()) })
```

The repository integration suite selects its image with `POSTGRES_VERSION` and
CI runs 14 through 18. Keep tests deterministic: use isolated tables or
databases, explicit timeouts, channel synchronization for contention, and
server-side primitives rather than arbitrary sleeps where possible.

The same matrix verifies every pgx isolation/access/deferrability combination,
native connection-hook failures, false-with-nil rejection, replacement, and
panic propagation, canceled-waiter cleanup, strict TLS refusal, authentication
redaction, commit-panic cleanup, and stop/restart recovery.

`CleanupTimeout` bounds container termination even after the setup context is
canceled. Setup errors, panics, and `testing.T.FailNow` clean up the owned
container; the original setup error or panic is preserved even if termination
also panics. A failed `Close` may be retried; successful cleanup is idempotent. Set
`HostPort` only when a stop/start test requires a stable loopback endpoint, and
ensure the selected port is isolated from concurrent jobs.

## Transaction isolation helper

`postgrestest.RunIsolated` begins a real pgx transaction, invokes the callback
once, and always rolls back with a fresh five-second cleanup context. Callback
and rollback failures are joined, while a panic is rolled back before the
original value is re-panicked. A callback that calls `testing.T.FailNow` also
rolls back before its goroutine terminates. A cleanup panic cannot replace a
returned callback error or either terminal callback path. With no earlier
callback cause, the cleanup panic propagates rather than returning success:

```go
err := postgrestest.RunIsolated(ctx, pool.Raw(), func(ctx context.Context, tx pgx.Tx) error {
    queries := sqlcgen.New(tx)
    return queries.CreateWidget(ctx, sqlcgen.CreateWidgetParams{
        ID:   1,
        Name: "isolated",
    })
})
```

Use it only when the code under test remains on the supplied transaction. It
cannot isolate code that commits, opens another connection, uses
non-transactional DDL, or produces process-external side effects.

Transaction-isolated tests are appropriate only when code does not commit,
open additional connections, use non-transactional DDL, or depend on session
state outside the test transaction. Otherwise create isolated schema/database
fixtures and clean them explicitly.
