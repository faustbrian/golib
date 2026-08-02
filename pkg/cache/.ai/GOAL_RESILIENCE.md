# Goal: Cache Resilience Integration

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Extend the implemented `cache` package as the only resilience owner for cached
result reuse and explicitly eligible stale results. This supplements the base
goals and MUST NOT introduce a generic fallback engine.

## Required Additions

- Define fresh, stale-eligible, expired, negative, absent, refresh-in-progress,
  and backend-unavailable states precisely.
- Require caller policy to declare which results may be cached, served stale,
  negatively cached, or refreshed.
- Bound stale lifetime, refresh attempts, waiters, key cardinality, retained
  value size, and refresh concurrency.
- Provide single-flight or equivalent stampede control locally and explicit
  distributed coordination semantics for Valkey/PostgreSQL adapters.
- Ensure loader cancellation, refresh failure, panic, and backend ambiguity do
  not corrupt a previously valid value.
- Never serve stale authentication, authorization, revocation, financial,
  mutable secret, or otherwise unsafe data without an explicit domain policy.

## Kubernetes Semantics

Local caches are per pod and cold-start independently. Distributed caches share
data but local stampede locks do not prevent fleet-wide refresh storms.
Documentation MUST cover scale-up cold starts, rolling revision keying,
invalidation, backend failover, stale fallback, drain, aggregate refresh concurrency,
and HPA effects.

Cache backend failure MUST not make liveness fail. Fail-open/closed/stale policy
is per operation and MUST expose whether returned data is stale and why.

## Composition

Document cache placement relative to breaker, rate limit, bulkhead, adaptive
limit/throttle, retry, hedge, and total deadlines. Cache hits SHOULD avoid
unnecessary downstream admission. Refresh loaders MUST pass through the same
bounded resilience policies and share retry/hedge budgets.

## Acceptance Criteria

- Stale or negative data is never returned outside explicit eligibility and
  bounds.
- Local and fleet stampedes are addressed honestly.
- Refresh and loader lifecycle cannot leak work or overwrite newer values.
- Existing public behavior remains compatible unless migrated under SemVer.
- Meaningful exact 100% statement coverage and exactly 100% viable mutation
  kills remain blocking requirements.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
