# Schema Migrations And Upgrades

`postgres.Migrations()` exposes one canonical
`000001_create_outbox.sql` file through `fs.FS`; applications choose the
runner and timing. The file uses `-- +migrations Up` and
`-- +migrations Down` sections accepted directly by `migrations`. The
library never migrates during initialization.

For every release, test clean install and upgrade from every released schema,
run old code during additive phases where compatibility is claimed, deploy the
required schema before code, and verify constraints, indexes, and integration
tests. Never edit a released migration; add a reversible version. Destructive
changes require expand/migrate/contract planning.

The initial schema has no older released upgrade source. Its down migration is
for development and deletes data. Production rollback should normally roll
forward or restore a consistent backup, not run destructive down SQL because
an application deployment failed.

## Schema-upgrade matrix

| Source state | Target | Supported path | Application/relay concurrency | Rollback policy | Evidence |
|---|---|---|---|---|---|
| Empty database | `000001` | Apply the canonical up section once | Writers and relays must start only after the install commits; no pre-schema compatibility is claimed | Development down removes both managed tables; production restores or rolls forward | Clean up/down/reapply on PostgreSQL 14–18 and real `migrations` clean install |
| `000001` already recorded | `000001` | Migration runner no-op | Existing writers and relays may continue because no DDL is applied | None required | Two concurrent real runners produce one owned-ledger record |
| Any published predecessor | `000001` | Not applicable: no schema version has been published before this unreleased candidate | No mixed-version compatibility claim exists yet | Not applicable | `CHANGELOG.md` remains `Unreleased` |
| Future predecessor | Future migration | Must use a new reversible expand/migrate/contract file | Old and new code must be exercised concurrently for every phase where compatibility is claimed | Roll forward by default; destructive down requires an explicit recovery plan | A fixture and integration case are required for every published predecessor |

Concurrent migration runners are proven today; concurrent application or
relay execution during the initial install is intentionally unsupported. The
first future upgrade must replace the not-applicable row with executable
old-schema fixtures and mixed-version tests before publication.

The PostgreSQL-major matrix executes the initial up migration, exercises the
full state machine, applies the development down migration, verifies both
managed tables are removed, and reapplies the clean schema. The local
cross-repository source contract test loads the embedded filesystem through
the real `migrations.FSSource` and verifies identity, sections, and checksum.
Its runner contract test launches two concurrent runners and verifies one
clean owned-ledger record; run it whenever the sibling `migrations` checkout
changes:

```sh
make migration-integration POSTGRES_VERSION=18
```

Set `GO_MIGRATIONS_DIR=/path/to/migrations` when the sibling checkout is not
at `../migrations`. The command creates and removes its own temporary Go
workspace. Future schema versions must add an upgrade fixture for every
released predecessor.
