# Goal Harden: `http-client`

## Mission

Perform an evidence-driven HTTP semantics, transport security, authentication,
retry, cancellation, observability, compatibility, and resource-ownership
audit of `http-client`, then implement every justified correction required
for hostile production networks.

## Authoritative Inputs

- Go `net/http`, `net/url`, `crypto/tls`, `context`, and transport contracts
- current HTTP semantics, redirects, authentication, retry, TLS, proxy, and
  OpenTelemetry specifications used by the package
- OAuth2 specifications and `golang.org/x/oauth2` contracts where integrated
- `.ai/GOAL.md`, public APIs, docs, examples, tests, fuzzers, benchmarks,
  dependencies, workflows, and changelog

Classify behavior as HTTP requirement, security policy, package guarantee,
vendor policy, or caller responsibility.

## Phase 1: Baseline And Threat Model

1. Inventory every exported API, default, transport clone, middleware,
   decorator, middleware stage, pagination strategy, pool, limiter, delay,
   breaker decision, cache operation, retry decision, request serializer,
   response decoder, stream, compressor, scope key, cookie jar, profile, fixture,
   body wrapper, error, metric, and test helper.
2. Map request lifecycle from construction through DNS, connect, TLS, write,
   headers, body, redirects, retries, decode, close, and cancellation.
3. Run the complete format/vet/lint/test/coverage/race/fuzz/benchmark/docs/
   vulnerability/workflow gate and record flakes or skips.
4. Threat-model malicious servers, proxies, redirects, oversized responses,
   slow peers, DNS rebinding, credential theft, retry amplification, and
   diagnostic leakage.
5. Require a failing regression before every behavior change.

## Request And Transport Audit

- URL joining, escaped paths, raw query, fragments, userinfo, Unicode hosts,
  IPv4/IPv6 literals, schemes, relative references, and base-path traversal
- proxy/no-proxy behavior, environment proxies, CONNECT, TLS server names,
  certificates, minimum versions, and custom roots
- request cloning, context ownership, header aliasing, body replay,
  `GetBody`, trailers, compression, expect-continue, and streaming bodies
- connection pooling, reuse after partial reads, body closure, idle limits,
  HTTP/1.1 versus HTTP/2, and transport shutdown
- explicit bounds for headers, responses, excerpts, redirects, retries, and
  elapsed time
- explicit ownership and shutdown of transports, idle connections, pending
  pools, waiters, timers, authentication refresh, and cache reconciliation

## Request Construction And Serialization Audit

- base and relative URL joining, escaped/raw paths, traversal, fragments,
  userinfo, query replacement/append, and trailing-slash behavior
- precedence and aliasing across client, endpoint, request, authentication,
  signing, middleware, and one-shot headers and parameters
- absent, null, empty, zero, default, omitted, duplicate, and multi-value fields
- repeated, comma, space, pipe, deep-object, form, multipart, and custom
  serialization with Unicode and reserved characters
- deterministic canonical ordering for signatures, cache keys, and fixtures
- body ownership, cloning, replayability, trailers, partial reads, and close
- generated-client adapters preserve standard `http.Request` access

## Authentication And Redirect Audit

- Basic, bearer, API-key, HMAC, and OAuth2 placement and redaction
- duplicate headers, malformed tokens, refresh races, expired tokens, refresh
  failure, cancellation, and single-flight behavior
- OAuth2 client-credentials scopes, endpoint authentication styles, bounded
  error bodies, token endpoint cancellation, and refresh error preservation
- prevention of recursive token transport, nested retries, and retry storms
- same-origin and cross-origin redirects, scheme downgrade, port changes,
  subdomains, userinfo, and credential stripping
- proxy authentication separation
- cookie domain/path/secure/SameSite/expiry behavior, public-suffix rules,
  redirect handling, persistence, and strict client/tenant isolation
- no secret in URL, error, trace, log, metric, fixture, or panic output

## Idempotency And Identity Audit

- generated and caller-provided key entropy, format, bounds, collision, and
  redaction
- one key per logical operation and stable reuse across every eligible attempt
- distinct keys across repeated calls, pool items, pagination requests, and
  unrelated redirects
- middleware order relative to signing, authentication, tracing, caching,
  retries, and transport
- operation, attempt, request, correlation, and trace identities remain distinct
- idempotency policy never upgrades an unsafe or non-replayable operation

## Retry And Resilience Audit

- method safety versus configured idempotency
- idempotency keys, replayable/non-replayable bodies, partial writes, and
  ambiguous server receipt
- status and transport-error classification, including HTTP/2 resets
- `Retry-After` date/delta parsing, overflow, clock skew, caps, and deadlines
- jitter distribution, maximum attempts, elapsed budget, cancellation, and
  nested retry amplification
- fixed/sliding-window, token/leaky-bucket admission, server-header learning,
  waiter fairness, maximum delay, cancellation, and clock behavior
- `circuit-breaker` state, rejection, half-open probe, HTTP classification,
  retry, limiter, cache, and telemetry ordering
- proof that breaker state is owned only by `circuit-breaker`
- deterministic errors that preserve the final response and all useful causes

## Middleware Audit

- operation versus attempt lifecycle and exact ordering of mutation,
  authentication, signing, telemetry, cache, limiter, breaker, retry, decode,
  error, and completion stages
- client, endpoint, and one-shot middleware precedence
- response short-circuit, replacement, panic, re-entry, duplicate registration,
  cancellation, and error wrapping
- proof that retries do not repeat operation-only side effects and that every
  network attempt receives required attempt middleware

## Pagination Audit

- page-number, offset/limit, opaque cursor/token, link-header, and custom
  continuation conformance
- cursor byte limits, empty/missing/repeated continuations, cycles, malformed
  links, integer overflow, inconsistent totals, and unstable collections
- maximum pages, items, bytes, duration, empty pages, and cancellation
- resumable state integrity and no secret leakage through continuation tokens
- sequential defaults and proof that concurrent pagination is enabled only when
  requests are independent and termination is known safely

## Concurrency And Pool Audit

- strict active and pending bounds, backpressure, dynamic concurrency, keyed
  results, stable/completion ordering, fail-fast, collect-all, and partial work
- iterator/generator/channel termination, producer panic, slow consumers,
  cancellation races, and pool reuse
- composition with pagination, retries, limiters, breakers, token refresh, and
  shared transports without nested fan-out or retry amplification
- no leaked goroutine, timer, request, response body, result, or blocked sender

## Cache And Revalidation Audit

- RFC 9111 storage, freshness, age, validation, invalidation, `Vary`, warning,
  and authenticated-response requirements
- `ETag`, weak/strong validators, `Last-Modified`, 304 merge, date parsing,
  clock skew, and incomplete response handling
- cache-key poisoning, header normalization, private/shared policy, credential
  isolation, redirects, compression, range requests, and unsafe methods
- stale-while-revalidate, stale-if-error, must-revalidate, offline behavior,
  explicit refresh, bypass, and purge semantics
- stampede coalescing, canceled leaders/waiters, backend outage, corruption,
  eviction, size limits, and slow stores
- proof that only complete bounded responses are cached and every synthetic
  response follows body-ownership and middleware contracts

## Streaming, Compression, And Transfer Audit

- streaming upload/download without hidden buffering and with strict byte/time
  limits
- multipart boundary, filename, media type, escaping, length, replay, and
  cancellation behavior
- progress callback frequency, panic, re-entry, cancellation, and secrecy
- checksum success, mismatch, unsupported algorithms, and partial destinations
- range and resume with strong/weak validators, changed resources, unsupported
  ranges, 200 fallback, 206 validation, and 416 handling
- temporary-file permissions, cleanup, atomic replacement, cross-filesystem
  rename, disk-full, and interrupted writes
- request compression and response decompression ordering with signatures,
  retries, caches, ranges, and content lengths
- decompression bombs, nested encodings, malformed streams, ratio limits,
  truncated data, trailing bytes, and unsupported encodings

## Response Lifecycle And Decode Audit

- ownership and closure across success, error, retry, redirect, cache,
  short-circuit, pool, stream, panic, and cancellation paths
- `HEAD`, 1xx, 204, 205, 304, empty body, missing/incorrect content length, and
  unexpected body behavior
- media-type parameters, suffixes, missing types, mismatches, sniffing, and
  caller overrides
- bounded decode, strict trailing-data policy, decoder panic, partial decode,
  malformed content, and codec cancellation
- status classification before/after decode and vendor error mapping
- bounded drain and connection reuse without blocking indefinitely or reading a
  body twice

## Policy Scope And Profile Audit

- origin, host, endpoint, credential, tenant, account, and custom scope-key
  construction, normalization, collision, and cardinality
- prove cookies, OAuth tokens, cache entries, coalescing, limiters, breakers,
  transports, and telemetry never cross forbidden scopes
- profile defaults and explicit override precedence for interactive, batch,
  streaming, and webhook workloads
- resolved-policy provenance, immutability, compatibility, and SemVer changes
- hostile tenant count, high-cardinality scope input, cleanup, eviction, and
  lifecycle bounds

## Egress Security Audit

- scheme, host, port, origin, CIDR, private, loopback, link-local, multicast,
  metadata-service, IPv4, IPv6, mapped-address, and Unix-socket policy
- hostname normalization, Unicode/punycode, trailing dot, alternate numeric IP
  forms, and authority confusion
- DNS rebinding, multi-answer DNS, connection-time address validation, resolver
  races, and proxy resolution ownership
- redirect and proxy destination revalidation with credential, cookie, trace
  baggage, and sensitive-header stripping
- dynamic base URLs, userinfo, TLS names, custom roots, client certificates,
  optional pinning, downgrade, and insecure opt-outs
- proof that allowlists cannot be bypassed through URL parsing differences

## Error, Telemetry, And Resource Audit

- typed errors through wrapping and `errors.Join`
- bounded excerpts, invalid encodings, compressed bombs, malformed status,
  truncated bodies, and decode failures
- low-cardinality metrics, trace propagation, sampling, disabled telemetry,
  hook panics, and duplicate instrumentation
- one logical-operation span and related attempt spans for retries, redirects,
  authentication challenges, and cache revalidation
- W3C Trace Context and explicitly allowlisted baggage without cross-origin or
  cross-tenant leakage
- no leaked response body, connection, timer, goroutine, token waiter, or retry
  buffer under success, failure, panic, or cancel

## Record And Replay Audit

- deterministic canonical matching for method, URL, query, selected headers,
  and bounded bodies
- repeated interactions, ordering, concurrency, optional interactions, unmatched
  requests, and unused fixture entries
- redaction of credentials, cookies, tokens, signatures, PII, cursors, volatile
  IDs, and caller-selected fields before persistence
- fixture schema versioning, migration, expiry, corruption, hostile files, size
  limits, path traversal, and atomic writes
- replayed responses follow real response ownership, middleware, cache,
  telemetry, decode, and error contracts
- recording is opt-in and production credentials can never be persisted by
  default

## Mandatory Hardening Evidence

- meaningful 100% production coverage with behavior review
- full race and leak suites
- fuzz targets for URLs, headers, challenges, redirects, error payloads, and
  retry options
- real HTTP/1.1, HTTP/2, TLS, proxy, cancellation, and redirect integration
  tests using controlled servers
- adversarial slow/large/truncated/compressed response tests
- allocation benchmarks for direct, instrumented, authenticated, and retried
  requests
- pagination and request-pool throughput benchmarks with bounded-memory evidence
- cache hit, miss, revalidation, stale, and concurrent stampede benchmarks
- limiter and circuit-breaker composition benchmarks
- request construction, serialization, stream copy, compression, decoding, and
  policy-scope benchmarks
- record/replay matching and large-fixture bounded-memory benchmarks
- runnable vendor-client examples validated in CI

## Required Deliverables

1. HTTP/security policy matrix and threat model.
2. Findings report with severity, reproduction, impact, and disposition.
3. Regression tests, fuzz corpus, fixes, leak checks, and benchmarks.
4. Updated API, request/response lifecycle, serialization, streaming,
   compression, middleware, pagination, pools, retry, rate-limit, breaker,
   caching, idempotency, cookies, scopes, profiles, egress, telemetry,
   record/replay, auth, security, operations, compatibility, migration,
   troubleshooting, and changelog documentation.
5. Final release verdict with exact command evidence and remaining risks.

## Release Blockers

Block release for credential forwarding/leakage, unsafe retry, unbounded body
or elapsed work, cancellation loss, connection/body leak, redirect bypass,
unbounded pagination or pool work, cursor cycles, limiter unfairness, incorrect
breaker ordering, cache poisoning or RFC violation, request serialization drift,
idempotency-key misuse, stream corruption, decompression bomb, cross-scope state,
SSRF or DNS-rebinding bypass, unsafe fixture recording, response ownership bug,
race, panic, flaky network test, misleading timeout guarantee, coverage game, or
any red quality/security gate.

## Completion Criteria

Hardening is complete only when request construction, serialization, identity,
streaming, compression, response ownership, middleware, pagination, pools,
retry, limiter, breaker, cache, cookies, scopes, profiles, egress, telemetry,
record/replay, redirects, and authentication have executable evidence; all high
and medium findings are resolved; the entire gate passes without unexplained
skips; and public documentation states precise HTTP guarantees rather than
framework-style promises.
