# Goal: pkg/identity/phone

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/phone`
- Canonical module: `pkg/identity/phone`
- Canonical goal after scaffolding: `pkg/identity/phone/.ai/GOAL.md`
- Requires: `identity`, `identity/otp`, `identity/delivery`, `identity/risk`
- Consumes existing primitives: `identifier`, `audit`, `workflow`
- Unlocks after verification: `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/phone` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/phone` module that owns phone-number canonicalization, ownership verification, primary-number changes, and verified-number lifecycle. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns phone-number canonicalization, ownership verification, primary-number changes, and verified-number lifecycle. It does not own SMS transport, SIM-swap/carrier intelligence, TOTP, and telephony UI. It consumes only immutable `RiskEvidence` issued by `identity/risk`; caller-supplied carrier facts or decisions are forbidden. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define NumberPolicy, VerificationService, ChangePolicy, OTPProfile, country metadata/version policy, and result contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The module MUST implement the version-1 privacy-export contributor contract
for canonical phone identifiers and verification history, and MUST participate
in identity anonymization and deletion cascades without exporting provider risk
payloads.
When the reference PostgreSQL profile is selected, `identity/postgres` MUST
persist the phone anonymization/deletion checkpoint and privacy-export fragment
for the exact tenant, subject, snapshot ID, policy version, contributor version,
content digest, and terminal outcome in the owning coordinator transaction.

The implementation and tests MUST canonicalize E.164 with pinned metadata; reject ambiguous input; race claims; verify ownership by purpose-bound OTP; replace safely; preserve tenant isolation; define recycling and re-verification policy. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Operations MUST include send verification OTP, verify, signup-on-verification
  when enabled, signin, update, remove and request password-reset OTP with
  purpose separation. Phone operations do not expose session suppression.
- Number parsing MUST use pinned metadata, require explicit default region when
  national input is allowed, produce E.164 canonical form, preserve bounded
  display form and reject extensions/short codes/ambiguous inputs unless a
  named profile supports them.
- Verification and signin MUST enforce code attempts, send limits, number
  uniqueness, account status, risk and required-verification policy without
  revealing whether the number exists.
- Update/remove MUST require recent authentication, verify the new number,
  notify old channels where policy requires, resolve collision races and
  preserve a configured recovery method.
- Number recycling/porting risk, metadata upgrades and previously valid numbers
  MUST have explicit re-verification and migration policy.
- Sender and custom verifier callbacks MUST NOT be treated as proof until the
  owning OTP/identity transition commits and MUST preserve unknown outcomes.
- Phone-based password recovery MUST be an explicit, separately configurable
  recovery profile, disabled unless the deployment accepts the stated SIM-swap,
  number-recycling and carrier risks. It MUST require the canonical reset
  capability, a purpose-bound phone OTP, one eligible independent factor, and
  immutable `RiskEvidence` whose decision permits recovery. The evidence MUST
  be issued by `identity/risk`, fresh, one-use, and bound to tenant, subject,
  recovery operation/purpose, canonical number, pre-auth transaction, attempt
  ID, and risk-policy version. Positive, unknown, unavailable, stale,
  mismatched, replayed, or caller-supplied carrier evidence MUST deny. A
  verified phone identifier is never unconditional recovery authority.
  Outcomes remain enumeration-safe and a successful reset invalidates sessions.
- Phone verification/change/recovery MUST compose OTP consumption, identity
  mutation and session effects through
  `.ai/identity-platform/TRANSACTION_CONTRACT.md`; exact enablement and risk
  defaults belong to `.ai/identity-platform/REFERENCE_CONFIGURATION.md`.
- Phone password-reset completion MUST use one coordinator command and unit of
  work to reserve and finalize RiskEvidence with the purpose-bound OTP, reset
  capability, password mutation, and session invalidation. This package MUST
  expose contributor interfaces for those effects without importing or naming
  concrete persistence adapters. Failure, retry, unknown commit, and recovery
  MUST follow the shared participant state machines and MUST never consume only
  a subset or permit another command to reuse evidence.
- Phone password-reset initiation MUST use one coordinator command and unit of
  work to reserve, apply, and finalize initiation-only RiskEvidence with
  purpose-bound OTP challenge and canonical reset capability issuance. The
  evidence purpose MUST be `phone-password-reset-initiate`; challenge,
  capability, outbox/audit, and command-result writes MUST commit together with
  its finalization. Same-command replay returns the recorded result, concurrent
  commands have one reservation winner, takeover is generation-CAS fenced, and
  rollback or unknown outcome MUST follow the shared release and recovery rules.
  Completion MUST require purpose `phone-password-reset-complete`. Initiation
  and completion MUST use separate purpose-bound RiskEvidence artifacts and MUST
  NOT validate, reserve, replay, or substitute one for the other.
- Recent authentication for number replacement/removal MUST be an explicit
  proof bound to subject, tenant, session/version, action, assurance and maximum
  age; a timestamp or caller-provided freshness flag is not proof.
- Phone signup/signin continuations MUST preserve the explicit persistent or
  non-persistent remember policy supplied to the owning OTP or session flow;
  fallback or MFA MUST NOT upgrade it. Password-reset continuations issue no
  session and therefore carry no remember or suppression choice.
- Public signup/signin initiation MUST create or use the canonical single-use
  pre-auth transaction and bind tenant, purpose, canonical number and resolved
  `RememberPolicy`; later verification/signin MUST consume that exact binding.
  Session-authenticated number-change challenges MUST NOT create or substitute
  a public pre-auth transaction and instead bind the current subject, session
  and identifier version.

## Security and abuse requirements

- When handling `identity.phone.verify`, `identity.phone.signin`,
  `identity.phone.update`, or `identity.phone.password-reset-complete`, this
  workflow MUST reserve/apply/finalize the purpose-bound OTP through the
  injected OTP transaction contributor in the same coordinator unit of work as its owning
  mutation. Non-consuming initiation/removal operations MUST NOT enlist an OTP
  participant. Release and recovery remain fail-closed on rollback or unknown
  commit.

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
