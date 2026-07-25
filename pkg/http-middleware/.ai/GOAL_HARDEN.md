# Hardening Goal: Explicit Server HTTP Middleware Foundation

## Objective

Prove that `http-middleware` is standards-aligned, deterministic, bounded,
panic-safe, leak-free, concurrency-safe, interoperable, and secure across every
chain order, request, response, short-circuit, wrapper capability, trust
boundary, origin, proxy hop, timeout, body, content coding, observer, and
hostile configuration.

## Required Audits

### Composition And Ordering Audit

- Exhaust empty, nil, single, deep, duplicated, named, conditional,
  short-circuiting, panicking, re-entering, and canceling chains.
- Prove exact request and response unwind order for every middleware pair used
  in recommended stacks.
- Make security-sensitive order constraints inspectable and regression-tested.
- Mutation-test every composition, condition, duplicate, and short-circuit
  decision.
- Prove no implicit registration, alias resolution, reflection, or global chain
  affects runtime behavior.

### ResponseWriter And HTTP Audit

- Exercise implicit/explicit status, informational responses, headers,
  trailers, no-body statuses, HEAD, partial writes, `ReadFrom`, flush, hijack,
  push, full duplex, upgrades, disconnects, and HTTP/2.
- Verify nested wrappers preserve only optional interfaces they can honor.
- Test behavior before and after headers commit for timeout, panic, compression,
  observation, and short-circuit paths.
- Differential-test standard-library semantics wherever wrappers delegate.
- Prove request and response bodies, timers, encoders, permits, and buffers are
  released on every path.

### Trust And Security Policy Audit

- Fuzz and exhaust request IDs, forwarding chains, addresses, hosts, schemes,
  origins, CORS preflights, security headers, media types, encodings, quality
  values, control characters, and malformed multi-value fields.
- Prove only configured trusted proxies influence effective client information.
- Test origin canonicalization, null origin, wildcard/credentials, default
  ports, IDNA, and `Vary` behavior against Fetch requirements.
- Attack CRLF injection, header splitting, host spoofing, open redirects,
  origin confusion, CORS cache poisoning, proxy-chain spoofing, compression
  side channels, and middleware-order bypass.
- Verify CORS is never treated as authentication or CSRF protection.

### Limits, Timeouts, And Admission Audit

- Exhaust missing, zero, negative, minimum, maximum, overflowing, streaming,
  compressed, multipart, already-read, and canceled body-limit cases.
- Prove deadlines preserve shorter parents, stop timers, release goroutines,
  and never claim to terminate context-ignoring handlers.
- Test admission fairness, cancellation, shutdown, starvation, permit leaks,
  immediate rejection, bounded waits, and overload storms.
- Enforce header, origin, proxy-hop, identifier, body, chain, timeout-buffer,
  waiter, compression, observation, and diagnostic budgets.
- Verify slow clients and hostile handlers cannot create unbounded memory,
  goroutine, timer, or connection retention through middleware-owned behavior.

### Compression And Content Audit

- Exhaust absent, empty, wildcard, identity, duplicate, invalid, and weighted
  `Accept-Encoding` values.
- Test content type, size, status, HEAD, range, upgrade, existing encoding,
  ETag, digest, `Content-Length`, trailer, flush, and cancellation interactions.
- Leak-test compressor pools and prove they do not retain sensitive or large
  data across requests.
- Fuzz media types, Accept fields, parameters, quality values, and malformed
  boundaries.
- Mutation-test every negotiate, skip, vary, size, and close decision.

### Observation And Privacy Audit

- Verify status, bytes, duration, route metadata, protocol, panic, cancellation,
  and short-circuit outcomes across all writer paths.
- Prove raw path, query, body, credentials, tenant, IDs, headers, panic values,
  and arbitrary errors are excluded by default.
- Test observer panic, slowness, cancellation, recursion, and cardinality.
- Verify `log` and `telemetry` adapters do not create exporters, duplicate
  spans/events, or package cycles.
- Establish and enforce low-cardinality event and allocation budgets.

### Ownership And Integration Audit

- Reconcile request ID and recovery ownership with `service`; one
  implementation must be authoritative and duplicate installation detectable.
- Prove authentication, authorization, rate limit, idempotency, and telemetry
  adapters delegate policy to their owning packages.
- Test representative chains with `router`, JSON-RPC, webhooks, health
  endpoints, streaming, and administrative payload views.
- Verify no router, controller injection, model binding, session, CSRF view,
  template, service container, or hidden application framework behavior enters
  the package.

### Concurrency And Dependency Audit

- Race/stress immutable policies, injected synchronized policies, chains,
  writers, pools, observers, timers, waits, cancellation, and panic recovery.
- Detect goroutine, timer, body, buffer, encoder, permit, and connection leaks.
- Audit every dependency for necessity, maintenance, license, vulnerabilities,
  and transitive cost; prefer standard library behavior where sufficient.
- Prove no unsafe, cgo, `go:linkname`, runtime patching, mutable global,
  background refresher, or hidden exporter remains.

## Required Deliverables

- Middleware execution and recommended-order matrices.
- ResponseWriter optional-interface compatibility matrix.
- Proxy trust, CORS, security-header, compression, timeout, limit, and admission
  behavior tables.
- Standards requirement and intentional-divergence report.
- Threat model and enforced resource-budget table.
- Fuzz, mutation, race, leak, HTTP integration, privacy, and benchmark evidence.
- Updated API, adoption, integration, migration, security, performance, FAQ,
  and troubleshooting documentation.

## Release Blockers

- Any order-dependent security bypass, wrong trust decision, CORS violation,
  header injection, body-limit bypass, timeout leak, permit leak, compression
  corruption, optional-interface lie, payload disclosure, race, deadlock,
  panic, hidden I/O, or unbounded operation.
- Any duplicated concern-specific state machine or ambiguous ownership with
  another package.
- Any controller injection, model binding, session, CSRF view, template,
  service-container, reflection-discovery, alias registry, or global magic.
- Any undocumented exported behavior or standards divergence.
- Missing meaningful 100% coverage, mutation evidence, or green blocking CI.

## Completion Criteria

- Composition, HTTP, writer, trust, CORS, security, limit, deadline, admission,
  compression, content, observation, ownership, and integration suites pass.
- Every operation survives enforced limits and adversarial inputs.
- Race, fuzz, mutation, leak, vulnerability, compatibility, and performance
  gates pass.
- NilAway runs visibly as advisory without blocking findings.
- No release blocker remains and the changelog is current.
