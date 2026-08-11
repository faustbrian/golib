# Goal: pkg/identity/risk/captcha/hcaptcha

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/risk/captcha/hcaptcha`
- Canonical module: `pkg/identity/risk/captcha/hcaptcha`
- Canonical goal after scaffolding: `pkg/identity/risk/captcha/hcaptcha/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/risk/captcha/hcaptcha:v1`; owned operation IDs: none
- Requires: `identity/risk/captcha`
- Consumes existing primitives: `http-client`, `audit`, `telemetry`
- Unlocks after verification: `identity/reference`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and MUST start only after
the coordinator marks this unit `in-progress` with its prerequisite verified.
Build an independently releasable hCaptcha adapter that converts hCaptcha site
verification into the provider-neutral CAPTCHA evidence contract without
leaking tokens, remote IP addresses, enterprise metadata, or provider bodies.

## Ownership and public contract

The module owns hCaptcha endpoint/configuration policy, form encoding, response
parsing, hostname/action/score/credit/enterprise-data interpretation, error-code
mapping, timeout and retry classification, and provider telemetry. It does not
own challenge rendering, risk decisions, HTTP application handlers, generic
rate limits, fail-open/fail-closed policy, or another CAPTCHA provider.

The public API MUST expose validated configuration, a bounded verifier, typed
provider evidence, provider reason codes and stable redacted failures. Site
secret, endpoint override, expected hostname/action, remote-IP disclosure and
provider-outcome classification MUST be explicit. The adapter MAY validate a
returned score's syntax and range but MUST NOT configure or apply a minimum;
`identity/risk` alone owns `captcha.score_threshold`. Endpoint overrides MUST
be opt-in and SSRF-safe.

Configuration MUST select the hCaptcha Siteverify API using
`POST https://api.hcaptcha.com/siteverify` with form fields `secret`,
`response`, optional `remoteip`, and optional expected `sitekey`. It MUST name
the exact Publisher/Pro or Enterprise product tier, configured site key and
canonical hostname set. Score, `score_reason` and other Enterprise fields MUST
be accepted only for an explicitly selected and evidenced tier; `credit` MUST
remain optional/deprecated evidence; and the Publisher/Pro profile MUST
represent score fields as unavailable. Configuration MUST reject action binding
unless the pinned selected-tier contract documents a returned action field.
The reference selection MUST consume the exact API, version, tier, site key,
hostname/origin allowlists, expected-action support, and remote-IP disclosure
fields from `REFERENCE_CONFIGURATION.md`; it MUST NOT infer provider defaults.

## Required behavior and security

The adapter MUST submit each response token at most once per verification
attempt, apply a bounded context, encode parameters exactly as documented,
reject empty/oversized tokens and malformed/oversized JSON, and validate every
configured hostname/sitekey and supported action invariant before returning
success. It MUST
preserve hCaptcha error codes as safe classifications while treating unknown
codes conservatively. Provider failure, throttling, timeout, cancellation and
ambiguous transport outcomes MUST remain distinct from a solved challenge and
from a valid negative decision. The adapter MUST return normalized evidence
containing the API/profile/tier, configured and returned site-key status,
`success`, `challenge_ts`, `hostname`, `credit`, `error-codes`, and availability
and values for `score`, `score_reason` and any documented action field. Absent
optional/deprecated fields MUST remain unavailable evidence. It MUST
NOT decide allow, deny, throttle, step-up, or provider-failure policy;
unavailable/unknown handling MUST come from
`.ai/identity-platform/REFERENCE_CONFIGURATION.md`.

Tokens and secrets MUST never enter URLs, logs, errors, traces, metrics,
fixtures or snapshots. Retries MUST NOT occur after an ambiguous accepted
request unless hCaptcha's pinned contract proves replay-safe behavior. Response
bytes, nesting, strings, errors and enterprise fields MUST be bounded before
allocation. Hostname canonicalization, IDNs, ports, suffix confusion, action
substitution, token replay, duplicated fields and clock skew MUST have
deterministic denial tests.

Each request and evidence result MUST bind the trusted operation, tenant,
action, purpose, subject scope, challenge, selected tier/site key and replay
identifier. Token or evidence replay under another binding MUST fail. The
adapter MUST NOT emit the canonical verification, rejection, replay or
binding-mismatch records; `identity/risk/captcha` owns those records after it
consumes the normalized evidence. Provider telemetry remains adapter-owned.
Challenge expiry, tenant/site disablement and secret rotation MUST follow
`.ai/identity-platform/LIFECYCLE_CASCADES.md`.

The adapter MUST NOT derive or return the authoritative replay fingerprint. It
exposes only normalized verification facts; `identity/risk` derives the durable
keyed replay identity from the raw token and trusted scope.

## Acceptance and blockers

Tests MUST cover success, negative verification, every documented error class,
hostname/action/sitekey mismatch, score boundaries, remote-IP opt-in/out,
timeouts, cancellation, throttling, malformed and oversized responses,
redaction, concurrency and endpoint validation. Parser fuzzing, bounded
allocation benchmarks, race checks, exact coverage/mutation gates, clean
consumer, API/docs/changelog and supply-chain gates are REQUIRED. Pinned
official fixtures plus documented hCaptcha sandbox/live interoperability are
REQUIRED; a fake server alone is not provider proof.

The unit MUST remain unverified if provider behavior is approximated, a token
or secret can escape, unknown/ambiguous outcomes become success, configured
binding is not enforced, or current provider interoperability is unavailable.
