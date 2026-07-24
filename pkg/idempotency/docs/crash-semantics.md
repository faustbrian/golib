# Crash and recovery semantics

The idempotency record and the business side effect are separate unless an
adapter-specific transaction integration explicitly combines them. The table
below states what can be concluded after a process dies.

| Crash point | Durable record | Safe recovery | Duplicate-effect risk |
| --- | --- | --- | --- |
| Before acquire | Missing or previous state | Retry acquisition. | None created by this attempt. |
| After acquire, before handler | Active lease | Wait for expiry or release during orderly shutdown. | None if the handler did not start. |
| During handler, before side effect | Active lease | Let the current owner resume or take over after expiry. | Depends on whether the handler actually crossed the side-effect boundary. |
| After side effect, before completion | Active or eventually expired lease | Reconcile by application identity before retrying; use the fence and business unique constraint. | High without application integration. |
| During heartbeat | Old or extended lease | Read the authoritative record; never infer extension from the request. | Same as the handler's current phase. |
| During result write and completion | Old active state or atomically completed state | Inspect, then replay only a fully completed record. | Backend update is atomic; the business effect may still need reconciliation. |
| During terminal failure write | Old active state or atomically failed state | Inspect; retry only according to the application's failure classification. | Retrying a truly terminal operation can duplicate effects. |
| During release | Active or atomically abandoned state | Inspect before reacquiring. | A released handler must have stopped producing effects. |
| During expiry or cleanup | Active/expired record or an atomically deleted eligible record | Acquire through the normal state machine; never manufacture a fence. | Cleanup cannot prove an old process stopped. |

Every row describes two possible observations when a response is lost: the
transition may not have committed, or it may be durable even though the caller
received an error. Only an authoritative inspection can distinguish them.

## Required ordering

An owner should follow this order:

1. Acquire and persist the returned ownership proof.
2. Start or heartbeat only while the lease is live.
3. Apply the business side effect using the fencing token and an application
   transaction, unique constraint, or conditional update.
4. Complete or fail with the same owner and fencing tokens.
5. Treat a stale-owner or expired-lease response as proof that the result was
   not recorded, not proof that the side effect was rolled back.

When the business store and idempotency store are PostgreSQL,
`postgres.Store.CompleteTx` writes completion inside a caller-owned `pgx.Tx`.
Commit the business write or outbox row in that same transaction. When the
stores are not transactionally related, callers need reconciliation logic.

## Backend loss

Storage errors fail closed by default: the package does not authorize handler
execution when it cannot establish durable ownership. The core
`AvailabilityAllowUntracked` policy is an explicit opt-in for operations whose
owners accept duplicate execution. It returns no durable ownership proof and
is never the package default. Transport integrations continue to fail closed
unless their own public contract exposes such a choice.

## Clock authority

The memory adapter accepts the narrow `clock.Clock` capability for
deterministic tests. The legacy `memory.Clock` name remains as a v1-compatible
interface. Durable adapters use backend-authoritative time for ownership
comparisons so process clock skew cannot let a caller extend or complete a
lease. Returned timestamps are diagnostic evidence; clients must not use local
time to override a backend decision.

## Executable crash-point evidence

`memory.TestCrashPointMatrix` exercises death before acquisition, after
acquisition, after heartbeat, after an expired owner continues through
takeover, after completion/result persistence, and after failure persistence.
The middleware panic tests exercise release with a fresh bounded context.
`TestPostgresUnknownCommitCanBeInspectedAfterReconnect` and
`TestValkeyUnknownResultsCanBeInspectedAfterReconnect` drop replies after the
backend mutation, then recover through inspection. PostgreSQL transaction,
deadlock, and serializable-abort tests prove that completion and business data
roll back together. Cleanup contention and Valkey promotion tests cover the
remaining backend crash boundaries. See the [hardening
report](hardening-report.md) for exact test names.
