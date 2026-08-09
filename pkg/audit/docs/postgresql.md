# PostgreSQL adapter and operations

The separate `audit/postgres` module embeds an append-only migration. It creates
`audit.records`, stable query indexes, immutable retention events, update
rejection, legal-hold-aware pruning, and three `NOLOGIN` group roles:
`audit_writer`, `audit_reader`, and `audit_retention`. Grant those roles to
deployment-specific login roles. Ordinary applications should receive only
writer rights and must not own the schema, tables, functions, or migrations.
The writer role receives schema usage and execute access to the idempotent
`audit.append_record` function, but no direct table read, update, or delete
authority. Reader and retention access remain separate.

## Migration and rolling upgrade

Apply the migration using an owner credential before deploying code. The core
canonical schema version and database columns are additive compatibility
surfaces. For future changes: deploy readers that accept old and new formats,
apply additive schema/index changes concurrently where appropriate, deploy new
writers, backfill only derived/index data without rewriting canonical records,
then remove obsolete readers in a later release. Test Up and Down only on
disposable databases. The migration creates missing cluster-wide group roles
idempotently; Down destroys only the database-local audit schema and leaves
those potentially shared roles in place for the deployment owner to manage.

The integration suite applies the migration inside a transaction, interrupts
it with rollback, proves the namespace was not partially retained, and then
performs a clean application. Version 1 has no older wire format with which to
run two distinct library binaries; its rolling exercise is the frozen v1
canonical fixture across the current writer, PostgreSQL persistence, current
reader, backup, and restore. A future format cannot ship until the matrix also
runs the previous released reader and writer binaries.

## Partitioning and high volume

For high volume, range-partition by `recorded_at` with the record ID retained in
every stable order and uniqueness design. Create equivalent tenant, actor,
subject, action, correlation, and time indexes per partition. Keep a searchable
hot window and archive old partitions only after chain/export verification.
Legal holds must prevent detaching or dropping any partition containing held
records unless held records are first preserved in an independently verified
active-hold store.

The shipped table is deliberately unpartitioned, so it has no adapter-owned
partition rollover to execute. A deployment that replaces it with a partitioned
schema owns a pre-production rollover exercise: create the next partition before
the boundary, write equal-time records on both sides, prove stable
`recorded_at, record_id` pagination, probe every per-partition index, retain held
records, archive and verify the old partition, and rehearse attach/detach rollback.

## Backup, restore, and reconciliation

Use PostgreSQL physical backups or `pg_dump` including schema, records,
retention events, functions, triggers, grants, and role recreation policy.
Restore into an isolated database, apply login-role mappings, verify row counts
per stable time range, compare canonical SHA-256 values, query indexes, legal
hold state, and chain checkpoints, then perform a bounded ordered export and
compare its final anchor. Never infer success only from backup command exit.

After unknown append outcomes, query by record ID and compare canonical bytes.
After disaster recovery, reconcile the last independently checkpointed range,
detect missing prefixes/suffixes, preserve inconsistent copies, and start a new
documented chain boundary if exact repair is impossible. Ordinary credentials
must continue to fail update and delete probes after restore.
