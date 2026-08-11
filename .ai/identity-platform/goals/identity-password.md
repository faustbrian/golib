# Goal: pkg/identity/password

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/password`
- Canonical module: `pkg/identity/password`
- Canonical goal after scaffolding: `pkg/identity/password/.ai/GOAL.md`
- Requires: `identity`, `identity/session`, `identity/risk`, `identity/delivery`
- Consumes existing primitives: `password`, `capability`, `capability/postgres`, `workflow`, `audit`, `rate-limit`
- Unlocks after verification: `identity/password/postgres`, `identity/username`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/password` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/password` module that owns registration, password signin, change, reset, upgrade, and credential-compromise response. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns registration, password signin, change, reset, upgrade, and credential-compromise response. It does not own password hashing primitives, UI forms, session persistence adapters, and delivery providers. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define Service, RegistrationPolicy, SignInPolicy, ResetProfile, ChangeRequest, PasswordVerifier, ReauthenticationProof, SessionIssuer, Delivery, and enumeration-safe result contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST register transactionally; sign in and upgrade hashes; deny suspended accounts; reset through single-use capability; revoke sessions on compromise; require freshness for change; prevent identifier enumeration and reset races. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Signup MUST support explicit enabled/disabled policy, required verified email
  policy, minimum/maximum password byte length, risk/HIBP decision, identity
  fields permitted at signup and optional post-commit session issuance.
- Signin MUST canonicalize the identifier through its owner, perform
  constant-work dummy verification for unknown identities, enforce status/
  verification/risk/MFA policy, upgrade hashes atomically and return
  indistinguishable public failures.
- Set-password for provider-only accounts MUST require recent authenticated
  authorization or a verified reset capability and MUST NOT allow an OAuth
  subject alone to choose a password.
- Change-password MUST verify the current password unless a stronger explicit
  recovery/admin contract applies, check risk/HIBP, prevent prohibited reuse,
  update hash and optionally revoke other/all sessions atomically.
- Forgot/reset MUST always return enumeration-safe request results, bind token
  to user/password version/purpose/tenant, handle email delivery unknowns,
  consume exactly once under races and revoke configured sessions after commit.
- Administrator set/reset MUST be a separate authorized operation with audit,
  forced-change/session policy and no ability to retrieve either old or new
  password.
- Password bytes MUST NOT be normalized, trimmed, copied into immutable logs or
  retained beyond verification/hashing. Hash parameters and upgrade policy MUST
  be bounded against denial of service.
- Sensitive change/set operations MUST consume an explicit reauthentication
  proof bound to subject, tenant, session/version, authentication method,
  assurance, action and maximum age; a session timestamp or caller-supplied
  `recent=true` flag is insufficient. Exact freshness defaults are owned by
  `.ai/identity-platform/REFERENCE_CONFIGURATION.md`.
- Reset links MUST use read-only Validate followed by explicit user confirmation
  and the Reserve/Apply/Finalize capability workflow in
  `.ai/identity-platform/TRANSACTION_CONTRACT.md`. Validation by mail scanners
  MUST NOT consume or authorize reset, and capability consumption, password
  mutation and required session invalidation MUST have one recoverable outcome.
- Registration, hash upgrade, change and reset compositions with identity,
  delivery and session modules MUST use the same coordinator contract; callback
  ordering MUST NOT be presented as cross-module atomicity.
- Password verification MUST be able to issue a bounded, single-purpose
  reauthentication proof without creating or extending a session. The proof
  MUST bind tenant, subject, source session/version, method, assurance, action,
  audience and expiry; issuance/validation MUST be rate-limited, replay-aware
  and reject cross-purpose use as specified by
  `.ai/identity-platform/API_OPERATIONS.md`.
- Signup and signin MUST accept the session-owned persistent or non-persistent
  remember policy and preserve it through risk/MFA continuation and
  SessionIssuer input. Absence MUST use the explicit default in
  `.ai/identity-platform/REFERENCE_CONFIGURATION.md`, never silently upgrade a
  non-persistent choice.

## Security and abuse requirements

- When handling `identity.otp.password-reset` or
  `identity.phone.password-reset-complete`, this workflow MUST
  reserve/apply/finalize the purpose-bound OTP through `identity/otp/postgres` in
  the same coordinator unit of work as its owning mutation. Signup, signin,
  password change, and capability-only reset MUST NOT enlist an OTP participant.
  Release and recovery remain fail-closed on rollback or unknown commit.

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
