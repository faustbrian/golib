# OA-REFERENCE-DURABILITY PostgreSQL Version Matrix Evidence

Observed through `2026-08-13T12:35:16Z` on Darwin/arm64 with Go 1.26.5 and
Docker Engine 29.6.2. The campaign used digest-pinned PostgreSQL and Valkey
images, uniquely labelled containers and networks, and task-owned disposable
Go build and module caches. Every task-owned resource was removed after the
successful run.

The maintained `check-version-matrix.sh` campaign had SHA-256
`83247e743d148964db98d20737aa0b5097c9a12fb577601ece40e1252e27947f`.
Its PostgreSQL matrix had SHA-256
`fd0bde73bfd9e6499e795dce399abe411416f8079c3d345b8576f16f01cb0acb`,
and its Valkey identity file had SHA-256
`091ea2a4d294694acaffe0a67099f29fd58bc05e4ddf40bfad1b60c8a48f408e`.

The same public Golib durability composition passed against:

- PostgreSQL 14.23 and Valkey 9.1.0;
- PostgreSQL 15.18 and Valkey 9.1.0;
- PostgreSQL 16.14 and Valkey 9.1.0;
- PostgreSQL 17.10 and Valkey 9.1.0; and
- PostgreSQL 18.4 and Valkey 9.1.0.

For every isolated backend pair, the fixture proved transaction rollback
isolation, atomic business/idempotency/outbox commit, Valkey Stream publication,
unacknowledged-task reclamation, acknowledgement, and idempotent command replay.
The campaign also verified each live server's reported major or exact version
before exercising the composition.

This proves the reference durability contract across the complete supported
PostgreSQL 14 through 18 matrix with Valkey 9.1.0. It does not prove an
in-place PostgreSQL upgrade, managed failover or backup/restore, storage
exhaustion, network partitions, production capacity, or ECS deployment.
`OA-REFERENCE-DURABILITY` and `OA-DEPLOYMENT-COMPATIBILITY` therefore remain
pending.
