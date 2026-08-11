# Goal: pkg/identity/risk/captcha/captchafox

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Execution metadata

- Unit: `identity/risk/captcha/captchafox`
- Canonical module: `pkg/identity/risk/captcha/captchafox`
- Canonical goal after scaffolding: `pkg/identity/risk/captcha/captchafox/.ai/GOAL.md`
- Requires: `identity/risk/captcha`
- Consumes existing primitives: `http-client`, `audit`, `telemetry`
- Unlocks after verification: `identity/http`

## Start gate and objective

The worker MUST satisfy `../COMMON_REQUIREMENTS.md` and MUST start only after
the coordinator marks this unit `in-progress` with its prerequisite verified.
Build an independently releasable CaptchaFox verification adapter with exact
provider semantics and conversion to provider-neutral CAPTCHA evidence.

## Ownership and public contract

The module owns CaptchaFox configuration, verification endpoint interaction,
request/response schemas, site and origin binding, provider error mapping,
timeout classification and safe telemetry. It does not own widgets, provider
accounts, generic risk decisions, application HTTP handlers, or other CAPTCHA
providers. Public configuration MUST validate the secret, site key, allowed
origins/hostnames, endpoint policy, request limits and remote-client metadata
before network use. Public results MUST distinguish solved, rejected,
unavailable, malformed, throttled, cancelled and ambiguous outcomes without
exposing request tokens or provider bodies.

## Required behavior and security

The verifier MUST apply bounded contexts and bodies; serialize exactly the
pinned CaptchaFox contract; enforce configured site, hostname/origin and any
documented action or score fields; conservatively reject missing, duplicated,
unknown-critical or inconsistent fields; and preserve documented error codes
as redacted categories. Provider outage MUST never be converted to a valid
solve by the adapter. Retry policy MUST account for token single-use and
ambiguous requests.

Secrets and response tokens MUST never occur in URLs, diagnostics, telemetry,
fixtures or evidence. Endpoint override MUST be opt-in and SSRF-safe. Tests
MUST cover origin canonicalization, IDNs, ports, suffix attacks, replay,
substitution, oversized JSON, duplicate keys, unexpected content types,
partial reads, cancellation, timeouts, throttling and concurrent use.

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
