# Goal: pkg/identity/otp/postgres

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Execution metadata

- Unit: `identity/otp/postgres`
- Canonical module: `pkg/identity/otp/postgres`
- Canonical goal after scaffolding: `pkg/identity/otp/postgres/.ai/GOAL.md`
- Requires: `identity/otp`, `identity/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `outbox`, `audit`
- Unlocks after verification: `identity/reference`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and start only from the
coordinator's committed assignment with both prerequisites verified. Build the
PostgreSQL OTP challenge, attempt, send-limit and atomic consumption adapter.

## Ownership and public contract

The adapter owns scoped digest storage, purpose/subject/channel binding,
challenge replacement/versioning, send and verify counters, expiry, lockout,
consumption transaction, cleanup and migrations. It does not generate or
deliver codes, issue sessions, decide risk, or implement the owning email,
phone, reset or MFA transition.

## Required behavior and evidence

- Raw OTP codes MUST never be stored. Digest lookup MUST bind tenant, purpose,
  subject, challenge ID and configured pepper/key version with rotation policy.
- Issue/resend MUST atomically enforce window/counter policy and replace or
  retain earlier challenges exactly as declared by the profile.
- Incorrect attempt, correct verification, check-without-consume and consume
  MUST serialize so concurrency cannot exceed attempts or produce two winners.
- Purpose, identifier and tenant mismatches MUST have enumeration-safe public
  behavior and MUST not decrement another challenge's counters.
- Database time MUST own expiry/window boundaries. Cleanup MUST use bounded
  indexed batches and preserve evidence needed for replay/lockout windows.
- Unknown issue/attempt/consume commits MUST be reconcilable by stable command
  ID and MUST never be translated to clean failure or success arbitrarily.
- Real PostgreSQL tests MUST cover concurrent correct/incorrect attempts,
  resend races, lockout, expiry boundaries, failover/disconnect, deletion and
  task-owned delivery integration.
- Migrations MUST cover active challenges/counters, algorithm/key-version
  change, mixed binaries, interrupted cleanup and backup/restore.

Exact coverage/mutation, race, query/lock benchmarks, clean-consumer,
API/docs/changelog and supply-chain gates are REQUIRED. Any raw-code storage,
attempt bypass, replay or unclassified commit ambiguity blocks verification.
