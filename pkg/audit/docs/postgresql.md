# PostgreSQL adapter and operations

The separate `audit/postgres` module embeds append-only forward migrations. It
creates `audit.records`, stable query indexes, immutable retention events,
update rejection, legal-hold-aware pruning, canonical projections and digests,
acceptance-order watermarks, and immutable record-ID digest tombstones that
survive record pruning. The security-definer append function reconstructs the
complete canonical record and rejects schema, privacy, and canonical-format
violations before inserting either identity or content. The published initial
migration retains its historical fixed `NOLOGIN` role creation; the hardening
migration revokes every audit privilege from those inert names. Do not assign
them to application logins. The fixed-role safety migration removes every
membership involving the reserved names and strips login, inheritance, and
elevated attributes. It also clears any stored password. Its migration owner
must therefore be authorized to manage roles. Every role-catalog lookup and
dynamic role statement is bound directly to `pg_catalog`, so a caller-controlled
search path cannot hide a membership from neutralization.

Create deployment-specific roles, pass their distinct names to
`PrivilegeSQL(RoleNames{...})`, review the returned statements, and apply them
with the migration owner. Ordinary applications should receive only writer
rights and must not own the schema, tables, functions, or migrations. The
writer receives schema usage and execute access to the idempotent append
function, but no direct table read, update, or delete authority. Reader and
retention access remain separate.

On a fresh database, execute `FreshInstallPreflightSQL()` and all embedded
migrations in one outer transaction. The preflight atomically creates all
three fixed roles as `NOLOGIN`; PostgreSQL rejects an existing collision with
error `42710`, while a concurrent creator waits and then fails under role-name
uniqueness. Migration 1 cannot grant temporary access to either collision. The
outer transaction ensures a failed installation retains none of the reserved
roles. On an existing
installation, commit migration 3 separately before migration 4: malformed
legacy history must not roll fixed-role neutralization back. Migration 4 uses
the repository's normal transactional mode and must not be split across
committed statements; legacy validation, identity backfill, trigger
installation, and privilege revocation must either all commit or all roll back.
Migration 4 also replaces the historical retention-order index with a unique
per-record index. Duplicate historical acceptance orders make legal-hold order
ambiguous, so the migration fails atomically instead of guessing or rewriting
their meaning.
Dependency-bearing database routines and trigger functions pin trusted search
paths so application-controlled schemas cannot shadow locking, validation, or
canonicalization dependencies.

The database encoding must be `UTF8`. Migration 4 rejects other server
encodings because they cannot represent every valid canonical record before
re-encoding it to canonical UTF-8 bytes.
Migration 4 also rejects legacy canonical records whose authentication method
is outside the closed core `AuthenticationMethod*` vocabulary. Such a value
may be a credential that passed the historical syntax-only check; treat the
failed migration as a privacy incident requiring independent review rather
than rewriting or silently blessing immutable history.

## Migration and rolling upgrade

Apply the migrations using an owner credential before deploying code. The core
canonical schema version and database columns are additive compatibility
surfaces. For future changes: deploy readers that accept old and new formats,
apply additive schema/index changes concurrently where appropriate, deploy new
writers, backfill only derived/index data without rewriting canonical records,
then remove obsolete readers in a later release. Migrations 2 through 4 are
intentionally forward-only because silently dropping the audit schema would
destroy accepted records; rollback requires an explicit deployment-owned
recovery plan. Migration 4 validates every legacy canonical row before
committing and fails
atomically on malformed history. The validation and identity backfill scan the
complete records table while migration locks exclude record and retention-event
writers. Migration 4 bounds lock acquisition at 30 seconds and fails visibly if
current writers do not drain; size the subsequent validation and unique-index
window from a production-like copy before rollout. If duplicate historical
retention orders are reported, preserve the failed database, reconcile the
source events and their intended acceptance order under the deployment's
incident process, and rerun the unchanged migration only after independent
review; the adapter does not silently choose which hold or release came last.

The integration suite applies the migration inside a transaction, interrupts
it with rollback, proves the namespace was not partially retained, and then
performs a clean application. It also upgrades the original schema, backfills
acceptance order and digests, holds a protocol-compatible version-1 writer at
the migration 4 lock, then proves it resumes after commit through the new
identity-capture trigger without omitting its tombstone. The frozen version-1
record is verified through the current writer, persistence, reader, backup,
and restore. Because there is no previous released binary, this exercises the
complete existing database protocol rather than a historical executable. A
future format cannot ship until the matrix also runs the previous released
reader and writer binaries.

`make integration-matrix` persists one atomic evidence record per major. Each
record binds the complete test input digest, selected major, immutable image
digest, execution revision, environment, log digest, and result; the aggregate
is derived only after all five checkpoints exist.

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
Pruning removes canonical content but retains the record ID and canonical
SHA-256 tombstone permanently. An identical delayed retry remains a duplicate,
and a different record under that ID remains a conflict, including during a
concurrent append and prune.
After disaster recovery, reconcile the last independently checkpointed range,
detect missing prefixes/suffixes, preserve inconsistent copies, and start a new
documented chain boundary if exact repair is impossible. Ordinary credentials
must continue to fail update and delete probes after restore.
