# Goal: pkg/identity/risk/captcha/captchafox

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/risk/captcha/captchafox`
- Canonical module: `pkg/identity/risk/captcha/captchafox`
- Canonical goal after scaffolding: `pkg/identity/risk/captcha/captchafox/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/risk/captcha/captchafox:v1`; owned operation IDs: none
- Requires: `identity/risk/captcha`
- Consumes existing primitives: `http-client`, `audit`, `telemetry`
- Unlocks after verification: `identity/reference`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and MUST start only after
the coordinator marks this unit `in-progress` with its prerequisite verified.
Build an independently releasable CaptchaFox verification adapter with exact
provider semantics and conversion to provider-neutral CAPTCHA evidence.

## Ownership and public contract

The module owns CaptchaFox configuration, verification endpoint interaction,
request/response schemas, site and hostname binding, provider error mapping,
timeout classification and safe telemetry. It does not own widgets, provider
accounts, generic risk decisions, application HTTP handlers, or other CAPTCHA
providers. It MUST NOT own fail-open/fail-closed policy. Public configuration
MUST validate the secret, site key, allowed hostnames, endpoint policy, request
limits and remote-client metadata
before network use. Public results MUST distinguish solved, rejected,
unavailable, malformed, throttled, cancelled and ambiguous outcomes without
exposing request tokens or provider bodies.

Configuration MUST select the CaptchaFox Siteverify API using
`POST https://api.captchafox.com/siteverify` with form fields `secret`,
`response`, optional `sitekey`, and optional case-sensitive `remoteIp`. It MUST
name the exact pinned API revision and Free/paid or Enterprise product tier,
the configured site key, and the canonical allowed hostname set. The adapter
MUST interpret `success`, optional `challenge_ts`, optional `hostname`,
failure-only `error-codes`, and Enterprise-only `insights` exactly as documented
for that revision/tier. CaptchaFox Siteverify does not document generic action
or score fields; configuration MUST reject those bindings unless a later pinned
contract adds them. Fields absent from the selected tier MUST remain unavailable
rather than fabricated values.
The reference selection MUST consume the exact API, version, tier, site key,
hostname/origin allowlists, expected-action support, and remote-IP disclosure
fields from `REFERENCE_CONFIGURATION.md`; it MUST NOT infer provider defaults.

## Required behavior and security

The verifier MUST apply bounded contexts and bodies; serialize exactly the
pinned CaptchaFox contract; enforce configured site and hostname and reject any
documented action or score fields; conservatively reject missing, duplicated,
unknown-critical or inconsistent fields; and preserve documented error codes
as redacted categories. Normalized evidence MUST preserve the exact API
version/tier, configured site-key binding, returned hostname availability and
match status, challenge timestamp, success status, reason codes and bounded
Enterprise `insights`. It MUST NOT claim provider-returned site-key, origin,
action or score evidence where the pinned contract does not return it. Provider
outage MUST never be converted to a valid solve by the adapter.
The adapter MUST NOT decide allow, deny, throttle, step-up, or provider-failure
policy; unavailable/unknown handling MUST come from
`.ai/identity-platform/REFERENCE_CONFIGURATION.md`. Retry policy MUST account
for token single-use and ambiguous requests.

Secrets and response tokens MUST never occur in URLs, diagnostics, telemetry,
fixtures or evidence. Endpoint override MUST be opt-in and SSRF-safe. Tests
MUST cover hostname canonicalization, IDNs, ports, suffix attacks, replay,
substitution, oversized JSON, duplicate keys, unexpected content types,
partial reads, cancellation, timeouts, throttling and concurrent use.

Each request and evidence result MUST bind the trusted operation, tenant,
action, purpose, subject scope, challenge, selected tier/site key and replay
identifier. Token or evidence replay under another binding MUST fail. CAPTCHA
verification, rejection, replay and binding-mismatch records MUST NOT be
emitted by this adapter; `identity/risk/captcha` owns those records after it
consumes the normalized evidence. Provider telemetry remains adapter-owned.
Challenge expiry, tenant/site disablement and secret rotation MUST follow
`.ai/identity-platform/LIFECYCLE_CASCADES.md`.

The adapter MUST NOT derive or return the authoritative replay fingerprint. It
exposes only normalized verification facts; `identity/risk` derives the durable
keyed replay identity from the raw token and trusted scope.

## Acceptance and blockers

The unit requires exact coverage/mutation, parser fuzz, race, bounded-resource
benchmark, clean-consumer, API baseline, docs/examples/changelog, dependency,
license, vulnerability, secret, SBOM and provenance gates. Provider proof MUST
include pinned official fixtures and documented CaptchaFox sandbox/live
interoperability. The evidence MUST name product-tier differences rather than
pretend unavailable fields exist.

The unit MUST remain unverified if it leaks bearer material, accepts an
unbound origin/site, treats unknown or provider failure as solved, relies only
on a fake, or lacks current proof for the declared CaptchaFox profile.
