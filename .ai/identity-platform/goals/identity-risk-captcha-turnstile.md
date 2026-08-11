# Goal: pkg/identity/risk/captcha/turnstile

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/risk/captcha/turnstile`
- Canonical module: `pkg/identity/risk/captcha/turnstile`
- Canonical goal after scaffolding: `pkg/identity/risk/captcha/turnstile/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/risk/captcha/turnstile:v1`; owned operation IDs: none
- Requires: `identity/risk/captcha`
- Consumes existing primitives: `http-client`, `secret-envelope`, `telemetry`
- Unlocks after verification: `identity/reference`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/risk/captcha/turnstile` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/risk/captcha/turnstile` module that owns Cloudflare Turnstile Siteverify adapter. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns Cloudflare Turnstile Siteverify adapter. It does not own widget rendering, key provisioning, generic risk policy, and non-Cloudflare CAPTCHA. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define configuration, secret ownership, idempotency key, remote IP policy, hostname/action/cdata mapping, error mapping, timeout, normalized provider evidence, and telemetry contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST reject hostname or action mismatch; handle duplicate, expired, and idempotent verification; bound JSON; test malformed, partial, throttled, and timed-out responses; prove live test-key or documented provider interoperability. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Verification MUST use `POST https://challenges.cloudflare.com/turnstile/v0/siteverify`
  with `secret`, `response`, optional `remoteip`, and optional
  `idempotency_key` according to explicit privacy/retry policy. Configuration
  MUST select the supported Turnstile Siteverify v0 profile and widget/site key,
  canonical hostname set, expected `action`, and optional expected `cdata`.
- The reference selection MUST consume the exact API, version, tier, site key,
  hostname/origin allowlists, expected action, and remote-IP disclosure fields
  from `REFERENCE_CONFIGURATION.md`; it MUST NOT infer provider defaults.
- Success MUST enforce configured hostname, action and optional cdata binding;
  challenge timestamp/skew and any declared metadata limits MUST be validated.
- Idempotency keys MUST be unique/bounded and reused only for the same logical
  verification; they MUST NOT allow a token to authorize a different action or
  subject.
- Missing optional fields MUST remain unavailable evidence rather than empty
  trusted values. Unknown error codes and malformed duplicate JSON fields MUST
  fail conservatively.
- Secret/token/remote-IP/cdata values MUST be redacted and bounded; endpoint
  overrides MUST be opt-in and SSRF-safe.
- Cloudflare test keys/official fixtures plus documented current provider
  interoperability MUST cover positive/negative, replay/expiry, hostname/
  action/cdata mismatch, idempotent retry, timeout, throttling and cancellation.
- Normalized evidence MUST preserve the Siteverify v0 profile, configured site
  key, `success`, `challenge_ts`, `hostname`, `action`, `cdata`, `error-codes`,
  and documented `metadata` fields with explicit availability. The adapter MUST
  NOT decide allow, deny, throttle, step-up, or provider-failure policy;
  unavailable/unknown handling MUST come from
  `.ai/identity-platform/REFERENCE_CONFIGURATION.md`.
- The idempotency key and evidence MUST bind the trusted operation, tenant,
  action, purpose, subject scope, challenge, site key and token digest. Replay
  under another binding MUST fail. The adapter MUST NOT emit the canonical
  verification, rejection, replay or binding-mismatch records;
  `identity/risk/captcha` owns those records after it consumes the normalized
  evidence. Provider telemetry remains adapter-owned. Lifecycle cascades MUST
  use `.ai/identity-platform/LIFECYCLE_CASCADES.md`.
- The adapter MUST NOT derive or return the authoritative replay fingerprint.
  Any provider request idempotency value is transport evidence only;
  `identity/risk` derives the durable keyed replay identity from the raw token
  and trusted scope.

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
