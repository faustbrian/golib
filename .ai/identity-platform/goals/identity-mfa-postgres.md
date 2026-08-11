# Goal: pkg/identity/mfa/postgres

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Execution metadata

- Unit: `identity/mfa/postgres`
- Canonical module: `pkg/identity/mfa/postgres`
- Canonical goal after scaffolding: `pkg/identity/mfa/postgres/.ai/GOAL.md`
- Requires: `identity/mfa`, `identity/postgres`, `webauthn/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `secret-envelope`, `outbox`, `audit`
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
  reconciliation. It stores only references to WebAuthn security-key
  credentials owned by `webauthn/postgres`; it does not verify factors, decide
  step-up, issue sessions or duplicate WebAuthn credential records.

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

Exact coverage/mutation, race, query/lock benchmarks, clean-consumer,
API/docs/changelog and supply-chain gates are REQUIRED. Recoverable plaintext,
replayed factors/codes or non-atomic disable/reset blocks verification.
