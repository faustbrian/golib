# Package integration

Adopt incrementally and preserve existing public constructors until a planned
major release. Add an internal constructor or option that accepts the narrowest
capability, then make the old constructor supply `clock.System{}`.

Timestamp-only packages such as authentication token issuers should accept
`clock.Clock`. Retry or timeout code may need `Sleeper` or `TimerFactory` but
should not receive ticker/callback methods. Cache expiry code often needs both
`Clock` and `TimerFactory`; define a local composite only for that consumer.

Two owned libraries provide the initial compatibility proof:

- `authentication` uses the root `Clock` contract for instrumentation, JWT,
  and OIDC wall time and builds deterministic fixtures on `manual.Clock`. Its
  existing named interfaces remain deprecated v1 compatibility contracts.
- `idempotency` uses `Clock` in its in-memory adapter and deterministic test
  fixtures. Durable PostgreSQL and Valkey adapters retain backend-authoritative
  time for fencing decisions.

Both adoptions passed their complete local race and exact 100% coverage gates
on 2026-07-16. The verified commits are `authentication@1da3643` and
`idempotency@1cdba39`; neither adoption relies on a committed local
replacement. The adoption sequence for the remaining owned libraries is:

1. inventory local clock interfaces and direct `time` calls;
2. map each client to one capability;
3. add a `clock` dependency without removing public symbols;
4. adapt internal implementations to `System`;
5. replace sleep-based tests with `manual` or `synctest`;
6. deprecate duplicated interfaces only when SemVer permits.

`calendar` continues to own civil arithmetic, `temporal` interval
algebra, `scheduler` scheduling, and `lease` distributed fencing.
