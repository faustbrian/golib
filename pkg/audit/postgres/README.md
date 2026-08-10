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
starting ordinary application processes. The published initial migration
retains historical fixed `NOLOGIN` roles, and migration 2 revokes all audit
privileges from them; do not assign them to application logins. Migration 1
refuses any pre-existing fixed role that is login-capable, elevated, or has a
membership in either direction before it grants privileges. Create distinct
deployment-specific writer, reader, and retention roles, then generate and
review their grants:

```go
sql, err := auditpostgres.PrivilegeSQL(auditpostgres.RoleNames{
	Writer: "billing_audit_writer", Reader: "security_audit_reader",
	Retention: "privacy_audit_retention",
})
```

The writer can execute the idempotent append function but cannot directly
select, update, or delete audit rows or retained record-identity tombstones.
The function rejects noncanonical, malformed, privacy-invalid, or
projection-inconsistent records before persistence.

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

`NewTx` stages each bounded batch behind a savepoint in a caller-owned
transaction. A failed batch rolls back its earlier inserts; a retryable
deadlock or serialization failure aborts the complete caller transaction, as
the complete business operation must be retried. Only the caller commits a
successful transaction. `NewRetentionAdmin`
must receive a separately controlled pool with the generated retention
privileges.

MIT. See [LICENSE](LICENSE).
