# Goal: pkg/identity/otp

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/otp`
- Canonical module: `pkg/identity/otp`
- Canonical goal after scaffolding: `pkg/identity/otp/.ai/GOAL.md`
- Requires: `identity`, `identity/session`, `identity/risk`, `identity/delivery`
- Consumes existing primitives: `capability`, `capability/postgres`, `password`, `rate-limit`, `audit`
- Unlocks after verification: `identity/otp/postgres`, `identity/phone`, `identity/mfa`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/otp` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/otp` module that owns one-time code generation, protected storage, delivery orchestration, verification, and signin profile. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns one-time code generation, protected storage, delivery orchestration, verification, and signin profile. It does not own TOTP authenticators, phone ownership, provider transport, and generic capability signing. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define CodeProfile, Challenge, Generator, Store, Delivery, Verifier, AttemptPolicy, SessionIssuer, and result contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST generate unbiased bounded codes; store only digests; consume once; cap attempts and resend; avoid enumeration; handle delayed delivery and concurrent verification; separate signin and verification purposes. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Separate purpose profiles MUST exist for signin, email verification,
  password reset, email change, MFA and generic step-up; a code issued for one
  purpose/subject/channel MUST never satisfy another.
- Code length/alphabet/entropy, expiry, resend interval, maximum sends,
  verification attempts, replacement and delivery channel MUST be validated at
  construction. Custom generators/verifiers MUST meet the same contract.
- Send MUST store only a scoped digest, invalidate or retain earlier codes by
  explicit policy, rate-limit identifier/IP/device dimensions and return an
  enumeration-safe result.
- Check-without-consume, if exposed, MUST be separately rate-limited and MUST
  not make replay or brute-force easier. Verify/consume MUST be atomic under
  concurrent correct and incorrect attempts.
- Signin/verification/reset/change-email callbacks MUST invoke the owning
  workflow and issue sessions only after that workflow commits.
- Delayed, duplicated, bounced and unknown delivery outcomes MUST have
  documented replacement and user-message behavior without logging codes.
- OTP digests MUST be keyed (for example, a versioned HMAC) over the complete
  tenant, purpose, subject/channel, challenge and code scope. A fast unkeyed
  hash of the small code space is forbidden; key rotation, lookup migration and
  retired-key behavior MUST be explicit without storing recoverable codes.
- Any link or API wrapper around an OTP challenge MUST separate scanner-safe
  read-only validation from explicit confirmation. Reserve/Apply/Finalize,
  owning workflow mutation and session issuance/invalidation MUST follow
  `.ai/identity-platform/TRANSACTION_CONTRACT.md` and MUST NOT consume on GET,
  preview or delivery-provider probing.
- Every consuming workflow MUST treat OTP precheck as non-authoritative and use
  the durable issue/attempt/reserve/apply/finalize/release/recover protocol.
  This unit owns purpose and attempt policy; `identity/otp/postgres` owns the
  authoritative state transitions and replay record.
- Signin challenges MUST bind and preserve the session-owned persistent or non-
  persistent remember policy through risk/MFA continuation and SessionIssuer
  input. Verification, resend or fallback MUST NOT upgrade persistence or
  extend the selected session lifetime.

## Security and abuse requirements

- Inputs MUST be bounded before parsing, allocation, storage, hashing, or
  cryptographic work.
- Subject, tenant, organization, purpose, audience, action, and redirect scope
  MUST be bound wherever applicable and MUST fail closed on mismatch.
- Enumeration, replay, fixation, confused-deputy, downgrade, race, and
  cross-scope attacks MUST have deterministic regression cases.
- Logs, traces, metrics, examples, fixtures, and errors MUST preserve the
  redaction requirements in `.ai/identity-platform/COMMON_REQUIREMENTS.md`.

## Persistence, lifecycle, and compatibility

The core MUST remain adapter-neutral unless this goal is itself an adapter.
State ownership, consistency, retention, deletion, migration, key rotation,
clock skew, concurrent callers, shutdown, and recovery MUST be documented and
tested where applicable. Unsupported protocol or deployment profiles MUST be
stated rather than silently approximated.

## Acceptance evidence

Before this unit becomes `verified`, the owner MUST satisfy every common gate,
the package-specific behavior above, the module's exact coverage and mutation
gates, race/fuzz/interoperability gates that apply, clean-consumer proof,
manifests, public API baseline, security and supply-chain checks, documentation,
changelog, and changed reverse-dependant gates. The final evidence record MUST
name any non-applicable gate with a reviewed reason; absence of infrastructure
or provider access is a blocker, not a pass.

## Release blockers

The unit MUST remain `implemented-unverified` or `blocked` if any prerequisite
is not `verified`, any ownership boundary is unresolved, a protocol claim
lacks pinned specification and interoperability evidence, a durable transition
has unhandled ambiguity, a secret can escape redaction, or any required gate is
stale, skipped, warning-only, or failing.
