# Goal: pkg/identity/risk/captcha/recaptcha

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/risk/captcha/recaptcha`
- Canonical module: `pkg/identity/risk/captcha/recaptcha`
- Canonical goal after scaffolding: `pkg/identity/risk/captcha/recaptcha/.ai/GOAL.md`
- Requires: `identity/risk/captcha`
- Consumes existing primitives: `http-client`, `secret-envelope`, `telemetry`
- Unlocks after verification: `identity/reference`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/risk/captcha/recaptcha` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/risk/captcha/recaptcha` module that owns Google reCAPTCHA server-verification adapter. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns Google reCAPTCHA server-verification adapter. It does not own widget rendering, key provisioning, generic risk policy, and non-Google CAPTCHA. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define configuration, secret ownership, siteverify request/response, score/action/hostname mapping, error-code mapping, timeout, normalized provider evidence, and telemetry contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST cover v2 and score-bearing responses only where documented; reject mismatched action or hostname; classify duplicate/expired tokens; bound JSON; test malformed, partial, throttled, and timed-out responses; prove live sandbox or documented provider interoperability. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Configuration MUST select and document supported reCAPTCHA v2 checkbox/
  invisible or v3 score profiles for the classic Siteverify API. The adapter
  MUST use `POST https://www.google.com/recaptcha/api/siteverify` with form
  fields `secret`, `response`, and optional `remoteip`. reCAPTCHA Enterprise
  Assessment API and enterprise-only tiers MUST be rejected unless separately
  implemented with their exact endpoint/version and evidence.
- The reference selection MUST consume the exact API, version, tier, site key,
  hostname/origin allowlists, expected action, and remote-IP disclosure fields
  from `REFERENCE_CONFIGURATION.md`; it MUST NOT infer provider defaults.
- Configuration MUST bind the selected profile, expected site key, canonical
  hostname set, and exact expected `action` for v3. Because classic Siteverify
  does not return a site key, the adapter MUST record the configured site key as
  configuration evidence and MUST NOT claim provider-returned site-key proof.
  It MUST never put secrets/tokens in URL logs or diagnostics.
- Result mapping MUST validate success, challenge timestamp/skew, expected
  hostname, action for score profiles, score presence and score range while
  preserving documented error codes and unknown codes conservatively. It MUST
  return normalized evidence without comparing the score to a policy threshold.
- This adapter MUST NOT own or apply score threshold, ambiguity, step-up,
  throttle, allow or deny policy. Those decisions and one-use evidence
  consumption belong exclusively to `identity/risk` under
  `captcha.score_owner`, `captcha.score_threshold`, `captcha.ambiguity` and
  `risk.operation_matrix`; adapter configuration MUST NOT shadow or override
  them.
- A v2 response without score/action MUST NOT fabricate them; a v3 policy MUST
  fail if required action/score evidence is missing.
- Duplicate/timeout-or-duplicate, invalid-input-secret, invalid-input-response,
  bad-request and provider/network/malformed outcomes MUST remain distinct for
  risk policy and telemetry.
- Official test keys/fixtures plus documented current provider interoperability
  MUST cover each declared profile, hostname/action mismatch, expiry/replay,
  throttling, cancellation and redaction.
- The adapter MUST return normalized evidence containing the classic API,
  selected v2/v3 profile, `success`, `challenge_ts`, `hostname`, `error-codes`,
  and v3 `score`/`action` availability and values. It MUST NOT decide allow,
  deny, throttle, step-up, or provider-failure policy; unavailable/unknown
  handling MUST come from `.ai/identity-platform/REFERENCE_CONFIGURATION.md`.
- Verification MUST bind the trusted operation, tenant, action, purpose,
  subject scope, challenge, configured site/profile and replay identifier.
  Token or evidence replay under another binding MUST fail. Security events and
  lifecycle cascades MUST use `.ai/identity-platform/SECURITY_EVENTS.md` and
  `.ai/identity-platform/LIFECYCLE_CASCADES.md` respectively.
- The adapter MUST NOT derive or return the authoritative replay fingerprint.
  It accepts the raw token only for the bounded Siteverify call; `identity/risk`
  derives the durable keyed replay identity from that token and trusted scope.

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

Verification applicability is exact for this unit: `race=required`,
`fuzz=required`, `hostile=required`, `leak=required`, `benchmark=required`,
`infrastructure=required`, and `provider_interoperability=required`; a gate
MAY be satisfied by the required composed reference evidence but MUST NOT be
silently skipped.

## Release blockers

The unit MUST remain `implemented-unverified` or `blocked` if any prerequisite
is not `verified`, any ownership boundary is unresolved, a protocol claim
lacks pinned specification and interoperability evidence, a durable transition
has unhandled ambiguity, a secret can escape redaction, or any required gate is
stale, skipped, warning-only, or failing.
