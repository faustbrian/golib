# Goal: pkg/identity/mfa

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals, as shown here.

## Execution metadata

- Unit: `identity/mfa`
- Canonical module: `pkg/identity/mfa`
- Canonical goal after scaffolding: `pkg/identity/mfa/.ai/GOAL.md`
- Requires: `identity`, `identity/session`, `identity/otp`, `identity/risk`, `webauthn`
- Consumes existing primitives: `password`, `secret-envelope`, `audit`, `rate-limit`
- Unlocks after verification: `identity/mfa/postgres`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/mfa` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/mfa` module that owns MFA enrollment, TOTP, challenge policy, recovery codes, trusted devices, and factor lifecycle. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns MFA enrollment, TOTP, challenge policy, recovery codes, trusted devices, and factor lifecycle. It does not own WebAuthn protocol, passkey UX, SMS provider transport, and primary authentication. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define Factor, Enrollment, TOTPProfile, SecurityKeyProfile, WebAuthnVerifier, Challenge, RecoverySet, TrustedDevice, StepUpPolicy, Store, and Service contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST confirm enrollment before activation; verify TOTP skew and replay; rotate recovery codes atomically; revoke trusted devices; require freshness for factor changes; prevent factor removal from bypassing required MFA; recover without enumeration. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Enable/disable MUST require recent primary authentication and password or
  equivalent configured proof, create a pending enrollment, and activate only
  after a valid second-factor confirmation.
- TOTP MUST expose a standards-compliant provisioning URI with issuer/account
  label policy, encrypted secret storage, supported algorithm/digits/period,
  bounded skew and per-step replay prevention using pinned RFC vectors.
- OTP second factor MUST use the OTP purpose contract, distinct send/verify
  limits and no fallback that silently reduces a required TOTP/security-key
  policy.
- WebAuthn security-key factors MUST use verified non-discoverable or explicitly
  allowed discoverable assertions from `webauthn`, bind the credential to the
  pending factor/user/RP and persist cryptographic state only through
  `webauthn/postgres`. A passkey sign-in credential MUST not automatically
  become an enrolled second factor.
- Recovery codes MUST be unbiased, shown once, digest-stored, atomically
  single-use, listable only as safe status/count and fully replaced on
  regeneration. Viewing plaintext codes after creation is forbidden.
- Trusted devices MUST use a bound, revocable, expiring credential with device
  metadata privacy, key rotation and risk invalidation; a cookie flag alone is
  insufficient.
- Challenge state MUST cap total attempts across factor methods and account for
  concurrency so switching methods cannot reset lockout.
- Factor list, rename/remove, recovery and administrator reset MUST preserve at
  least one policy-compliant recovery path, revoke relevant sessions/trusted
  devices and emit immutable audit.

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
