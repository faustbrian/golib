# Goal Harden: `webhook`

## Mission

Perform an evidence-driven cryptographic, canonicalization, HTTP, replay,
delivery, SSRF, interoperability, compatibility, and resource-safety audit of
`webhook`, then implement every justified hardening change required at an
untrusted internet boundary.

## Authoritative Inputs

- Go `net/http`, `crypto/hmac`, SHA-2, `crypto/subtle`, URL, context, and time
  contracts
- authoritative provider signature specifications for every preset
- relevant HTTP signature, digest, timestamp, and webhook documentation used
  by generic schemes
- `http-client`, `outbox`, and `queue` contracts for optional adapters
- `.ai/GOAL.md`, public APIs, vectors, docs, tests, fuzzers, benchmarks,
  dependencies, workflows, and changelog

Provider blog posts or copied snippets cannot substitute for authoritative
vectors and independently generated interoperability evidence.

## Phase 1: Baseline And Threat Model

1. Inventory every API, scheme, header, canonicalization rule, body read,
   secret path, replay operation, endpoint check, retry, adapter, and metric.
2. Build signature/provider/interoperability matrices with source links and
   positive/negative vectors.
3. Run all format/vet/lint/test/coverage/race/fuzz/benchmark/docs/security and
   interoperability gates.
4. Threat-model forgery, replay, timing attack, body substitution, header
   smuggling, secret leakage, SSRF, DNS rebinding, retry abuse, and payload DoS.
5. Add a failing regression or vector before every behavioral fix.

## Inbound Verification Audit

- exact raw bytes, empty body, chunked body, compressed body, trailers,
  partial reads, prior middleware reads, restoration, and close ownership
- duplicate/folded/malformed signature headers, whitespace, casing, encodings,
  multiple algorithms, unknown versions, and downgrade attempts
- timestamp parse, precision, boundaries, skew, overflow, multiple timestamps,
  and trusted clock ownership
- multiple active secrets, key IDs, rotation overlap, revoked secrets, and
  deterministic selection
- constant-time comparisons for all secret-dependent equality
- body/header count/size limits before allocation, hashing, or decode

## Replay And Idempotency Audit

- atomic check-and-record adapter contract
- concurrent identical requests, storage outage, TTL boundaries, clock skew,
  cancellation, and partial failure
- event ID collisions, absent IDs, provider retries, and tenant namespaces
- distinguish cryptographic replay rejection from application idempotency
- no exactly-once claim

## Outbound Signing And Interoperability Audit

- canonical method, path, query, host, headers, body digest, timestamp, key ID,
  line endings, Unicode, escaping, duplicate headers, and empty values
- deterministic vectors generated independently in another implementation
- mutation tests for every signed component
- secret rotation and recipient verification compatibility
- SemVer treatment of canonical bytes and emitted headers

## Delivery And SSRF Audit

- schemes, userinfo, fragments, redirects, hostnames, IP literals, private,
  loopback, link-local, multicast, and metadata-service ranges
- DNS resolution changes, rebinding, IPv4-mapped IPv6, proxies, and redirect
  target revalidation
- retries, idempotency keys, ambiguous receipt, `Retry-After`, cancellation,
  dead letters, replay, and nested retries
- body/response bounds, endpoint concurrency, fan-out limits, and shutdown
- integration adapter errors must preserve delivery guarantees

## Diagnostics And Observability Audit

- no secret, signature, complete payload, sensitive header, query credential,
  endpoint token, or replay key disclosure
- safe external verification errors versus detailed internal causes
- low-cardinality metrics, trace propagation, duplicate instrumentation,
  hook panic, disabled telemetry, and overhead

## Mandatory Hardening Evidence

- meaningful 100% production coverage and full race/leak suites
- authoritative and independently generated positive/negative vectors
- fuzzing for headers, canonicalization, timestamps, URLs, envelopes, and
  provider presets
- high-contention replay-store conformance tests
- controlled DNS/redirect/SSRF tests and `httptest` delivery failures
- resource-bound tests for bodies, headers, fan-out, attempts, and replay keys
- allocation/timing benchmarks for signing and verification
- executable receiver/sender/rotation/queued-delivery examples in CI

## Required Deliverables

1. Threat model and signature/provider/interoperability/SSRF matrices.
2. Findings report with severity, exploitability, evidence, and disposition.
3. Regression vectors, fuzz corpus, fixes, fault tests, and benchmarks.
4. Updated API, security, provider, delivery, operations, compatibility,
   migration, troubleshooting, and changelog documentation.
5. Release verdict with exact commands and residual risks.

## Release Blockers

Block release for forgery/bypass, non-constant-time secret comparison, replay
race, canonicalization ambiguity, SSRF/redirect bypass, secret/payload leak,
unbounded input or delivery, incorrect provider claim, interoperability gap,
coverage game, flaky security test, or red gate.

## Completion Criteria

Hardening is complete only when every supported signature has independent
vectors, hostile requests and endpoints are bounded, replay and rotation are
race-proven, high/medium findings are resolved, claims match exact evidence,
and all quality, interoperability, security, documentation, and release gates
pass.
