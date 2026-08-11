# Goal: pkg/identity/risk/captcha

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/risk/captcha`
- Canonical module: `pkg/identity/risk/captcha`
- Canonical goal after scaffolding: `pkg/identity/risk/captcha/.ai/GOAL.md`
- Requires: `identity/risk`
- Consumes existing primitives: `http-client`, `audit`, `telemetry`
- Unlocks after verification: `identity/risk/captcha/recaptcha`, `identity/risk/captcha/turnstile`, `identity/risk/captcha/hcaptcha`, `identity/risk/captcha/captchafox`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/risk/captcha` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/risk/captcha` module that owns provider-neutral server-side CAPTCHA verification and normalized risk signals. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns provider-neutral server-side CAPTCHA verification and
normalized evidence shared by reCAPTCHA, Turnstile, hCaptcha and CaptchaFox
adapters. It does not own browser widgets, provider account management,
provider-specific wire fields, risk decisions, fail-open/fail-closed choices,
or deciding when a challenge is required. Provider-specific fields MAY survive
only in a bounded, typed evidence envelope. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define Verifier, Request, Result, Failure, ProviderEvidence, provider capability metadata, hostname/origin/action/site binding, optional score policy, clock, retry classification, and redaction contracts. It MUST represent unavailable provider fields without fabricating generic values. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST normalize success and failure without discarding attributable provider evidence; bind configured hostname/origin, action and site; apply score only when supported; reject replay where providers expose identifiers; distinguish invalid token, provider outage, throttling, cancellation, malformed response, unsupported capability, ambiguous transport outcome and policy mismatch; and never accept client claims without provider verification. Every adapter MUST return normalized evidence and MUST NOT return allow, deny, throttle or step-up. The consuming operation MUST apply `.ai/identity-platform/REFERENCE_CONFIGURATION.md`: protected signup/signin/reset/credential-change ambiguity is deny; low-risk read ambiguity is step-up only when an independent configured factor exists and otherwise deny; no unavailable or unknown result becomes allow. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Configuration MUST select an exact provider, API/version and product tier and
  MUST declare the site key, allowed canonical hostname/origin set, expected
  action when supported, and whether remote client address disclosure is
  permitted. Construction MUST fail when a required binding is absent or a
  configured field is unavailable for that profile.
- A verification request MUST bind a trusted operation identifier, tenant,
  action, purpose, subject scope, challenge identifier, provider/profile/site
  key and replay/idempotency identifier. Tokens and evidence MUST NOT be reused
  under a different binding. Caller-supplied action, host or origin labels MUST
  NOT replace trusted server configuration.
- No caller or provider adapter may supply, override, or select the
  authoritative replay fingerprint. The `identity/risk` issuance authority
  derives it from the raw token and trusted provider/site/profile scope; this
  package passes the token only through the bounded verification request and
  returns no reusable replay identity.
- Normalized evidence MUST retain provider, API/version, tier, site binding,
  action/hostname/origin availability and match status, challenge timestamp,
  score availability/value, provider reason codes and transport outcome. It
  MUST distinguish absent from empty and MUST NOT fabricate unsupported fields.
- Verification, rejection, replay, provider-unavailable and binding-mismatch
  events MUST use `.ai/identity-platform/SECURITY_EVENTS.md`; challenge expiry,
  tenant/site disablement and secret rotation MUST follow
  `.ai/identity-platform/LIFECYCLE_CASCADES.md`.

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
