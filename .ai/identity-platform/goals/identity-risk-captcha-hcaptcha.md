# Goal: pkg/identity/risk/captcha/hcaptcha

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Execution metadata

- Unit: `identity/risk/captcha/hcaptcha`
- Canonical module: `pkg/identity/risk/captcha/hcaptcha`
- Canonical goal after scaffolding: `pkg/identity/risk/captcha/hcaptcha/.ai/GOAL.md`
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
rate limits, or another CAPTCHA provider.

The public API MUST expose validated configuration, a bounded verifier, typed
provider evidence, provider reason codes and stable redacted failures. Site
secret, endpoint override, expected hostname/action, minimum score when the
selected hCaptcha product returns one, remote-IP disclosure and fail policy
MUST be explicit. Endpoint overrides MUST be opt-in and SSRF-safe.

## Required behavior and security

The adapter MUST submit each response token at most once per verification
attempt, apply a bounded context, encode parameters exactly as documented,
reject empty/oversized tokens and malformed/oversized JSON, and validate every
configured hostname/action/sitekey invariant before returning success. It MUST
preserve hCaptcha error codes as safe classifications while treating unknown
codes conservatively. Provider failure, throttling, timeout, cancellation and
ambiguous transport outcomes MUST remain distinct from a solved challenge and
from a valid negative decision; only the consuming risk policy may select
fail-open, fail-closed or step-up behavior.

Tokens and secrets MUST never enter URLs, logs, errors, traces, metrics,
fixtures or snapshots. Retries MUST NOT occur after an ambiguous accepted
request unless hCaptcha's pinned contract proves replay-safe behavior. Response
bytes, nesting, strings, errors and enterprise fields MUST be bounded before
allocation. Hostname canonicalization, IDNs, ports, suffix confusion, action
substitution, token replay, duplicated fields and clock skew MUST have
deterministic denial tests.

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
