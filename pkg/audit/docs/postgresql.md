# PostgreSQL adapter and operations

The separate `audit/postgres` module embeds append-only forward migrations. It
creates `audit.records`, stable query indexes, immutable retention events,
update rejection, legal-hold-aware pruning, canonical projections and digests,
and acceptance-order watermarks. Migrations create no deployment roles and the
hardening migration revokes privileges from historical fixed role names.

Create deployment-specific roles, pass their distinct names to
`PrivilegeSQL(RoleNames{...})`, review the returned statements, and apply them
with the migration owner. Ordinary applications should receive only writer
rights and must not own the schema, tables, functions, or migrations. The
writer receives schema usage and execute access to the idempotent append
function, but no direct table read, update, or delete authority. Reader and
retention access remain separate.

## Migration and rolling upgrade

Apply the migration using an owner credential before deploying code. The core
canonical schema version and database columns are additive compatibility
surfaces. For future changes: deploy readers that accept old and new formats,
apply additive schema/index changes concurrently where appropriate, deploy new
writers, backfill only derived/index data without rewriting canonical records,
then remove obsolete readers in a later release. Migration 2 is intentionally
forward-only because silently dropping the audit schema would destroy accepted
records; rollback requires an explicit deployment-owned recovery plan.

The integration suite applies the migration inside a transaction, interrupts
it with rollback, proves the namespace was not partially retained, and then
performs a clean application. It also upgrades the original schema, backfills
acceptance order and digests, and verifies the frozen version-1 record through
the current writer, persistence, reader, backup, and restore. Because there is
no previous released binary, this is the only meaningful mixed-version
exercise for version 1. A future format cannot ship until the matrix also runs
the previous released reader and writer binaries.

## High volume and partitioning boundary

The supported schema is deliberately unpartitioned because PostgreSQL cannot
enforce the adapter's global record-ID uniqueness on a range-partitioned table
unless the partition key becomes part of that uniqueness constraint. The
adapter therefore does not claim partition rollover support. Replacing the
table with a partitioned design is a different first-party adapter contract,
not a deployment tuning step; it requires its own global-ID reconciliation,
indexes, retention and legal-hold semantics, migrations, rollover and restore
tests, and compatibility evidence before use.

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
