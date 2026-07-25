# Quickstart

## Install the core module

```sh
go get github.com/faustbrian/golib/pkg/outbox@vX.Y.Z
```

The current version 1 candidate is not released. When a release exists, pin
its exact version and review the compatibility, migration, and changelog
contracts before upgrading.

## Apply migrations

Read versioned migrations from `postgres.Migrations()` and pass the returned
`fs.FS` to the application's migration runner. The application chooses when
and how migrations run; `outbox` does not migrate at package initialization.

## Write with pgx

```go
func createOrder(ctx context.Context, pool *pgxpool.Pool, payload []byte) error {
    builder, err := outbox.NewEnvelopeBuilder()
    if err != nil {
        return err
    }
    envelope, err := builder.Build(outbox.NewEnvelopeParams{
        Topic:          "orders.created",
        Payload:        payload,
        PayloadVersion: 1,
        OrderingKey:    "customer-42",
        IdempotencyKey: "create-order-command-7",
    })
    if err != nil {
        return err
    }

    tx, err := pool.Begin(ctx)
    if err != nil {
        return err
    }
    defer tx.Rollback(ctx)

    if _, err := tx.Exec(ctx,
        "INSERT INTO orders (id) VALUES ($1)", "order-42",
    ); err != nil {
        return err
    }

    writer, err := postgres.NewWriter(postgres.WriterConfig{})
    if err != nil {
        return err
    }
    if err := writer.Insert(ctx, tx, envelope); err != nil {
        return err
    }

    return tx.Commit(ctx)
}
```

## Use the same transaction with sqlc

```go
tx, err := pool.Begin(ctx)
if err != nil {
    return err
}
defer tx.Rollback(ctx)

queries := generated.New(pool).WithTx(tx)
if err := queries.CreateOrder(ctx, params); err != nil {
    return err
}
if err := writer.Insert(ctx, tx, envelope); err != nil {
    return err
}

return tx.Commit(ctx)
```

The generated query object and writer must receive the same `tx`. A generated
query object still bound to the pool breaks atomicity.

## Run an embedded relay

```go
store, err := postgres.NewStore(pool, postgres.StoreConfig{
    MaxClaimBatch:    100,
    MaxLeaseDuration: time.Minute,
})
if err != nil {
    return err
}

worker, err := relay.New(store, publisher, relay.Config{
    Owner:         hostname,
    BatchSize:     100,
    Workers:       8,
    LeaseDuration: 30 * time.Second,
    MaxAttempts:   10,
    PollInterval:  time.Second,
    Serialization: postgres.SerializeByOrderingKey,
})
if err != nil {
    return err
}

return worker.Run(ctx)
```

Cancellation stops polling. Publisher calls receive the canceled context, and
claims canceled before acceptance are released with a bounded cleanup context.
Process death is recovered through lease expiry.

## Consumer idempotency

Persist the envelope ID or an application idempotency key in the same
transaction as the consumer side effect. If the key already exists, acknowledge
the duplicate without repeating the side effect. The
[`idempotency` integration guide](idempotency.md) covers canonical
fingerprints, acquisition outcomes, fail-closed behavior, and transactional
completion. Using it does not change delivery to exactly once.
