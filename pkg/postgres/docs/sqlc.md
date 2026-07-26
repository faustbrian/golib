# sqlc integration

Generated pgx/v5 code usually exposes a `DBTX` interface and
`Queries.WithTx(pgx.Tx)`. `Pool.Raw()` satisfies generated query methods, and
the transaction callback receives the exact native `pgx.Tx` expected by
`WithTx`.

```go
queries := db.New(pool.Raw())

err := postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{},
    func(ctx context.Context, tx pgx.Tx) error {
        qtx := queries.WithTx(tx)
        account, err := qtx.LockAccount(ctx, accountID)
        if err != nil {
            return err
        }
        return qtx.UpdateBalance(ctx, db.UpdateBalanceParams{
            ID: account.ID,
            Balance: account.Balance + delta,
        })
    },
)
```

Do not add an adapter around every generated method. Keep the generated query
API and pgx types visible. For telemetry query names, use trusted static names
from generated methods or caller code and configure the allow-list in
`telemetry/instrumentation/gopostgres`; never derive metric labels from SQL,
arguments, tenant IDs, or unbounded request input.

```go
queryCtx := gopostgres.ContextWithOperation(ctx, "accounts.lock")
account, err := queries.LockAccount(queryCtx, accountID)
```

Only names present in the tracer's finite `Operations` allow-list are emitted;
unknown names collapse to its fixed default.
