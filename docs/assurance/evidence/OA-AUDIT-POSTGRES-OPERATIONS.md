# Audit PostgreSQL Operations Evidence

Observed at `2026-08-13T09:18:04Z` on `darwin/arm64` with Go `1.26.5`,
Docker Engine `29.6.2`, Testcontainers for Go `0.43.0`, PostgreSQL client tools
`18.4` (`psql` `17.0`), and digest-pinned PostgreSQL `18.4-alpine`.

## Executed Proof

Two focused audit PostgreSQL integration scenarios ran through the public
audit and PostgreSQL adapter APIs with the repository workspace, cold
task-owned Go caches, and disposable Testcontainers:

- an interrupted initial migration rolled back completely; a
  protocol-compatible writer waited across the durability migration and
  resumed after commit; malformed concurrent input remained rejected; and
  historical retention order and immutable record identity survived the
  upgrade;
- `pg_dump`, `pg_restore`, and `psql` restored canonical records, SHA-256
  digests, integrity chains, indexes, triggers, retention events, and legal
  holds into an isolated database;
- post-restore reconciliation matched original canonical bytes, held records
  remained excluded from retention, an application writer could append through
  its granted function, and that writer remained unable to read, update, or
  delete canonical audit rows;
- append, query, duplicate reconciliation, stable pagination, memory-adapter
  interoperability, retention planning, legal hold and release ordering,
  concurrent retention, transaction rollback, backend termination, pool
  recovery, and least-privilege behavior retained their contracts; and
- hostile credential-shaped canonical fields and unauthorized direct writes
  failed closed rather than entering immutable audit history.

Both selected scenarios passed. Testcontainers terminated both task-owned
PostgreSQL containers and the reaper, and the task-owned Go caches were removed
after the campaign.

## Claim Boundary

This proves bounded local audit durability, migration compatibility,
backup/restore reconciliation, retention and legal-hold behavior, privacy
validation, and least privilege against PostgreSQL 18.4. It does not prove the
full PostgreSQL 14 through 18 matrix in this campaign, managed backup or
point-in-time recovery, standby promotion, external archival custody, legal or
policy correctness of retention decisions, production key custody, operator
authorization, ECS deployment, dashboards, alerts, or incident response. The
associated operational-assurance scenarios remain pending.
