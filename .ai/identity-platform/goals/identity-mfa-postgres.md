# Goal: pkg/identity/mfa/postgres

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/mfa/postgres`
- Canonical module: `pkg/identity/mfa/postgres`
- Canonical goal after scaffolding: `pkg/identity/mfa/postgres/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/mfa/postgres:v1`; owned operation IDs: none
- Requires: `identity/mfa`, `identity/postgres`, `webauthn/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `capability/postgres`, `secret-envelope`, `outbox`, `audit`
- Unlocks after verification: `identity/reference`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and start only from the
coordinator's committed assignment with all listed prerequisites verified. Build the
durable PostgreSQL factor, enrollment, TOTP replay, recovery-code and trusted-
device store required by MFA.

## Ownership and public contract

The adapter owns factor/enrollment schema, encrypted TOTP secrets, factor
versions, last accepted TOTP step, recovery-code digests, trusted-device
digests/metadata, challenge attempt state, locking, cleanup, migration and
reconciliation. It owns the durable continuation state and its atomic completion
as the `identity/mfa` store participant, while `capability/postgres` exclusively
owns capability reservation, finalization and recovery rows. It stores only
references to WebAuthn security-key credentials owned by `webauthn/postgres`;
it does not verify factors, decide step-up, issue sessions or duplicate WebAuthn
credential records.

## Required behavior and evidence

- Pending enrollment and active factor MUST be distinct states; activation,
  replacement and removal MUST use optimistic/row locking and atomic events.
- TOTP secrets MUST use `secret-envelope` with user/factor/tenant context and
  key rotation. Plain secrets MUST never appear in database diagnostics,
  evidence or backups outside encrypted test fixtures.
- Last accepted TOTP step MUST update atomically with success so the same step
  cannot win concurrently or after process restart.
- Recovery codes MUST be digest-stored, consumed with one winner, regenerated
  as a whole versioned set and listable only as safe count/status.
- Trusted-device credentials MUST be digest-stored, expiring, independently
  revocable and bound to factor/user/tenant/version. Revocation and compromise
  version increments MUST propagate deterministically.
- Challenge attempts across methods MUST share the configured account/challenge
  cap so method switching cannot reset limits.
- Disable/reset MUST atomically remove or revoke factors, recovery codes and
  trusted devices and emit the required session-revocation/outbox effects.
- Real PostgreSQL tests and migrations MUST cover all races, unknown commits,
  key rotation, populated rows, mixed binaries, cleanup and backup/restore.
- The adapter MUST persist a versioned MFA continuation supplied or advanced by
  the owning MFA service and bound to tenant, user, source session/pre-auth
  transaction, required assurance, allowed methods, attempts, expiry and the
  original persistent/non-persistent remember policy. It MUST implement only
  the public continuation contract defined by `identity/mfa` and remain blocked
  if that contract is absent. Continuations MUST be digest-protected, single-
  completion and unusable as authenticated sessions before that service
  finalizes them; the adapter MUST NOT alter remember policy.
- Factor activation/replacement/removal, continuation completion, recovery-code
  consumption, trusted-device issuance/revocation and required session effects
  MUST implement `.ai/identity-platform/TRANSACTION_CONTRACT.md`; unknown
  commits MUST be recoverable without replaying a factor or code.
- Continuation completion MUST treat capability validation as read-only and MUST
  use the enlisted `capability/postgres` participant to reserve the existing
  proof before applying the MFA transition and invoking the transaction-aware
  session issuer; it MUST finalize the capability and continuation in the same
  authoritative unit-of-work commit. The adapter MUST NOT create a capability
  row, consume one in a standalone transaction, or infer completion from a
  pre-consumed proof.
- Trusted devices MUST bind a random digest-stored credential to tenant, user,
  device, factor/global compromise versions and the MFA-service policy for the
  assurance it may satisfy. They MUST have explicit expiry, rotation and
  individual/global revocation, MUST never bypass a method or action that
  requires fresh user verification, and MUST follow authority invalidation in
  `.ai/identity-platform/LIFECYCLE_CASCADES.md` with audit outcomes from
  `.ai/identity-platform/SECURITY_EVENTS.md`.

Exact coverage/mutation, race, query/lock benchmarks, clean-consumer,
API/docs/changelog and supply-chain gates are REQUIRED. Recoverable plaintext,
replayed factors/codes or non-atomic disable/reset blocks verification.
