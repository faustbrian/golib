# OA-FAILURE-RECOVERY Workflow Evidence

Observed at `2026-08-13T01:34:13Z` on `darwin/arm64` with Go `1.26.5`,
Docker Engine `29.6.2`, Testcontainers for Go `0.43.0`, and PostgreSQL
`18-alpine`.

## Executed Proof

The workflow PostgreSQL integration suite executed these focused scenarios
with `GOWORK=off`, cold task-owned Go caches, and disposable Testcontainers:

- a process exiting before commit reconciled as missing, while the same
  transition committed before process exit reconciled as committed;
- a dead worker's lease remained fenced until expiry, then a replacement
  worker reclaimed it with incremented attempt and fencing tokens;
- process death after an externally visible activity effect preserved an
  unknown activity outcome and did not invoke the effecting handler again;
- a forced PostgreSQL deadlock committed one complete transition set and
  rejected the other complete set, with exact reconciliation and no partial
  history;
- PostgreSQL restart preserved post-snapshot history, while snapshot restore
  returned the authoritative history to the snapshot boundary and reconciled
  newer work as missing;
- streaming-replica promotion rejected an interrupted primary transaction and
  reconciled the promoted replica's committed state; and
- dead-letter retry and discard commands were idempotent, audited, and
  rejected stale fencing tokens.

All six selected tests passed. Testcontainers terminated the task-owned
PostgreSQL, replica, network, and reaper resources after the campaign, and the
task-owned Go caches were removed.

## Claim Boundary

This proves bounded workflow behavior against local PostgreSQL containers. It
does not prove managed PostgreSQL failover, point-in-time or provider backup
restore, storage exhaustion, cross-region recovery, Kafka or OpenSearch
failure, Valkey failover, network partitions, or reconciliation of arbitrary
third-party side effects. `OA-FAILURE-RECOVERY` therefore remains pending.
