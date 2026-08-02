# Goal: HTTP Client Resilience Composition

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Complete `http-client` as the explicit HTTP integration and composition surface
for retry, breaker, rate limit, cache, bulkhead, hedge, concurrency limit, and
adaptive throttle. This supplements the executed base goals and MUST NOT copy
their algorithms into the client.

## Required Composition Model

The client MUST distinguish:

- caller total deadline;
- admission/queue wait deadline;
- DNS, connect, TLS, response-header, body, and idle transport limits;
- per-attempt deadline;
- retry delay and attempt count;
- hedge delay and concurrent attempts;
- pagination or fan-out child requests; and
- shutdown/drain deadline.

No child policy may extend the caller deadline. Timeout expiry cancels through
context but MUST not claim an uncooperative transport or server-side side effect
has stopped.

Provide explicit reviewed pipeline presets plus a lower-level builder whose
order is visible. Presets MUST document which policy sees each physical attempt
and which sees the logical operation.

## Replay And Amplification

- Retry and hedge require replayable request bodies and method/operation safety.
- A shared work budget MUST cap attempts, hedges, redirects, pagination,
  refresh, and fan-out where composed.
- Idempotency keys MAY support safe writes but MUST not be treated as universal
  proof across proxies and vendors.
- Every physical response body, including losing hedges and failed retries,
  MUST be drained/closed according to transport reuse policy.
- `Retry-After` and vendor delays remain bounded by caller policy.

## Policy Classification

Stable typed errors MUST distinguish local rate, bulkhead, adaptive-limit,
adaptive-throttle, breaker, retry-budget, cache, caller cancellation, transport,
protocol, and body-processing outcomes. Local rejection MUST not become a
downstream failure sample. HTTP status defaults MUST be conservative and
overrideable per vendor client.

Cache fallback is limited to explicitly cacheable and stale-eligible responses.
Alternate vendor/business fallback remains caller-owned.

## Kubernetes And Fleet Behavior

Per-client policy state is pod-local unless a distributed backend is selected.
Documentation MUST calculate connection pools, in-flight limits, retries,
hedges, and downstream aggregate load using min/max replicas and request fan-out.

Cover DNS/service discovery changes, uneven endpoint load, cold connection
pools, rolling policy revisions, scale-up amplification, SIGTERM drain, and HPA
feedback. Hedging SHOULD support caller-owned endpoint diversity without
violating auth, consistency, or residency. Dependency outage MUST not fail
process liveness.

## Deliverables

- Executable pipeline diagrams and attempt timelines.
- Safe presets for ordinary idempotent calls, latency-sensitive reads, and
  controlled writes, with unsafe features off by default.
- Guides for package composition, budgets, timeout scopes, body replay,
  endpoint diversity, Kubernetes sizing, drain, observability, and migration.
- Equivalent-behavior benchmarks against `net/http`, maintained client
  frameworks, and Failsafe-Go compositions.

## Acceptance Criteria

- No policy algorithm is duplicated in `http-client`.
- Every request has a finite total bound and finite amplification.
- All bodies and loser/retry resources are owned and closed exactly once.
- Pipeline ordering and failure classification match documentation.
- Existing users retain compatible behavior unless opting into new policies.
- Meaningful exact 100% statement coverage and exactly 100% viable mutation
  kills remain blocking requirements.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
