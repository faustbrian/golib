# audit/postgres

This separately releasable adapter persists `audit.Record` values in an
append-only PostgreSQL schema with idempotent record-ID insertion, stable
bounded queries and exports, caller-owned transaction staging, and
legal-hold-aware archive-before-delete retention.

Use `Migrations()` with the repository migration contract, construct `Store`
with an `audit_writer` pool, and reserve `RetentionAdmin` for an independently
controlled `audit_retention` pool. See the core module's
[PostgreSQL operations guide](../docs/postgresql.md).

## Usage

Apply the embedded migration with the deployment's migration owner before
starting ordinary application processes. The application pool should inherit
only `audit_writer` (or `audit_reader` for a read-only process).
`audit_writer` can execute the idempotent append function but cannot directly
select, update, or delete audit rows.

```go
pool, err := pgxpool.New(ctx, dsn)
if err != nil {
	return err
}
defer pool.Close()

sink, err := auditpostgres.New(pool, auditpostgres.Config{
	MaxBatchRecords: 100,
})
if err != nil {
	return err
}
result, err := sink.Append(ctx, record)
if err != nil {
	if audit.AppendOutcomeOf(err) == audit.AppendUnknown {
		// Reconcile record.ID() before deciding whether to retry.
	}
	return err
}
_ = result
```

`NewTx` stages the same bounded idempotent insert in a caller-owned
transaction; only the caller commits or rolls back it. `NewRetentionAdmin`
must receive a separately controlled pool with `audit_retention` privileges.

MIT. See [LICENSE](LICENSE).
