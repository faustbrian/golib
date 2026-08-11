# Goal: pkg/identity/risk/hibp

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals, as shown here.

## Execution metadata

- Unit: `identity/risk/hibp`
- Canonical module: `pkg/identity/risk/hibp`
- Canonical goal after scaffolding: `pkg/identity/risk/hibp/.ai/GOAL.md`
- Requires: `identity/risk`
- Consumes existing primitives: `http-client`, `password`, `audit`,
  `telemetry`
- Unlocks after verification: No program unit.

## Start gate

The agent MUST read and satisfy `../COMMON_REQUIREMENTS.md`. It MUST NOT begin
until `../INVENTORY.md` marks `identity/risk/hibp` as `ready` and
`identity/risk` is `verified`. The agent MUST claim only this unit and
record its owner before any implementation edit.

## Objective and observable completion

Build an independently releasable `pkg/identity/risk/hibp` module that
queries the Have I Been Pwned Pwned Passwords range API using its k-anonymity
protocol and converts matching breach counts into provider-specific evidence
and provider-neutral identity-risk signals.

## Ownership boundary

This module owns SHA-1 prefix derivation required by the HIBP protocol, range
requests, suffix matching, breach-count parsing, padding policy, response
limits, caching policy, and HIBP failure classification. It does not own
password hashing, password acceptance policy, complete-password disclosure,
generic risk decisions, user messaging, or other HIBP products.

## Required public contract

The design MUST define a checker or signal-provider contract, configuration,
endpoint policy, padding policy, cache policy, result and breach-count bounds,
typed failures, telemetry, and an adapter to the `identity/risk` signal
contract. Public results MUST distinguish no match, a bounded match count,
provider unavailability, invalid configuration, malformed response, and
cancellation without revealing the candidate password or hash material.

## Required behavior

The implementation and tests MUST consume the candidate exactly according to
the explicit password-input contract without independently normalizing or
trimming it, compute SHA-1 only because the HIBP range protocol requires it,
transmit exactly the first five uppercase hexadecimal characters, parse and
compare suffixes locally without timing-dependent early acceptance, and zero
or release transient sensitive buffers where practical. It MUST NOT transmit,
log, cache, trace, or expose the complete password or complete SHA-1 digest.
It MUST NOT log, trace, or expose the returned suffix set; it MAY cache that
set only under the bounded private cache policy below.

## Provider and privacy requirements

- The client MUST use the documented range endpoint and MUST NOT use endpoints
  that disclose a complete hash or password.
- Response bytes, lines, suffix length, count width, cache size, and request
  concurrency MUST be bounded before allocation or parsing.
- The `Add-Padding` behavior MUST be explicit and enabled by the recommended
  privacy profile unless a caller deliberately selects a documented alternative.
- Cache keys MUST contain only the five-character prefix and MUST have bounded
  retention. Cache values MUST remain private and MUST NOT be persisted by
  default.
- Network, parsing, throttling, and provider failures MUST follow an explicit
  fail-open, fail-closed, or step-up policy owned by the consuming risk action.
  The adapter MUST NOT silently convert provider failure into a clean password.

## Required test and interoperability evidence

Tests MUST cover no-match and matched suffixes, case handling, padded and
unpadded responses, duplicate suffixes, maximum counts, malformed separators,
invalid hexadecimal values, oversized bodies and lines, partial reads,
cancellation, timeouts, throttling, retries, caching, concurrency, and
redaction. Deterministic fixtures MUST prove that requests contain only a
five-character prefix. Provider interoperability MUST use HIBP's documented
protocol or test facilities without submitting a real user password.

## Acceptance evidence

Before this unit becomes `verified`, the owner MUST satisfy every common gate,
the package-specific privacy and provider requirements above, exact coverage
and mutation gates, fuzzing of the range parser, race and concurrency checks,
bounded-allocation benchmarks, clean-consumer proof, manifests, API baseline,
security and supply-chain checks, documentation, changelog, and any changed
reverse-dependant gates.

## Release blockers

The unit MUST remain `implemented-unverified` or `blocked` if it can send or
expose more than the five-character prefix, accept malformed or unbounded
responses, treat provider failure as no breach without explicit policy, retain
sensitive material outside documented bounds, or lacks current HIBP protocol
and interoperability evidence.
