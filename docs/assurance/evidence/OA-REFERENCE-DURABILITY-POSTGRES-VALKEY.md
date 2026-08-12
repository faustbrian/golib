# OA-REFERENCE-DURABILITY PostgreSQL And Valkey Evidence

Observed at `2026-08-12T20:01:52Z` on `darwin/arm64` with Go `1.26.5`,
Docker Engine `29.6.2`, PostgreSQL `18.4-alpine`, and Valkey `9.1.0-alpine`.

## Executed Proof

- A task-owned disposable PostgreSQL database was migrated through the public
  `migrations`, `postgres`, `idempotency`, and `outbox` APIs.
- One transaction staged application state, idempotency completion, and an
  outbox envelope, then rolled back. No application or outbox row escaped, and
  the original idempotency ownership remained acquired.
- The same work then committed atomically. The outbox relay published the
  envelope through the public queue adapter to a disposable Valkey Stream.
- A consumer received the task and terminated without acknowledging it. A new
  consumer reclaimed the identical pending payload and acknowledged it.
- Repeating the original command with the same key and fingerprint returned
  the persisted result instead of creating a second application row.
- The canonical module check passed every applicable gate. The integration
  harness and its task-owned containers, network, and Go caches were removed
  after the run.

## Claim Boundary

This proves one bounded PostgreSQL and Valkey durability composition using
public Golib APIs. It does not prove cache behavior, Kafka, schema registry,
CloudEvents, scheduler, workflow, dead-letter handling, reconciliation,
provider failover, managed-service behavior, load, soak, deployment, or
production readiness. `OA-REFERENCE-DURABILITY` therefore remains pending.
