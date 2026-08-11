# Goal: pkg/identity/otp/postgres

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

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

The adapter owns the durable OTP participant state machine: `issued`,
`reserved`, `finalized`, `released`, `expired`, `revoked`, and `exhausted`.
Owning workflows decide their domain mutation, but no other store may reserve,
finalize, release, recover, or erase the OTP replay record.

## Required behavior and evidence

- Raw OTP codes MUST never be stored. Digest lookup MUST bind tenant, purpose,
  subject, challenge ID and configured pepper/key version with rotation policy.
- Issue/resend MUST atomically enforce window/counter policy and replace or
  retain earlier challenges exactly as declared by the profile.
- Incorrect attempt, correct verification, check-without-consume and consume
  MUST serialize so concurrency cannot exceed attempts or produce two winners.
- Purpose, identifier and tenant mismatches MUST have enumeration-safe public
  behavior and MUST NOT decrement another challenge's counters.
- Database time MUST own expiry/window boundaries. Cleanup MUST use bounded
  indexed batches and preserve evidence needed for replay/lockout windows.
- Unknown issue/attempt/consume commits MUST be reconcilable by stable command
  ID and MUST never be translated to clean failure or success arbitrarily.
- Real PostgreSQL tests MUST cover concurrent correct/incorrect attempts,
  resend races, lockout, expiry boundaries, failover/disconnect, deletion and
  task-owned delivery integration.
- Migrations MUST cover active challenges/counters, algorithm/key-version
  change, mixed binaries, interrupted cleanup and backup/restore.
- Digest storage MUST use a keyed, versioned construction over the complete
  tenant/purpose/subject/channel/challenge/code tuple; database exposure MUST
  not permit offline enumeration of the configured small code space. Rotation
  MUST support bounded active key versions without ambiguous lookup.
- Issue, reserve, attempt, consume and owning-workflow finalization MUST expose
  the participant and recovery semantics in
  `.ai/identity-platform/TRANSACTION_CONTRACT.md`; a row marked consumed before
  an uncommitted owning transition MUST remain recoverable rather than become a
  lost credential.
- Purpose-bound reservation MUST run through the predeclared contributor in the
  coordinator's single reservation transaction, with one winner and
  same-command/fingerprint/generation idempotency. Takeover MUST CAS the exact
  prior generation with every one-time participant; unknown completion remains
  `reserved` until authoritative recovery. Apply/finalize MUST share the owning
  workflow's domain commit; release requires proven non-commit and is terminal.
- Cleanup MUST exclude unresolved reservations and retain a restricted terminal
  digest/state tombstone through key retirement so expiry or payload erasure
  cannot make a code reusable.

Exact coverage/mutation, race, query/lock benchmarks, clean-consumer,
API/docs/changelog and supply-chain gates are REQUIRED. Any raw-code storage,
attempt bypass, replay or unclassified commit ambiguity blocks verification.
