# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Use a deterministic execution budget for default fuzz smoke campaigns while
  preserving explicit duration overrides for extended fuzzing.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.

### Security

- Require HTTPS for trusted authentication origins by default, with an
  explicit insecure opt-in limited to local test endpoints.
- Verify that the insecure-origin opt-in permits HTTP only and still rejects
  non-HTTP credential origins.
- Reject direct-request origin userinfo and malformed ports before
  credential, session, scope, telemetry, or fixture origin policy uses them.
- Contain caller decoder and transfer-progress panics as typed secret-safe
  failures while preserving deterministic response-body closure.
- Close transformed request bodies when middleware short circuits before a
  physical transport, preventing blocked compression workers.

### Changed

- Expand the fuzz smoke gate with redirect credential-boundary and retry-policy
  targets plus a retained empty-method corpus case.
- Expand allocation benchmarks across request policy, pagination, pools,
  cache states and stampedes, limiter/breaker composition, body processing,
  request construction and serialization, policy scopes, and large fixtures.
- Clarify that `Client.HTTPClient()` bypasses operation identity, middleware,
  and target-URL egress policy.
- Pin current analyzer versions and keep pull-request and tag-release workflow
  prerequisites identical.

### Added

- Add public API, transport, typed integration, error, testing, security,
  compatibility, migration, performance, FAQ, and troubleshooting guides.
- Add executable GitHub REST and Ethereum JSON-RPC adoption examples that use
  deterministic local TLS servers and keep vendor types, status semantics, and
  protocol errors outside core.
- Add aggregate process-exit goroutine leak detection to normal, race, and
  uncached release-gate test execution.
- Add MIT licensing plus contribution, conduct, governance, security, support,
  issue, pull-request, attribution, and release policies for OSS operation.
- Add CI and local gates for format, vet, lint, tests, race detection, complete
  production coverage, fuzz smoke tests, benchmarks, docs, vulnerabilities,
  `GO-SAFETY-1`, and tagged GitHub releases.
- Fuzz hostile URLs, headers, authentication inputs and challenges, and bounded
  vendor error payload classification.
- Add live HTTP/1.1, HTTP/2, proxy, connection-reuse, and total-timeout
  integration coverage for the standard client contract.
- Add context-aware circuit outcome classification so caller cancellation is
  distinguishable from dependency-produced cancellation errors.
- Add an outbound HTTP client with finite total, connection, TLS handshake,
  response-header, idle-connection, and response-header-size limits.
- Add immutable egress policy for schemes, hosts, ports, origins, CIDRs,
  private address classes, metadata services, redirects, proxies, and
  connection-time DNS rebinding defense.
- Add immutable TLS policy for protocol minimums, private roots, fixed server
  names, client certificates, and rotating SHA-256 SPKI pins.
- Add opaque resource-specific policy scopes for origin, host, endpoint,
  credential, tenant, account, and caller-defined dimensions, with cache and
  coalescing identity separation.
- Add named versioned workload profiles with finite policy defaults,
  deterministic client and request overrides, and operation/attempt
  provenance inspection.
- Add optional operation/attempt telemetry, safe `slog` hooks, W3C Trace
  Context propagation, baggage allowlists, trust-boundary stripping, and
  closed low-cardinality metric labels.
- Add strict ordered scripted HTTP fixtures plus bounded sanitized recording,
  versioned persistence, explicit migration and expiry, stable failure modes,
  response trailers, and unused-interaction verification.
- Add explicit borrowed and owned transport lifecycles.
- Add deterministic client shutdown that cancels pending requests, closes
  active response bodies, and drains owned idle connection pools.
- Add typed transport errors that retain their cause without displaying URL
  credentials, query parameters, or fragments.
- Add immutable request specifications with same-origin URL resolution,
  non-aliasing request builds, and explicit metadata precedence.
- Add repeated, comma-delimited, space-delimited, pipe-delimited, deep-object,
  null, empty, omitted, and structurally custom query serialization.
- Add replayable byte and factory bodies plus explicitly one-shot streaming
  bodies with content metadata, `GetBody`, and typed open failures.
- Add canonical replayable form URL encoding with snapshotted values,
  deterministic key ordering, and preserved repeated-value order.
- Add deterministic bounded multipart request bodies with explicit part
  metadata, replay-derived retry safety, exact known lengths, streaming limit
  enforcement, and joined reader ownership.
- Add immutable layered request trailers with prohibited-field validation,
  independent request snapshots, replay preservation, and proven standard
  transport delivery.
- Add immutable operation and attempt middleware pipelines with explicit
  stages, registration layers, priorities, names, and resolved inspection.
- Add request and transport short-circuiting, response replacement, error
  recovery, completion hooks, cancellation propagation, and panic containment.
- Run attempt middleware for every physical `RoundTrip`, including redirects,
  while operation middleware runs once around the logical client call.
- Add immutable Basic, bearer, header API-key, explicit query API-key, and
  vendor-configurable HMAC request editors.
- Add origin-bound authentication middleware that reapplies credentials per
  attempt and strips sensitive headers across untrusted redirects.
- Add `golang.org/x/oauth2` token-source editors and a context-aware outbound
  client-credentials source with coordinated refresh and cancelable waiters.
- Send client-credentials token requests through the hardened standard
  transport while bypassing integration middleware, cookie jars, and
  integration retries.
- Add opt-in per-client cookie jars with a public-suffix default, same-origin
  redirect policy, explicit custom-jar ownership, and no ambient global jar.
- Add bounded session persistence loading, manual load/save operations, and
  save-on-close lifecycle with secret-safe typed errors.
- Add cryptographically random logical operation identity that remains stable
  across physical attempts and changes for every distinct client call.
- Add explicit endpoint idempotency middleware with caller and generated keys,
  entropy and length validation, provenance, redaction, and redirect policy.
- Order middleware by stage, priority, layer, and name so identity and
  idempotency policy can precede authentication across registration layers.
- Add bounded operation retry middleware with replay and method safety,
  explicit unsafe-endpoint opt-in, exponential jitter, `Retry-After`, and
  context-aware deterministic delay seams.
- Add typed, secret-safe retry exhaustion errors and bounded draining of every
  response discarded before another physical attempt.
- Add fixed-window, sliding-window, token-bucket, and bounded leaky-bucket
  admission controllers with context-aware maximum waits.
- Add per-attempt rate-limit middleware that observes RFC `Retry-After` and
  configurable vendor remaining/reset headers to defer future admission.
- Add logical-operation circuit-breaker middleware, HTTP outcome
  classification, fail-fast typed rejection, and a first-party
  `circuit-breaker` adapter without duplicating breaker state.
- Move initial limiter admission ahead of breaker admission while preserving
  exactly one reservation for every retry and redirect transport attempt.
- Add lazy typed pagination with resumable buffered state and finite page,
  item, byte, elapsed-time, empty-page, and continuation bounds.
- Add page-number, offset/limit, opaque-cursor, RFC Link-header, and custom
  continuation strategies with cycle detection and deterministic errors.
- Add typed bounded request pools for slices, generators, and channels with
  backpressure, stable keys, configurable result ordering, partial failures,
  cancellation, dynamic concurrency, and finite run-wide budgets.
- Add optional RFC-aware HTTP caching with finite in-memory storage, freshness
  and age calculation, validation, protected `Vary` identities, coalescing,
  bounded stale behavior, explicit controls, and same-origin invalidation.
- Add bounded streaming JSON response decoding with media-type validation,
  strict document boundaries, explicit empty semantics, and consume-and-close
  ownership.
- Add bounded caller-selected typed response codecs with explicit media-type
  allowlists, unread trailing-data policy, and the same consume-and-close
  ownership contract.
- Add shared declared response-length validation for JSON and custom codecs,
  including explicit-zero, unknown-length, and semantic-empty behavior.
- Add a public bounded response drain-and-close helper with exact-limit EOF
  detection and secret-safe typed read, close, and overflow failures.
- Add independent HTTP status classification with accepted-body preservation,
  bounded rejection draining, mandatory excerpt redaction, vendor mapping,
  request identity, and retryability metadata.
- Add explicit streaming gzip request and response policy with replay-safe
  request factories, deterministic worker shutdown, absolute decoded-size
  limits, and compressed-to-decompressed ratio protection.
- Add bounded response-to-writer transfers with explicit ownership, context
  cancellation, throttled progress, length checks, and constant-time SHA-256
  or SHA-512 validation.
- Add atomic response-to-file replacement with same-directory temporary files,
  restrictive modes, validation before rename, durable sync, and cleanup on
  failure.
- Add strict byte-range request and response policy with strong `If-Range`
  validators, `Content-Range` checks, and explicit continue, restart, or
  already-complete dispositions.
- Add resumable file downloads with persistent same-directory partials,
  validator-safe append, automatic full-response restart, append rollback,
  whole-file digest validation, and atomic publication.
