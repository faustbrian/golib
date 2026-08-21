# Migrations PostgreSQL Recovery Evidence

Observed at `2026-08-13T09:08:32Z` on `darwin/arm64` with Go `1.26.5`,
Docker Engine `29.6.2`, Testcontainers for Go `0.43.0`, and PostgreSQL
`18.4-alpine`.

## Executed Proof

The complete migrations PostgreSQL integration conformance test ran with
`GOWORK=off`, cold task-owned Go caches, and disposable Testcontainers. It
proved that:

- migration planning, apply, status, idempotency, rollback, checksum drift,
  rename, deletion, and concurrent runner serialization behaved consistently;
- a terminated process waiting for the advisory lock did not prepare the
  ledger or mutate the schema, while timeout and cancellation preserved the
  current owner's session and a PostgreSQL-terminated lock connection released
  ownership for retry;
- process termination during transactional SQL rolled back both schema and
  ledger, while termination after a no-transaction dirty insert preserved
  visible dirty state for checksum-bound recovery;
- termination during the clean-ledger update preserved dirty state until an
  explicit checksum-bound mark-applied recovery;
- transaction failure and statement timeout left retryable history, while
  no-transaction failure exposed partial effects as dirty until reviewed
  recovery;
- a historical v1 ledger remained readable, accepted a pending migration, and
  retained its existing history without rewriting it; and
- Laravel migration history remained unchanged while exact baseline fixtures
  were accepted and empty, drifted, partial, and unexpectedly advanced schemas
  failed closed.

All conformance and nested recovery scenarios passed. Testcontainers
terminated the task-owned PostgreSQL container and reaper, and the task-owned
Go caches were removed after the campaign.

## Claim Boundary

This proves local migration durability, interruption recovery, source and
ledger compatibility, and Laravel baseline protection against PostgreSQL
18.4. It does not prove the documented PostgreSQL 14 through 18 matrix in this
campaign, managed-service failover or backup restore, storage exhaustion,
network partitions, ECS deployment orchestration, mixed application binaries,
Goose upgrade combinations, or rollback of arbitrary application releases.
The associated operational-assurance scenarios remain pending.
