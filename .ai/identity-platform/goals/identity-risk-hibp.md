# Goal: pkg/identity/risk/hibp

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/risk/hibp`
- Canonical module: `pkg/identity/risk/hibp`
- Canonical goal after scaffolding: `pkg/identity/risk/hibp/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/risk/hibp:v1`; owned operation IDs: `contract:operation:identity.risk.hibp-check:v1`
- Requires: `identity/risk`
- Consumes existing primitives: `http-client`, `password`, `audit`,
  `telemetry`
- Unlocks after verification: `identity/reference`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/risk/hibp` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

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

The design MUST define a checker or signal-provider contract, CheckRequest, configuration,
endpoint policy, padding policy, cache policy, result and breach-count bounds,
typed failures, telemetry, and an adapter to the `identity/risk` signal
contract. Public results MUST distinguish no match, a bounded match count,
provider unavailability, invalid configuration, malformed response, and
cancellation without revealing the candidate password or hash material. The
adapter MUST return normalized evidence and MUST NOT decide allow, deny,
throttle, step-up, password acceptance, or provider-failure policy.

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

`CheckRequest` MUST accept the bounded candidate password as a borrowed
`golibpassword.Secret` together with the trusted operation, tenant, action,
purpose, subject scope, policy version and replay identifier. Prefix input is
not a public request alternative: this module MUST derive the five-character
uppercase SHA-1 prefix locally from the exact candidate and MUST compare the
returned suffix set locally.

## Provider and privacy requirements

- The client MUST use the documented range endpoint and MUST NOT use endpoints
  that disclose a complete hash or password. The production endpoint MUST be
  fixed to `https://api.pwnedpasswords.com/range/{PREFIX}`, where `{PREFIX}` is
  exactly five uppercase hexadecimal characters. The client MUST require HTTPS,
  MUST send a non-secret `User-Agent`, MUST NOT follow redirects, and MUST treat
  every redirect, non-200 response or endpoint-authority change as unavailable.
- Response bytes, lines, suffix length, count width, cache size, and request
  concurrency MUST be bounded before allocation or parsing.
- The `Add-Padding` behavior MUST be explicit and enabled by the recommended
  privacy profile unless a caller deliberately selects a documented alternative.
  The recommended profile MUST send exactly `Add-Padding: true`; it MUST parse
  zero-count padding rows as syntactically valid non-matches and MUST NOT expose
  padding-row count or response size as signal evidence.
- Cache keys MUST contain only the five-character prefix and MUST have bounded
  retention. Cache values MUST remain private and MUST NOT be persisted by
  default. Only a complete, status-200, successfully parsed range response MAY
  be cached; redirects, partial bodies, cancellations, malformed responses and
  other failures MUST NOT be cached as no-match or stale success. Eviction and
  expiry MUST zero or release suffix data where practical.
- Network, parsing, throttling, and provider failures MUST follow the explicit
  outcome owned by the consuming operation in
  `.ai/identity-platform/REFERENCE_CONFIGURATION.md`. The reference password
  create/change/reset operations deny on unavailable or unknown HIBP evidence
  even when another factor exists; the adapter itself makes no decision.
  Unavailable/unknown MUST never become allow or a clean password.
- The parser MUST accept only bounded ASCII rows of exactly 35 hexadecimal
  suffix characters, one ASCII colon, and an unsigned decimal count, separated
  by LF or CRLF. It MUST compare suffixes case-insensitively after canonical
  uppercase decoding; reject invalid lengths, characters, separators, duplicate
  real suffixes with conflicting counts, embedded carriage returns and trailing
  data; collapse identical duplicate suffix/count rows; and consume the
  complete bounded body before returning evidence. Decimal overflow or a count
  above the configured public `hibp.max_breach_count` bound MUST produce malformed/unavailable evidence
  and MUST NOT wrap, saturate, truncate or become no-match.
- Concurrent requests for the same five-character prefix MUST coalesce into
  one bounded in-flight request. Coalescing MUST NOT cross prefixes, endpoint/
  padding profiles or cache namespaces. One waiter's cancellation MUST stop
  only that waiter; the shared request MUST be cancelled when all waiters leave.
  Every waiter MUST receive the same immutable parsed result, while per-caller
  deadlines and telemetry remain independent.
- Total outbound requests MUST obey the configured
  `hibp.outbound_concurrency` bound. Saturation queues bounded callers without
  bypassing same-prefix coalescing, and cancellation removes only that caller.
- Each request and evidence result MUST bind the trusted operation, tenant,
  action, purpose, subject scope, policy version and replay identifier. Cached
  range data MAY be shared only as provider data; a derived match result MUST
  NOT be replayed across bindings. Query, match, unavailable and malformed
  events MUST use `.ai/identity-platform/SECURITY_EVENTS.md` without hash
  material; cache expiry, tenant disablement and configuration/key changes MUST
  follow `.ai/identity-platform/LIFECYCLE_CASCADES.md`.

## Required test and interoperability evidence

Tests MUST cover no-match and matched suffixes, case handling, padded and
unpadded responses, duplicate suffixes, maximum counts, malformed separators,
invalid hexadecimal values, oversized bodies and lines, partial reads,
cancellation, timeouts, throttling, retries, caching, concurrency, and
redaction. Tests MUST also cover redirect refusal, exact `Add-Padding: true`,
zero-count padding rows, duplicate real suffixes, decimal overflow, cacheable
versus non-cacheable outcomes, same-prefix coalescing, all-waiter cancellation
and no cross-profile coalescing. Deterministic fixtures MUST prove that requests
contain only a five-character prefix. Provider interoperability MUST use HIBP's
documented protocol or test facilities without submitting a real user password.

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
