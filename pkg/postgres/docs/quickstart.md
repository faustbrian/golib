# Quickstart

Install the module and create one application-owned pool:

```sh
go get github.com/faustbrian/golib/pkg/postgres
```

```go
pool, err := postgres.New(ctx, postgres.Config{
    DSN:             os.Getenv("DATABASE_URL"),
    MaxConns:        20,
    MinIdleConns:    2,
    AcquireTimeout:  2 * time.Second,
    PingTimeout:     time.Second,
    ShutdownTimeout: 10 * time.Second,
    SessionInit: func(ctx context.Context, conn *pgx.Conn) error {
        _, err := conn.Exec(ctx, "SET statement_timeout = '5s'")
        return err
    },
})
if err != nil {
    return err
}
defer pool.Close(context.Background())
```

`New` parses and validates the DSN without returning it in validation errors,
constructs the native pool, then performs a bounded startup ping. Use
`StartupLazy` only when startup must succeed while PostgreSQL is unavailable.

Use native pgx methods through `pool.Raw()`:

```go
var count int
err := pool.Raw().QueryRow(ctx, "SELECT count(*) FROM jobs").Scan(&count)
```

Use the transaction runner when cleanup and error composition matter:

```go
err := postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{},
    func(ctx context.Context, tx pgx.Tx) error {
        _, err := tx.Exec(ctx, "UPDATE jobs SET claimed_at = now() WHERE id = $1", id)
        return err
    },
)
```

Read [TLS](tls.md), [pool lifecycle](pool-and-lifecycle.md), and
[transactions](transactions.md) before production deployment.
