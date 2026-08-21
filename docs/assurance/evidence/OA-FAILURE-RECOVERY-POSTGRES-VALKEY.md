# OA-FAILURE-RECOVERY PostgreSQL And Valkey Evidence

Observed at `2026-08-13T01:12:01Z` on `darwin/arm64` with Go `1.26.5`,
Docker Engine `29.6.2`, PostgreSQL `18.4-alpine`, and Valkey
`9.1.0-alpine`.

## Executed Proof

- A fresh process atomically committed one business mutation, its idempotency
  completion, and its outbox intent through public package APIs, relayed the
  outbox envelope to a Valkey Stream, and claimed the task without
  acknowledgement.
- The harness persisted the exact envelope, task, and idempotency identities to
  a task-owned mode-0600 handoff, then terminated the process with `SIGKILL`.
  The observed exit status was `137`; no application shutdown callback ran.
- PostgreSQL and Valkey were both killed and their containers removed. Fresh
  containers were created from the pinned images using the same dedicated
  durable volumes, exercising PostgreSQL crash recovery and Valkey AOF replay.
- Recovery while PostgreSQL was absent failed at the PostgreSQL connection
  boundary. After PostgreSQL returned, recovery while Valkey was absent failed
  at the replacement-consumer boundary. Neither outage was treated as success.
- After both dependencies returned, a fresh process observed exactly one
  business row, a completed idempotency replay, the matching delivered outbox
  record, and the exact abandoned queue task. It reclaimed and acknowledged
  that task without duplicating the business mutation or outbox record.
- Valkey was killed and replaced a second time after acknowledgement. A fresh
  consumer-group inspection reported known lag with zero pending, lagging, or
  total work, proving the acknowledgement survived broker replacement.
- Every container, volume, process, handoff, binary, and Go cache was task-owned
  and removed immediately after the campaign.

## Claim Boundary

This proves bounded local process death, dependency unavailability, PostgreSQL
crash recovery, Valkey AOF replay, abandoned-delivery reclamation, idempotent
database replay, and acknowledgement persistence. It does not prove an
ambiguous PostgreSQL commit, deadlock or serialization retry, backup/restore,
storage exhaustion, stale leases, Valkey eviction or script interruption,
managed failover, network partitions, Kafka, OpenSearch, poison work,
cross-region recovery, or externally visible side-effect reconciliation.
`OA-FAILURE-RECOVERY` therefore remains pending.
