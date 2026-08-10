# audit/postgres

This separately releasable adapter persists `audit.Record` values in an
append-only PostgreSQL schema with idempotent record-ID insertion, stable
bounded queries and exports, caller-owned transaction staging, and
legal-hold-aware archive-before-delete retention.

Use `Migrations()` with the repository migration contract, create
deployment-owned roles, and apply the output of `PrivilegeSQL`. Construct
`Store` with the deployment writer pool and reserve `RetentionAdmin` for an
independently controlled retention pool. See the core module's
[PostgreSQL operations guide](../docs/postgresql.md).

## Usage

Apply the embedded migration with the deployment's migration owner before
starting ordinary application processes. Fixed role names are intentionally
not provisioned. Create distinct deployment-specific writer, reader, and
retention roles, then generate and review their grants:

```go
sql, err := auditpostgres.PrivilegeSQL(auditpostgres.RoleNames{
	Writer: "billing_audit_writer", Reader: "security_audit_reader",
	Retention: "privacy_audit_retention",
})
```

The writer can execute the idempotent append function but cannot directly
select, update, or delete audit rows.

```go
pool, err := pgxpool.New(ctx, dsn)
if err != nil {
	return err
}
defer pool.Close()

sink, err := auditpostgres.New(pool, auditpostgres.Config{
	MaxBatchRecords: 100,
	MaxBatchBytes:   8 << 20,
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
must receive a separately controlled pool with the generated retention
privileges.

MIT. See [LICENSE](LICENSE).
