# outbox

`outbox` is a PostgreSQL-first Go implementation of the transactional
outbox pattern. It writes application state and publishable envelopes in the
same caller-owned `pgx` transaction, then relays committed envelopes to a
small publisher contract with at-least-once delivery.

The version 1 release candidate targets the compatibility surfaces described
in [the compatibility policy](docs/compatibility.md). Delivery remains at
least once; upgrading the library does not remove the consumer's idempotency
duty.

## Guarantees

- Atomic application and outbox persistence only when both writes use the
  same successful `pgx.Tx`.
- At-least-once relay delivery. Publisher acceptance followed by a failed or
  ambiguous delivered update can publish the same envelope again.
- Concurrent claims use PostgreSQL row locks and `SKIP LOCKED`.
- Every mutation of a leased record requires its current opaque lease token.
- Batch, worker, lease, retry, administrative, payload, and polling limits are
  explicit and bounded.
- Optional ordering-key or topic serialization is enforced at the PostgreSQL
  claim seam across relay processes. There is no global ordering guarantee.

Consumers **must be idempotent**. This project does not provide distributed
transactions or exactly-once delivery.

## Packages

- `github.com/faustbrian/golib/pkg/outbox`: envelope construction and validation.
- `github.com/faustbrian/golib/pkg/outbox/postgres`: migrations, transactional writer,
  claims, leases, retries, dead letters, replay, and retention.
- `github.com/faustbrian/golib/pkg/outbox/relay`: bounded embedded relay.
- `github.com/faustbrian/golib/pkg/outbox/adapters/goqueue`: separately versioned
  `queue` publisher adapter; importing core does not add `queue`.
- `github.com/faustbrian/golib/pkg/outbox/adapters/gotelemetry`: separately versioned
  metrics and trace-linkage integration compatible with `telemetry`.

## Quick start

```go
builder, err := outbox.NewEnvelopeBuilder()
if err != nil {
    return err
}

envelope, err := builder.Build(outbox.NewEnvelopeParams{
    Topic:          "orders.created",
    Payload:        payload,
    OrderingKey:    customerID,
    IdempotencyKey: commandID,
})
if err != nil {
    return err
}

tx, err := pool.Begin(ctx)
if err != nil {
    return err
}
defer tx.Rollback(ctx)

if _, err := tx.Exec(ctx, insertOrderSQL, orderID); err != nil {
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
```

The writer never opens or commits a transaction. Passing a pool or standalone
connection is impossible because the API requires `pgx.Tx`.

See the [documentation index](docs/README.md),
[full quickstart](docs/quickstart.md), [delivery guarantees](docs/guarantees.md),
and [architecture and crash matrix](docs/architecture.md).

## Development gates

```sh
make check
make recovery POSTGRES_VERSION=18
make migration-integration POSTGRES_VERSION=18
```

Integration tests use ephemeral Testcontainers PostgreSQL instances. They do
not use an existing application or production database. The migration gate
uses `GO_MIGRATIONS_DIR` when the sibling checkout is not at
`../migrations`.

## Status

The version 1 release candidate includes the core state machine, concurrency
tests, payload-safe lifecycle events, health diagnostics, PostgreSQL backlog
statistics, `queue` and telemetry adapters, CI matrices, fuzzing,
benchmarks, and archive-before-delete retention. The hardening verdict and
release gates are green; the project remains unreleased until maintainers
publish a version.

## License

MIT. See [LICENSE](LICENSE).

Contribution, conduct, support, and vulnerability-reporting policies are in
[CONTRIBUTING.md](CONTRIBUTING.md),
[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md), [SUPPORT.md](SUPPORT.md), and
[SECURITY.md](SECURITY.md).
