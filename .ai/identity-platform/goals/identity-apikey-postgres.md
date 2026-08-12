# Goal: pkg/identity/apikey/postgres

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/apikey/postgres`
- Canonical module: `pkg/identity/apikey/postgres`
- Canonical goal after scaffolding: `pkg/identity/apikey/postgres/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/apikey/postgres:v1`; owned operation IDs: none
- Requires: `identity/apikey`, `identity/postgres`, `organization/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `outbox`, `audit`
- Unlocks after verification: `identity/reference`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and start only from the
coordinator's committed assignment with both prerequisites verified. Build the
PostgreSQL API-key record, digest lookup, permissions, metadata, quota/refill,
rotation and revocation adapter.

## Ownership and public contract

The adapter owns key metadata/schema, secret digest/prefix, configuration ID,
user/organization ownership, permissions, expiry, rotation lineage,
rate/quota/refill state, usage timestamps/counters, migrations, cleanup and
reconciliation. It never generates or returns raw keys after the core's
reveal-once result and does not decide authorization policy.

## Required behavior and evidence

- Digest and prefix collisions MUST be handled safely; authentication MUST
  select candidates by bounded prefix/configuration and verify the complete
  digest without timing-dependent early acceptance.
- Create/rotate MUST atomically persist only digest metadata and return a known
  commit result before the core reveals success. Unknown commits MUST NOT cause
  a second raw secret to represent the same intended rotation silently.
- The adapter MUST persist an idempotency key, configuration revision and
  issuance/rotation operation record sufficient to distinguish not committed,
  committed and unknown without persisting plaintext. A status lookup MUST be
  owner-scoped and enumeration-safe. Recovery from lost reveal or ambiguous
  commit MUST support explicit revoke-and-reissue/rotate and MUST never
  reconstruct or re-reveal the original secret.
- Get/list/update/delete/delete-expired MUST enforce owner/tenant scope, stable
  pagination and optimistic versioning. Ownership changes are forbidden.
- Permission and typed metadata updates MUST validate schemas and preserve
  explicit empty versus inherit semantics without privilege broadening.
- Quota/remaining/refill and optional rate limits MUST update atomically by
  database time under concurrent verification. Usage observation MUST be
  write-amplification bounded and MUST NOT turn authentication success unknown.
- This adapter MUST implement the public `identity/apikey.AtomicVerifier` as
  the durable verification and quota authority. `VerifyAndConsume` MUST insert
  or lock the exact attempt ID and canonical fingerprint, validate current key
  and owner authority, compute refill and apply at most one debit in one
  PostgreSQL transaction. A matching retry MUST return the recorded result
  without another digest comparison or debit; a fingerprint mismatch MUST
  conflict before mutation.
- A known rollback or failure before transaction submission MUST return
  `not-debited`; a successful profile with no finite quota MUST return
  `not-applicable`. Loss of connection or acknowledgement after submission,
  when a positive debit's commit cannot be proved, MUST return
  `possible-debit` and `apikey.ErrDebitOutcomeUnknown` carrying the attempt ID;
  it MUST NOT be translated to unavailable or invalid credential and MUST NOT
  trigger another store/fallback attempt.
  `ReconcileVerifyAndConsume` MUST perform an owner- and tenant-scoped read of
  the durable attempt/result only, never rerun verification, refill or debit,
  and preserve `possible-debit` until PostgreSQL can prove the terminal state.
  Cleanup MUST retain attempt fingerprints and redacted terminal results for at
  least the configured retry/idempotency horizon and protect them from key-row
  expiry or deletion.
- The exported PostgreSQL store MUST implement the core `identity/apikey.Store`
  signatures exactly, including
  `ApplyPermissionReduction(context.Context, apikey.PermissionReductionDecision) (apikey.PermissionReductionResult, error)`
  and
  `ReconcilePermissionReduction(context.Context, apikey.PermissionReductionReconcileQuery) (apikey.PermissionReductionResult, error)`.
  Transaction participation MUST be a separate
  `PermissionReductionContributor` with
  `StagePermissionReduction(context.Context, identitypostgres.Work, apikey.PermissionReductionDecision) (apikey.PermissionReductionResult, error)`;
  the store implementation MUST delegate through an already enlisted
  contributor and MUST NOT open an independent transaction. Reconciliation
  MUST read the primary command journal by the exact original command ID and
  fingerprint, MUST NOT re-decide or reapply the transition, and MUST return no
  post-transition record while the outcome remains unknown.
- Revocation/expiry/rotation MUST win against concurrent verification according
  to a documented isolation point and propagate to any API-key session.
- Verification MUST join or transactionally validate the key's current owner,
  tenant/organization membership, authority version, policy ceiling,
  configuration revision and revocation/expiry state. Cached or denormalized
  permissions MAY only narrow that current authority. Configuration lookup MUST
  distinguish active, verify-only, deprecated, disabled and missing revisions
  without prefix-based fallback.
- Cleanup MUST use bounded indexed batches and retain required rotation/audit/
  issuance and verification/debit unknown-outcome evidence.
- Real PostgreSQL races, disconnects, migrations from older key formats,
  mixed binaries, backup/restore and query plans are REQUIRED.

The adapter's operations MUST preserve [`API_OPERATIONS.md`](../API_OPERATIONS.md)
authorization, [`TRANSACTION_CONTRACT.md`](../TRANSACTION_CONTRACT.md) outcome
classification, [`LIFECYCLE_CASCADES.md`](../LIFECYCLE_CASCADES.md) invalidation
ordering and [`REFERENCE_CONFIGURATION.md`](../REFERENCE_CONFIGURATION.md)
revision semantics.
Auditable transitions and denial records MUST preserve the outbox and field
contract in [`SECURITY_EVENTS.md`](../SECURITY_EVENTS.md).

Exact coverage/mutation, race, query/lock benchmarks, clean-consumer,
API/docs/changelog and supply-chain gates are REQUIRED. Raw-key persistence,
quota races, fallback resurrection or cross-owner access blocks verification.
