# postgres

`postgres` is focused production infrastructure for PostgreSQL applications
built directly on [`pgx/v5`](https://github.com/jackc/pgx). It standardizes
finite pool configuration, bounded lifecycle operations, transaction cleanup,
SQLSTATE classification, health, safe telemetry, and real PostgreSQL testing
without hiding native pgx types.

It is not a driver, ORM, query builder, migration engine, repository layer, or
multi-database abstraction.

## Requirements

- Go 1.25 or 1.26
- pgx 5.10.x
- PostgreSQL 14, 15, 16, 17, or 18
- Docker-compatible container runtime only for `postgrestest` and integration
  tests

## Quick start

```go
ctx := context.Background()
pool, err := postgres.New(ctx, postgres.Config{
    DSN:             os.Getenv("DATABASE_URL"),
    MaxConns:        20,
    AcquireTimeout:  2 * time.Second,
    PingTimeout:     time.Second,
    ShutdownTimeout: 10 * time.Second,
})
if err != nil {
    return err
}
defer pool.Close(context.Background())

err = postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{
    TxOptions: pgx.TxOptions{IsoLevel: pgx.Serializable},
}, func(ctx context.Context, tx pgx.Tx) error {
    _, err := tx.Exec(ctx, "INSERT INTO jobs (name) VALUES ($1)", "reindex")
    return err
})
```

`New` performs a bounded startup ping by default. `Pool.Raw()` returns the exact
`*pgxpool.Pool`, so generated `sqlc` code and all native pgx operations remain
available.

## Safe defaults

| Setting | Default |
| --- | ---: |
| connect timeout | 5 seconds |
| acquire timeout | 5 seconds |
| ping timeout | 2 seconds |
| shutdown timeout | 10 seconds |
| maximum connections | 10 |
| maximum connection lifetime | 1 hour |
| lifetime jitter | 5 minutes |
| maximum idle time | 30 minutes |
| health-check period | 1 minute |
| startup policy | fail-fast ping |

Zero values select these defaults. Every limit is overrideable. TLS remains an
explicit deployment decision: use a DSN with `sslmode=verify-full` or provide a
verified `tls.Config` with `TLSRequire`; never assume encryption authenticates
the server when `InsecureSkipVerify` is enabled.

## Packages

- root: configuration, pool lifecycle, transactions, health, classification,
  bounded observations, and safe `slog` integration
- `otelpostgres`: optional standard OpenTelemetry metrics adapter
- `postgrestest`: optional Testcontainers lifecycle and always-rollback
  transaction helpers for real PostgreSQL

Query tracing is provided by
[`telemetry/instrumentation/gopostgres`](https://github.com/faustbrian/golib/pkg/telemetry/tree/main/instrumentation/gopostgres)
through the native `pgx.ConnConfig.Tracer` hook. It records allow-listed query
names and never SQL or arguments.

## Documentation

Start with the [documentation index](docs/README.md), [quickstart](docs/quickstart.md),
and [API reference](docs/api.md). Operators should read the
[pool and lifecycle guide](docs/pool-and-lifecycle.md), [TLS guide](docs/tls.md),
[Kubernetes guide](docs/kubernetes.md), and [operational FAQ](docs/faq.md).

## Development

```sh
make safety
make integration
make check
```

`make coverage` proves exact 100% production statement coverage with a real
PostgreSQL container. CI runs the integration suite on every supported
PostgreSQL major version.

## License

MIT. See [LICENSE](LICENSE) and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
