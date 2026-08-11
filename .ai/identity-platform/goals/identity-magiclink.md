# Goal: pkg/identity/magiclink

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/magiclink`
- Canonical module: `pkg/identity/magiclink`
- Canonical goal after scaffolding: `pkg/identity/magiclink/.ai/GOAL.md`
- Requires: `identity`, `identity/session`, `identity/email`, `identity/risk`
- Consumes existing primitives: `capability`, `capability/postgres`, `workflow`, `audit`, `rate-limit`
- Unlocks after verification: `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/magiclink` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/magiclink` module that owns passwordless email signin and optional signup through single-use links. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns passwordless email signin and optional signup through single-use links. It does not own email verification policy, mail transport, generic token mechanics, and UI. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define RequestPolicy, CapabilityProfile, Callback, SessionIssuer, SignupPolicy, RedirectPolicy, and result contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST request without enumeration; bind intended action and redirect; consume exactly once; reject link scanners without accidental consumption policy gaps; apply risk and session issuance atomically; revoke superseded attempts. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Request MUST support signin-only and signup-if-allowed profiles, expiration,
  resend/supersession, callback URL allowlist and enumeration-safe delivery.
- The capability MUST bind canonical identifier, tenant, action, redirect,
  issue time, expiry, key/token version and maximum uses. Stored lookup/replay
  state MUST NOT contain the raw link token.
- Signing, parsing, time validation, key rotation, revocation and atomic
  single-use consumption MUST use the public `capability` contracts and their
  selected existing store adapter. This module MUST NOT create a second token
  format, consumption table, replay cache or ambiguous fallback authority.
- Verification/inspection MUST allow UI or link-scanner policy to inspect
  validity without consuming authentication exactly when configured; consume
  MUST remain atomic and single-winner through `capability`. Inspection MUST
  NOT decrement, reserve or otherwise mutate the consumption record.
- Callback MUST distinguish expired, superseded, already consumed, invalid,
  risk denied, signup disabled and unknown commit without revealing whether an
  account pre-existed.
- Session issuance and optional identity creation MUST occur only after token
  consumption and identity transaction can be reconciled; a retry after
  unknown commit MUST NOT create a second user or session family.
- Redirects MUST be exact-bound/allowlisted and MUST never copy the bearer link
  into referrer-visible or third-party URLs.

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
