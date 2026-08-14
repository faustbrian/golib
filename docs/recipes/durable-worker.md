# Durable Command And Worker Recipe

Use this composition when one command must atomically persist business state,
idempotency completion, and an outbox record before asynchronous work is
published and acknowledged.

## Ownership

- `postgres` owns pool and transaction mechanics.
- `migrations` owns forward-only schema transitions and migration history.
- `idempotency` owns command replay identity and stored terminal outcomes.
- `outbox` owns publication records written in the business transaction.
- `queue` owns claim, redelivery, acknowledgement, and dead-letter mechanics.
- The application owns business invariants, retry eligibility, and external
  side-effect reconciliation.

The reference flow rolls back all durable records on command failure, commits
them atomically on success, relays through Valkey Streams, reclaims
unacknowledged work after consumer restart, acknowledges terminal work, and
returns the stored command result on replay.

Run the bounded composition:

```sh
./scripts/run-modules.sh check --jobs 1 --modules pkg/service/integration/reference-durability
```

Run the task-owned crash/replacement and PostgreSQL 14 through 18 campaigns:

```sh
cd pkg/service/integration/reference-durability
./check-recovery.sh
./check-version-matrix.sh
```

Do not claim exactly-once external side effects. A worker can prove durable
claim and settlement, while an effect completed before process death can still
have an unknown outcome. Preserve stable idempotency keys and reconcile that
boundary explicitly.

See [persistence operations](../operations/index.md#durability-and-workers) and
the fixture [README](../../pkg/service/integration/reference-durability/README.md).

Return to the [recipe index](index.md).
