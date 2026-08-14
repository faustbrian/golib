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

For a fresh database, execute `FreshInstallPreflightSQL()` and every embedded
migration in one outer transaction with the deployment's migration owner. The
preflight atomically creates the three fixed roles as `NOLOGIN`; an existing
name collision fails with PostgreSQL error `42710`, while a concurrent creator
waits and then fails under role-name uniqueness. Migration 1 cannot grant
temporary access to either collision. The outer transaction is required so a
failed installation rolls the role reservation back. For an existing
installation, commit migration 3's role neutralization before starting
migration 4; poisoned legacy history must not
roll the role safety change back. Apply migration 4 transactionally without
splitting its validation, identity backfill, trigger installation, and
privilege revocation across committed statements. It fails if the required
record and retention-event locks cannot be acquired within 30 seconds, or if
historical retention events contain duplicate per-record acceptance orders.
The published initial migration
retains historical fixed `NOLOGIN` roles, and migration 2 revokes all audit
privileges from them. Migration 3 removes every membership involving
those reserved names and strips login, inheritance, and elevated role
attributes and stored passwords. Apply it with a migration owner authorized to
manage roles, and do not assign the fixed names to applications. The database
encoding must be `UTF8`; migration 4 rejects databases that cannot represent
every canonical record. Create distinct deployment-specific writer, reader,
and retention roles, then generate and review their grants:

```go
sql, err := auditpostgres.PrivilegeSQL(auditpostgres.RoleNames{
	Writer: "billing_audit_writer", Reader: "security_audit_reader",
	Retention: "privacy_audit_retention",
})
```

The writer can execute the idempotent append function but cannot directly
select, update, or delete audit rows or retained record-identity tombstones.
The function rejects noncanonical, malformed, privacy-invalid, or
projection-inconsistent records before persistence. Dependency-bearing
database routines and trigger functions pin trusted search paths so
caller-controlled schemas cannot replace their lock, validation, or
canonicalization dependencies. Authentication methods must be empty or match
the core module's closed `AuthenticationMethod*` vocabulary; arbitrary custom
labels and credential-shaped values are rejected.

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

## Ecosystem

Use the [Golib documentation portal](https://github.com/faustbrian/golib/blob/main/docs/index.md)
to choose companion packages, supported stacks, recipes, and operations guidance.
