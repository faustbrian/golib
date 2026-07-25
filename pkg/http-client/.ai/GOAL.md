# Goal: `http-client`

## Objective

Build a production-grade open source Go module for creating typed outbound
HTTP integrations with standardized transport, authentication, resilience,
observability, error, and testing behavior.

It should provide the reusable policy layer expected from a serious vendor
client system while preserving `net/http` types and normal Go composition.

## Product Position

`http-client` should be:

- built on `net/http`, `http.Client`, and `http.RoundTripper`
- suitable for REST, JSON-RPC, SOAP, webhook, and proprietary HTTP APIs
- usable for handwritten and generated clients
- explicit about retries, replayability, timeouts, and response ownership
- modular enough to adopt authentication or observability independently
- neutral about application architecture and payload codec

It must not become a fluent request DSL, hide HTTP semantics, or replace typed
vendor-specific client packages.

## First-Version Scope

### Transport Construction

- safe transport and client defaults
- connection, TLS, proxy, DNS, idle-pool, and HTTP/2 configuration seams
- optional HTTP/3 adapter only after its transport, fallback, telemetry, and
  compatibility behavior are independently proven
- total and per-stage timeout policy
- strict base URL and relative path resolution
- request and response body size limits
- deterministic user-agent and request-header policy
- response-body closure and connection-reuse helpers
- explicit transport ownership, reuse, `CloseIdleConnections`, pool draining,
  pending-work cancellation, and deterministic shutdown

### Request Construction And Serialization

- immutable reusable request specifications without replacing `http.Request`
- deterministic base URL, escaped path, raw path, query, fragment, and relative
  reference resolution
- explicit precedence for client, endpoint, request, authentication, signing,
  and one-shot headers and query parameters
- safe multi-value headers and query parameters without comma-folding fields
  whose semantics forbid it
- absent, null, empty, zero, default, and omitted value distinctions
- repeated, comma-delimited, space-delimited, pipe-delimited, deep-object, and
  custom query serialization strategies
- deterministic map/key ordering where signatures, caches, fixtures, or tests
  require a canonical representation
- form URL encoding and streaming multipart construction with caller-controlled
  filenames, media types, lengths, and limits
- explicit body ownership, replayability, `GetBody`, trailers, and close policy
- request cloning that never aliases mutable headers, URLs, maps, or body state
- typed extension points for generated OpenAPI clients without importing
  generator-specific models into core

### Middleware Pipeline

- deterministic request, transport, response, error, and completion stages
- connector/client-wide, endpoint/request-specific, and one-shot middleware
- explicit ordering, names, priorities, and inspectable resolved pipelines
- request mutation before signing and authentication with immutable snapshots
  where later middleware must observe a stable request
- response replacement and short-circuit responses for cache hits, fixtures,
  circuit rejection, and policy decisions without network I/O
- middleware-safe retry semantics: per-operation middleware runs once and
  per-attempt middleware runs for every network attempt
- panic containment, cancellation propagation, and typed middleware errors
- no hidden global middleware registry or implicit `init` registration

### Authentication

- Basic, bearer, API-key, and HMAC request decorators
- OAuth2 token-source integration through `golang.org/x/oauth2`
- outbound OAuth2 client-credentials support and safe token refresh coordination
- token endpoint requests using the same hardened transport policy without
  recursive authentication or nested retry loops
- no inbound authentication server, authorization framework, or OAuth2 protocol
  reimplementation
- credential redaction and no accidental URL/query leakage
- composable request editors for generated clients
- opt-in cookie-jar and session support with explicit ownership, persistence,
  public-suffix policy, redirect behavior, and per-client isolation
- no ambient or package-global cookie jar

### Idempotency And Operation Identity

- caller-supplied and generated idempotency-key support
- one stable key per logical operation, reused across retries and redirects only
  where redirect policy preserves operation identity
- a new key for every distinct logical operation, including repeated pool items
- explicit per-endpoint policy because provider idempotency semantics differ
- key generation, validation, maximum length, entropy, redaction, and provenance
- middleware ordering that assigns identity before signing, telemetry, retry, and
  transport attempts
- no assumption that an idempotency key makes an arbitrary operation safe

### Resilience

- retry decisions based on method, status, error, idempotency, and body replay
- bounded exponential backoff with jitter
- `Retry-After` parsing and policy
- caller cancellation and deadline preservation
- proactive rate limiting with fixed-window, sliding-window, token-bucket, and
  leaky-bucket policy adapters
- `Retry-After` and standardized/vendor rate-limit header observation that can
  delay future admission before another request reaches the server
- context-aware in-process request delays with explicit maximum wait and no
  detached timers or goroutines
- first-party `circuit-breaker` integration through a narrow interface,
  including fail-fast rejection and half-open probe control
- protection against retry storms and multiplicative nested retries

### Pagination

- typed lazy iteration without replacing vendor-specific request/response types
- page-number, offset/limit, opaque cursor/token, RFC link-header, and custom
  continuation strategies
- cursor extraction from headers, response envelopes, and typed callbacks
- opaque cursor preservation with configurable byte-length bounds
- maximum pages, items, elapsed time, response bytes, and empty-page limits
- repeated-cursor and continuation-cycle detection
- caller cancellation, resumable continuation state, and deterministic errors
- configurable item extraction without untyped map-based vendor APIs
- sequential pagination by default and bounded concurrent pagination only when
  total pages or independent continuations make it semantically safe

### Concurrency And Request Pools

- bounded concurrent execution over slices, iterators, generators, and channels
- fixed and dynamically selected concurrency with strict minimums and maximums
- backpressure and bounded pending work rather than one goroutine per request
- keyed requests and stable input-order or completion-order result modes
- per-request success/error results without losing partial completed work
- fail-fast and collect-all policies with explicit cancellation behavior
- pool-wide elapsed, request-count, byte, and memory budgets
- shared transport reuse without conflating transport pools and execution pools
- composition with rate limits, retries, circuit breakers, and pagination without
  multiplicative concurrency or retry amplification

### HTTP Caching And Revalidation

- optional caching middleware with no mandatory cache backend
- RFC 9111 cache-control, freshness, age, validation, and `Vary` semantics
- conditional requests using `ETag`/`If-None-Match` and
  `Last-Modified`/`If-Modified-Since`
- private versus shared cache policy and authorization-aware cache safety
- explicit cacheable methods and statuses; unsafe methods are never cached by
  default
- configurable cache keys, namespaces, TTL overrides, bypass, refresh, and
  invalidation
- stale-while-revalidate and stale-if-error only where policy and protocol allow
- request coalescing/single-flight protection against cache stampedes
- bounded response storage with complete-body-only admission
- in-memory reference backend plus optional `cache` or application-provided
  adapters without forcing Redis or Valkey on all users
- cache provenance and age metadata without changing standard response access

### Streaming, Compression, And Transfers

- streaming request uploads and response downloads without mandatory buffering
- context-aware progress observers with bounded frequency and no lock-held calls
- caller-provided writers and readers plus safe temporary-file helpers
- atomic destination replacement only after complete validation
- content length, transferred bytes, elapsed time, and decompressed-size limits
- checksums and digest validation with explicit algorithms and mismatch errors
- range requests, resumable downloads, validator checks, and restart fallback
- streaming multipart bodies with deterministic boundary and replay policy
- request compression and response decompression with explicit supported
  encodings
- compressed-to-decompressed ratio and absolute output bounds against bombs
- correct interaction among compression, signatures, retries, caches, ranges,
  progress, and observability
- no automatic retry or redirect of a non-replayable stream

### Response Lifecycle And Decoding

- one documented response ownership contract for success, error, short-circuit,
  retry, redirect, cache, pool, and streaming paths
- media-type parsing and explicit expected-content-type validation
- bounded typed decoding through caller-selected codecs
- empty-body, `HEAD`, 1xx, 204, 205, 304, and content-length mismatch behavior
- optional trailing-data rejection and strict single-document decoding
- status classification independent from body decoding
- typed vendor-error mapping hooks that preserve status, headers, bounded safe
  excerpts, request identity, and original causes
- complete body read/close and bounded drain policy for connection reuse
- no double read, hidden full buffering, or decoder ownership ambiguity

### Policy Scope And Profiles

- explicit scope keys for origin, host, endpoint, credential, tenant, account,
  and caller-defined dimensions
- independent scoping of transports, cookies, OAuth tokens, caches, request
  coalescing, rate limiters, circuit breakers, and metrics
- secure defaults that prevent credentials or cached data crossing tenant or
  identity boundaries
- named, versioned policy profiles for interactive, batch, streaming, and
  webhook-delivery workloads
- profiles define finite timeout, retry, pool, limiter, breaker, cache, body, and
  shutdown defaults without hiding their resolved values
- explicit per-client and per-request overrides with deterministic precedence
- inspectable resolved policy and provenance for operations and attempts

### Egress Security

- allowed scheme, host, port, origin, and CIDR policies
- explicit private, loopback, link-local, multicast, metadata-service, and Unix
  socket access policy
- DNS rebinding defenses that validate resolved addresses at connection time
- redirect and proxy destination revalidation against the same egress policy
- strict credential, cookie, trace baggage, and sensitive-header stripping when
  trust boundaries change
- TLS minimums, roots, server names, client certificates, and optional pinning
  without insecure defaults
- bounded DNS, connect, TLS, proxy, header, body, and total-operation work
- no userinfo credentials or unvalidated dynamic base URLs

### Errors And Observability

- typed transport, HTTP-status, decode, limit, and retry-exhaustion errors
- bounded response excerpts with mandatory redaction hooks
- stable access to status, headers, vendor code, cause, and retryability
- `slog` hooks and optional `log` integration without mandatory logging
- OpenTelemetry tracing and metrics through optional `telemetry` adapters
- request IDs and correlation propagation
- W3C Trace Context propagation with explicit baggage allowlists
- one logical-operation span plus related physical-attempt spans for retries,
  redirects, authentication challenges, and revalidation
- stable low-cardinality metrics that distinguish operation, attempt, cache,
  limiter, breaker, pool, and decode outcomes
- no tenant, credential, cursor, raw path, query, or vendor message as an
  uncontrolled telemetry label

### Testing Support

- deterministic fake clock/backoff/randomness seams
- `httptest` helpers for scripted exchanges
- request capture with secret redaction
- fixtures for retry, timeout, cancellation, truncation, and malformed responses
- contract-test helpers usable by concrete vendor clients
- sanitized record/replay fixtures with deterministic request matching
- canonical matching for method, URL, query, selected headers, and bounded body
- configurable secret and volatile-field redaction before fixture persistence
- strict unmatched and unused interaction failures
- fixture schema versioning, migration, expiry metadata, and compatibility checks
- no recording of live credentials or authorization material by default

## Integration Model

Concrete vendor clients remain separate packages and expose typed methods,
requests, responses, and domain errors. This module supplies transport policy
through `RoundTripper`, request decorators, response classifiers, and testing
helpers.

Generated OpenAPI or WSDL clients must be wrapped by the vendor package. This
module may provide adapters but must not expose generator-specific models as a
core API.

`circuit-breaker` owns generic breaker state and policy. `http-client`
owns HTTP outcome classification, middleware ordering, request replay, response
closure, and the placement of breaker admission relative to retries and rate
limits. The HTTP module MUST NOT maintain a second breaker state machine.

Cache storage, distributed rate-limit coordination, and telemetry exporters are
ports with optional adapters. Core behavior MUST remain useful with in-process
implementations and standard-library transports.

`wire` owns JSON, XML, SOAP, YAML, TOML, and MessagePack codecs.
`http-client` owns codec invocation, media-type validation, body limits,
response ownership, and decode error context. `cache` owns reusable cache
backends while this module owns HTTP cache semantics and cache-key safety.
`queue` owns durable delay and replay; this module owns only bounded
context-aware in-process admission delays.

## Non-Goals

- no replacement for `net/http`
- no generic untyped `Get`/`Post` API as the primary product
- no JSON, XML, SOAP, or JSON:API implementation
- no vendor business logic or endpoint catalog
- no service discovery, load balancer, gateway, or reverse proxy
- no automatic retries for unsafe requests
- no durable request scheduling or delayed-job persistence; use `queue`
- no distributed crawler, workflow engine, or unbounded fan-out executor
- no assumption that every cursor pagination model can execute concurrently
- no built-in JSON, XML, SOAP, MessagePack, or vendor DTO implementation
- no persistent cache backend implementation in core
- no vendor endpoint catalog, business error taxonomy, pagination extraction,
  signing algorithm, or idempotency contract
- no SSE or WebSocket lifecycle in the request/response client; those require
  dedicated packages and goals
- no service discovery, client-side load balancing, or endpoint health polling
- no logging of complete payloads or credentials by default

## Required Design Properties

- default clients must have explicit finite timeouts
- retries must require a replayable body and safe/idempotent policy
- redirects must not forward credentials across trust boundaries
- body limits must apply before unbounded allocation or decoding
- cancellation must stop backoff, token refresh waits, and body processing
- middleware order must be deterministic and inspectable
- operation, attempt, cache, limiter, breaker, authentication, signing, and
  telemetry middleware order must have one documented default
- pagination and pools must enforce bounds before creating work
- rate-limit and delay waits must honor context cancellation and deadlines
- cache hits must obey authentication, `Vary`, freshness, and redaction policy
- breaker failures, limiter rejection, retry exhaustion, and HTTP failures must
  remain distinguishable through typed errors
- logical operation identity, idempotency key, and trace identity must remain
  stable or change according to one documented attempt model
- policy state must never cross a scope boundary accidentally
- request and response ownership must be explicit for every middleware exit
- streaming paths must remain bounded without hidden whole-body buffering
- shutdown must leave no pending pool work, waiter, timer, body, connection, or
  background refresh goroutine owned by the client
- egress policy must be rechecked after DNS resolution, proxying, and redirects
- callers must retain access to standard HTTP request/response primitives
- errors and telemetry must never require reading a consumed body twice

## Documentation Deliverables

- README, quickstart, and complete public API reference
- guides for typed JSON, XML/SOAP, JSON-RPC, and generated OpenAPI clients
- authentication cookbook for every supported scheme
- request construction, query serialization, forms, multipart, and generated
  client integration guide
- response ownership, status classification, media-type, decoding, and vendor
  error mapping guide
- idempotency-key and logical-operation identity guide
- retry safety and idempotency guide
- middleware lifecycle, ordering, short-circuit, and custom-policy guide
- pagination guide covering page, offset, cursor, link, custom, resume, and
  bounded-concurrency scenarios
- request-pool guide covering backpressure, ordering, partial results, and
  cancellation
- rate-limit and delay guide covering proactive and server-directed admission
- circuit-breaker integration and policy-composition guide
- RFC-aware caching, revalidation, invalidation, stale, and backend guide
- streaming upload/download, ranges, resume, progress, checksum, compression,
  decompression, and temporary-file guide
- cookies and isolated session guide
- policy scope, multi-tenant isolation, and versioned profile guide
- egress policy, SSRF, DNS rebinding, proxy trust, and TLS guide
- logical-operation versus physical-attempt tracing and metrics guide
- sanitized record/replay fixture guide
- timeout, connection pool, TLS, proxy, and redirect guide
- error classification and vendor error mapping guide
- OpenTelemetry and `slog` integration guide
- testing and fixture guide
- adoption examples for at least two materially different vendor APIs
- FAQ, troubleshooting, security, compatibility, migration, and performance
  documentation

## Testing And Quality Standard

Meaningful 100% production coverage is mandatory, proving all branches and
failure semantics rather than line execution.

Required verification includes:

- real `httptest.Server` integration tests
- HTTP/1.1 and HTTP/2 behavior where practical
- redirect, proxy, TLS, timeout, cancellation, and connection-reuse tests
- deterministic retry, jitter, `Retry-After`, and rate-limit tests
- pagination conformance tests for page, offset, cursor, link, custom,
  repetition, limits, resume, cancellation, and malformed continuation
- pool tests for concurrency bounds, backpressure, ordering, partial failure,
  cancellation, and composition with retries and rate limits
- cache tests for RFC 9111 directives, age, `Vary`, authorization, conditional
  requests, invalidation, stampede prevention, and stale policies
- request construction and serialization conformance matrices
- idempotency-key lifecycle tests across retries, redirects, pools, and failures
- streaming, multipart, compression, range, resume, checksum, progress, and
  temporary-file ownership tests
- response ownership, status, media type, empty body, strict decode, drain, and
  connection reuse tests
- cookie and policy-scope isolation tests across origins, credentials, tenants,
  endpoints, caches, limiters, breakers, and token sources
- egress tests for private addresses, metadata endpoints, DNS rebinding, proxies,
  redirects, dynamic hosts, TLS, and credential stripping
- record/replay matching, redaction, schema compatibility, and hostile fixture
  tests
- profile default, override, provenance, and SemVer compatibility tests
- operation/attempt trace and low-cardinality metric conformance tests
- circuit-breaker ordering, rejection, probe, retry, and recovery tests
- race tests for token refresh, cache coalescing, limiters, pools, pagination,
  breaker integration, and shared transports
- fuzzing for URLs, headers, authentication challenges, and error payloads
- leak tests for bodies, connections, timers, and goroutines
- benchmarks with allocation reporting for middleware and retry paths

## Repository And Release Requirements

- GitHub Actions for format, vet, lint, tests, meaningful 100% coverage, race,
  fuzz smoke tests, benchmarks, docs, `govulncheck`, and tagged releases
- `make safety` and `make check` matching CI
- `GO-SAFETY-1`, with no production `unsafe`, cgo, or `go:linkname`
- complete OSS governance, security, contribution, attribution, and release
  files
- strict `CHANGELOG.md` updates for every implementation task
- SemVer treatment of exported APIs, defaults, errors, headers, and retry
  behavior

## Execution Plan

1. Specify policy, scope, ownership, operation/attempt identity, errors, limits,
   profiles, and middleware ordering.
2. Implement transport lifecycle, egress policy, request construction,
   serialization, streaming, compression, and response ownership primitives.
3. Implement middleware, authentication, cookies, idempotency, and observability.
4. Implement retry, delay, rate-limit, and `circuit-breaker` composition.
5. Implement pagination, bounded pools, cache/revalidation, and policy scoping.
6. Implement record/replay, deterministic test support, and vendor examples.
7. Fuzz, race-test, leak-test, benchmark, and document every scenario.
8. Complete compatibility/security audits and publish `v1`.

## Acceptance Criteria

The module is ready only when concrete vendor clients can adopt it without
losing HTTP control, retry safety is executable rather than aspirational,
all supported pagination models are bounded, request pools provide backpressure,
rate-limit and breaker composition cannot amplify traffic, caching preserves HTTP
semantics, request/response and stream ownership is proven, idempotency and
operation identity remain stable, policy scopes prevent cross-tenant state,
egress rules prevent trust-boundary bypass, record/replay fixtures are safe,
documentation covers all public scenarios, and every CI, safety, coverage,
security, and release gate passes.
