# Migration Guides

Migration preserves observable contracts first and replaces framework
ownership incrementally. Do not combine language migration with an avoidable
database, payload, queue, or provider migration.

- [Laravel and PHP](laravel.md) maps framework facilities to explicit Golib
  packages and identifies intentionally absent magic.
- [Standalone Go](standalone.md) covers incremental adoption from standard
  library or third-party Go services.
- [Monorepo paths](../migration.md) records the repository's own standalone-
  repository migration.

Every migration needs request and response fixtures, database compatibility,
queue and side-effect ownership, rollback boundaries, performance baselines,
and a period where old and new implementations can be compared safely.

Return to the [documentation index](../index.md).
