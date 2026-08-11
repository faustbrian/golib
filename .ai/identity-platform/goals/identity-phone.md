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
- Requires: `identity`, `identity/otp`, `identity/delivery`
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

This module owns phone-number canonicalization, ownership verification, primary-number changes, and verified-number lifecycle. It does not own SMS transport, SIM-swap intelligence, TOTP, and telephony UI. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define NumberPolicy, VerificationService, ChangePolicy, OTPProfile, country metadata/version policy, and result contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST canonicalize E.164 with pinned metadata; reject ambiguous input; race claims; verify ownership by purpose-bound OTP; replace safely; preserve tenant isolation; define recycling and re-verification policy. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Operations MUST include send verification OTP, verify, signup-on-verification
  when enabled, signin, update, remove and request password-reset OTP with
  purpose separation and optional session suppression.
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
  number-recycling and carrier risks. It MUST require a purpose-bound OTP plus
  configured risk/step-up policy, MUST NOT equate a verified phone identifier
  with unconditional recovery authority, and MUST preserve enumeration-safe
  outcomes and session invalidation after reset.
- Phone verification/change/recovery MUST compose OTP consumption, identity
  mutation and session effects through
  `.ai/identity-platform/TRANSACTION_CONTRACT.md`; exact enablement and risk
  defaults belong to `.ai/identity-platform/REFERENCE_CONFIGURATION.md`.
- Recent authentication for number replacement/removal MUST be an explicit
  proof bound to subject, tenant, session/version, action, assurance and maximum
  age; a timestamp or caller-provided freshness flag is not proof.
- Phone signup/signin and recovery continuations MUST preserve the explicit
  persistent or non-persistent remember policy supplied to the owning OTP or
  session flow. `session suppression` MUST remain distinct from non-persistent
  session issuance, and neither may be silently upgraded by fallback or MFA.
- Public signup/signin initiation MUST create or use the canonical single-use
  pre-auth transaction and bind tenant, purpose, canonical number and resolved
  `RememberPolicy`; later verification/signin MUST consume that exact binding.
  Session-authenticated number-change challenges MUST NOT create or substitute
  a public pre-auth transaction and instead bind the current subject, session
  and identifier version.

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
