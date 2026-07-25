# Goal: Explicit Server HTTP Middleware Foundation

## Objective

Build a production-grade open-source Go package for composing and applying
generic server-side HTTP middleware with explicit order, ownership, security,
resource limits, and transport behavior.

The package MUST use the standard `func(http.Handler) http.Handler` model. It
MUST provide reusable HTTP transport policies without becoming a framework,
router, service runtime, authentication system, authorization engine, session
layer, template stack, dependency-injection container, or hidden middleware
registry.

## Product Principles

- Standard `net/http` types remain the public contract.
- Every middleware is independently importable and explicitly configured.
- Middleware order is deterministic, inspectable, and documented.
- Defaults are secure, conservative, and bounded.
- Request and response ownership is explicit at every short-circuit.
- Optional `ResponseWriter` capabilities are preserved where promised.
- Trusted-proxy and origin decisions are deny-by-default security boundaries.
- No middleware performs hidden network calls, background refresh, or global
  registration.
- Concern-specific policy remains in its owning package.
- Convenience MUST NOT conceal control flow or error behavior.

## Authoritative Semantics

The implementation MUST track applicable current primary standards, including:

- Go `net/http`, `http.ResponseController`, request, response, server, and
  handler contracts;
- RFC 9110 HTTP semantics;
- RFC 9111 HTTP caching where headers or conditional behavior are affected;
- RFC 7239 `Forwarded` syntax and security considerations;
- the Fetch standard's CORS protocol;
- W3C Trace Context for propagation adapters;
- applicable content-coding, MIME, origin, cookie, and security-header
  specifications.

Each middleware MUST document exactly which standards behavior it implements.
Do not claim complete compliance from a few happy-path header tests.

## Core Composition Model

- Define `Middleware` as or compatibly with
  `func(http.Handler) http.Handler`.
- `Chain` composes an explicit ordered list around one terminal handler.
- Document request execution order and response unwind order.
- Reject nil handlers and nil middleware with typed construction errors.
- Support immutable named descriptors for introspection without a registry.
- Allow chain concatenation, prefix/suffix composition, and explicit
  conditional application.
- Conditional middleware uses caller-supplied predicates and documented panic,
  cancellation, and error policy.
- A resolved chain can be inspected before serving.
- Composition performs no reflection, dependency resolution, alias lookup, or
  `init` registration.
- Duplicate middleware is allowed only when semantics support it or rejected by
  explicit name/policy validation.

## Ownership Boundary

This package owns generic HTTP transport middleware such as:

- request and correlation identifiers;
- panic recovery and safe terminal responses;
- request body limits;
- request deadline and handler timeout policies;
- trusted proxy and effective client information;
- CORS;
- configurable response security headers;
- response compression and content-coding negotiation;
- access-log event construction through injected observers;
- generic request/response telemetry hooks;
- content-type and Accept enforcement helpers;
- bounded in-flight request admission;
- cache-control and no-store response policy helpers;
- maintenance and readiness admission helpers where they remain transport-only.

This package does not own:

- credentials or authentication: `authentication`;
- access decisions: `authorization`;
- rate algorithms and distributed quotas: `rate-limit`;
- idempotency records or operation semantics: `idempotency`;
- tracing SDK/exporter lifecycle: `telemetry`;
- logging backends: `log` and `log/slog`;
- routing: `router`;
- server/process lifecycle: `service`;
- response caching stores or refresh algorithms: `cache` and application
  policy;
- circuit-breaker state: `circuit-breaker`.

Adapters MAY make owning-package middleware compose consistently but MUST NOT
reimplement its policy or state machine.

## Request Identifiers

- Generate bounded cryptographically appropriate or caller-supplied identifiers.
- Accept inbound identifiers only under explicit trust and validation policy.
- Distinguish request ID, correlation ID, trace ID, operation ID, and
  idempotency key.
- Reject or replace invalid, oversized, control-character, or multi-value
  identifiers according to named policy.
- Propagate through request context and configured response header.
- Never use identifiers as authorization evidence.
- Avoid unbounded metric labels and automatic payload logging.
- Resolve ownership overlap with `service`: one implementation MUST become
  authoritative and integration MUST not install duplicate ID middleware.

## Panic Recovery

- Recover panics originating below the middleware boundary.
- Re-panic runtime-fatal conditions where Go guidance requires it.
- Produce a minimal safe response only when headers are not committed.
- Define behavior after partial headers/body, streaming, flushing, hijacking,
  and connection upgrade.
- Preserve bounded panic classification for injected observers without
  exposing stack traces or panic values to clients.
- Stack capture is optional, bounded, redacted, and caller-observed.
- Recovery MUST not disguise context cancellation or normal abort behavior.
- Resolve ownership overlap with `service` and prevent duplicate recovery
  layers through documented composition.

## Body Limits And Request Decoding Boundaries

- Apply maximum body bytes before application reads or decodes.
- Use standard-library bounded readers where semantics are sufficient.
- Distinguish missing length, known oversized length, streaming overflow,
  compressed request bodies, multipart parsing, and already-read bodies.
- Return standards-aligned status and connection behavior.
- Define whether unread bytes are drained or the connection is closed, with
  strict limits and no unbounded draining.
- Preserve close ownership and cancellation behavior.
- Middleware MUST not parse JSON, XML, forms, or application payloads merely to
  enforce byte limits.

## Deadlines And Timeouts

- Apply request context deadlines with explicit clock/timer ownership.
- Distinguish handler deadline, server read/write timeout, upstream timeout,
  idle timeout, and shutdown deadline.
- Do not promise interruption of arbitrary code that ignores context.
- Define safe response behavior before and after headers are committed.
- Bound buffering if timeout behavior requires a response recorder; streaming
  handlers MUST use an explicit compatible policy or reject timeout wrapping.
- Preserve parent deadlines and never extend them accidentally.
- Stop timers and release goroutines on every path.
- Integrate with `clock` only where deterministic time control warrants it.

## Trusted Proxies And Effective Request Information

- Direct connection information is authoritative unless a configured trusted
  proxy boundary applies.
- Support RFC 7239 `Forwarded` and explicitly configured de facto
  `X-Forwarded-*` fields.
- Parse multiple hops, IPv4, IPv6, ports, obfuscated identifiers, quoted
  strings, and malformed values with strict bounds.
- Trust is based on explicit proxy IP/network policy, not header presence.
- Define trusted-hop traversal and first-untrusted-client selection precisely.
- Strip, ignore, or reject untrusted forwarding fields according to policy.
- Effective client IP, host, scheme, and prefix are separate values.
- Do not mutate `Request.RemoteAddr`, URL, Host, or TLS state silently.
- Context values identify provenance and trust level.
- Prevent host-header injection, spoofed secure scheme, open redirect support,
  internal topology disclosure, and cross-tenant trust confusion.

## CORS

- Implement the Fetch standard's CORS protocol for origin requests and
  preflight responses within documented server scope.
- Explicit allowed origins, methods, headers, exposed headers, credentials,
  private-network behavior if supported, and maximum age.
- Wildcard and credentials combinations MUST be validated correctly.
- Origin comparison handles scheme, host, port, serialized null origin, IDNA,
  case, default ports, and malformed values explicitly.
- Dynamic origin predicates are injected, bounded, cancellation-aware where
  applicable, and never globally registered.
- Preflight short-circuiting and application OPTIONS routes compose
  predictably.
- Correct `Vary` behavior prevents cache confusion.
- CORS is not authentication or CSRF protection and MUST never be documented as
  either.
- No browser session or CSRF-view framework is introduced.

## Security Headers

- Provide explicit immutable policies for relevant response headers.
- Support practical API defaults without assuming HTML rendering.
- Avoid obsolete, contradictory, or browser-ignored headers.
- Existing handler headers follow one documented replace, merge, preserve, or
  reject policy per field.
- Content Security Policy is optional and explicit; no generic policy can infer
  safe application scripts or templates.
- HSTS requires explicit deployment acknowledgement because incorrect use can
  create long-lived outages.
- Nonce generation, if offered, is explicit context data and does not imply a
  templating engine.
- Headers MUST be applied correctly on success, redirects, errors,
  short-circuits, and panic recovery according to configured order.

## Response Compression

- Negotiate supported content codings from `Accept-Encoding` with correct
  quality values, wildcard, identity, absent, empty, duplicate, and malformed
  behavior.
- Set `Content-Encoding` and merge `Vary: Accept-Encoding` correctly.
- Do not compress no-body statuses, HEAD bodies, upgrades, ranges, already
  encoded responses, excluded media types, or responses below configured size.
- Define `Content-Length`, ETag, digest, cache, and trailer interactions.
- Preserve flushing and streaming only when the encoder and response contract
  can do so safely.
- Bound compression level, buffers, pools, output, and retained memory.
- Close and return encoders to pools on every success, error, panic, and
  cancellation path.
- Prevent compression side-channel claims from being hidden; sensitive dynamic
  responses require explicit opt-out or policy.
- Additional codecs are additive and dependency-isolated.

## Access Observation And Telemetry Hooks

- Emit one bounded completion event per request through an injected observer.
- Include method, matched route metadata when available, status, bytes,
  duration, protocol, outcome, and trusted client classification.
- Do not include raw path, query, headers, payload, credentials, record IDs,
  tenant values, or error text by default.
- Status and byte recording MUST handle implicit status, informational
  responses, partial writes, `ReadFrom`, flushing, hijacking, panic, and
  cancellation.
- Preserve `Flusher`, `Hijacker`, `Pusher`, `ReaderFrom`, and
  `ResponseController` behavior only where accurately supported.
- Observer panic policy is explicit and cannot corrupt the response.
- `log` and `telemetry` adapters consume events; core has no exporter,
  global logger, tracer, or meter.

## Content Negotiation Helpers

- Optional middleware can require or select accepted request/response media
  types without owning serialization.
- Parse media types, parameters, wildcards, quality values, duplicates, and
  malformed headers with bounded work.
- Distinguish missing `Content-Type` on empty and non-empty bodies.
- Return correct 406 and 415 outcomes where policy requires them.
- Do not decode request bodies or encode responses.
- `wire`, `jsonapi`, `jsonrpc`, and applications own representation
  formats and protocol-specific negotiation.

## In-Flight Admission And Backpressure

- Bound concurrent requests through context-aware admission.
- Support immediate rejection and bounded wait policies.
- Cancellation and shutdown release every permit exactly once.
- Define fairness and starvation behavior.
- Reject with explicit status and optional standards-aligned retry guidance.
- This is local concurrency protection, not distributed rate limiting.
- No hidden goroutine per waiter or unbounded queue.
- Metrics use bounded states and never individual requester identities.

## Cache-Control And Maintenance Helpers

- Provide declarative response header policies such as `no-store` for sensitive
  administrative and payload endpoints.
- Do not implement a response cache, stale refresh, request collapsing, or
  storage backend in core.
- Maintenance/readiness admission may short-circuit through an injected
  immutable or concurrency-safe state source.
- Health and readiness handlers remain owned by `service`.
- Policy decisions are explicit, inspectable, and safe under concurrent reads.

## ResponseWriter Compatibility

- Every wrapping middleware documents supported optional interfaces.
- Use `http.ResponseController` where it provides stable capability forwarding.
- Do not expose an optional interface unless the underlying writer and wrapper
  can honor it correctly.
- Test HTTP/1.1, HTTP/2, streaming, flushing, hijacking, push where supported,
  trailers, `io.ReaderFrom`, informational responses, and full duplex.
- Nested wrappers MUST preserve capabilities or report loss explicitly.
- Wrapper behavior is part of the public compatibility contract.

## Errors And Short-Circuit Responses

- Construction errors are typed and support `errors.Is`/`errors.As`.
- Runtime middleware does not return Go errors through `http.Handler`; it uses
  explicit injected observers and bounded safe HTTP responses.
- Shared response helpers are minimal, deterministic, and content-type safe.
- Error bodies never expose panic values, configuration, credentials, internal
  addresses, stack traces, or arbitrary wrapped error strings.
- Applications may inject protocol-specific error writers explicitly.
- JSON-RPC and JSON:API errors remain owned by their protocol packages.

## Concurrency And Lifecycle

- Configured middleware values are immutable or explicitly concurrency-safe.
- No package-global state, registry, default chain, background goroutine,
  exporter, refresher, or shutdown hook.
- Every timer, permit, buffer, encoder, observer call, and context value has
  explicit ownership.
- Mutable caller policies require documented synchronization contracts.
- Construction is separate from request handling.
- Request pools MUST not retain credentials, headers, paths, payloads, or large
  buffers across requests.

## Security And Resource Bounds

- Bound headers, origins, methods, proxy hops, identifiers, body bytes,
  middleware depth, timeout buffers, waiters, compressed output, observer
  fields, diagnostics, and configuration values.
- Threat-model slowloris interaction, request smuggling assumptions, spoofed
  forwarding headers, host injection, origin confusion, CORS cache poisoning,
  response splitting, CRLF injection, compression bombs, side channels,
  middleware-order bypass, panic disclosure, and resource exhaustion.
- Header values produced by the package reject control characters.
- Middleware ordering requirements that affect security MUST be machine-
  inspectable and tested through representative chains.
- Production code MUST NOT use unsafe, cgo, `go:linkname`, reflection-based
  discovery, hidden globals, or runtime patching.

## Explicit Non-Goals

- No router, controller layer, controller injection, action resolution, model
  binding, ORM, request DTO reflection, or automatic validation.
- No sessions, flash data, CSRF form/view framework, cookies-as-server-session
  state, authentication guard, or login system.
- No templates, HTML components, frontend assets, redirects-to-routes, or view
  composers.
- No service container, auto-wiring, facade, service locator, annotation
  scanning, middleware aliases, kernel registration, or magic groups.
- No protocol-specific JSON-RPC, JSON:API, SOAP, webhook, or business policy.
- No mandatory logger, telemetry SDK, cache, database, queue, or configuration
  dependency.
- No claim that middleware can replace secure ingress, proxy, server, or
  application configuration.

## Package Shape

- Root: middleware type, chain, descriptors, conditions, errors, and inspection.
- `requestid`: request and correlation identifiers.
- `recovery`: panic containment and safe observation.
- `bodylimit`: bounded request bodies.
- `deadline`: request deadline and timeout policies.
- `proxy`: trusted forwarded information.
- `cors`: Fetch-aligned CORS protocol.
- `secureheader`: explicit response security policies.
- `compress`: content-coding negotiation and response compression.
- `observe`: access and generic telemetry event construction.
- `content`: media-type and Accept enforcement.
- `admission`: bounded local in-flight requests.
- `responsepolicy`: cache-control, no-store, and maintenance helpers.
- `middlewaretest`: chains, recorders, optional-interface fixtures, and
  deterministic assertions.
- `adapter`: optional owning-package integrations without dependency cycles.

Subpackages MUST remain independently importable. Importing the root MUST not
pull every optional codec or integration dependency.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Tests MUST prove
transport, security, ownership, and ordering behavior rather than merely
execute statements.

Required evidence includes:

- exhaustive composition, execution, unwind, nil, duplicate, conditional,
  short-circuit, panic, and cancellation matrices;
- real-listener HTTP/1.1 and HTTP/2 integration tests;
- `ResponseWriter` optional-interface and nested-wrapper compatibility tests;
- request ID trust, proxy chain, CORS, security header, encoding negotiation,
  body limit, timeout, content negotiation, and admission truth tables;
- differential parsing against standard-library functions where semantics
  overlap;
- hostile header, origin, forwarding, encoding, media type, identifier,
  timeout, and configuration fuzzing;
- mutation testing for every trust, origin, quality, limit, ordering,
  short-circuit, and header decision;
- race and leak tests for concurrent requests, policies, timers, waiters,
  observers, compression pools, cancellation, panic, and shutdown;
- allocation-reporting benchmarks for base chains, each middleware, deep
  chains, streaming, compression, proxy parsing, and contention;
- integration chains with `router`, `service`, `authentication`,
  `authorization`, `rate-limit`, `idempotency`, and `telemetry`;
- tests proving duplicate installation with `service` cannot occur silently.

## Documentation Deliverables

- Five-minute chain and independently importable middleware quickstarts.
- Complete API and ordering reference for every exported type, policy, event,
  error, and short-circuit response.
- Security-sensitive deployment guides for trusted proxies, CORS, HSTS,
  compression, request IDs, body limits, timeouts, and middleware order.
- `ResponseWriter` capability compatibility matrix.
- Adoption guides for REST-like APIs, JSON-RPC, webhooks, admin APIs, health
  endpoints, and Kubernetes services.
- Integration cookbook for all owning packages and explicit recommended chains.
- Migration guide from ad hoc `net/http`, Laravel middleware, and common Go
  middleware without reproducing framework magic.
- Performance, security, compatibility, FAQ, troubleshooting, architecture,
  examples, cookbook, contribution, release, and maintained changelog docs.
- Every user-facing scenario and exported API MUST be documented sufficiently
  for adoption without reading implementation source.

## Repository And Release Requirements

Use the latest stable Go release as the minimum at implementation time. Pin all
tools and dependencies. GitHub Actions MUST run formatting, vet, Staticcheck,
strict golangci-lint, advisory NilAway, tests, meaningful 100% coverage, race,
fuzz smoke, mutation checks, HTTP integration, standards compatibility,
vulnerability scans, benchmarks, docs, API compatibility, and signed releases.

Every blocking command MUST be reproducible locally through documented `make`
targets. Repository setup MUST include README badges for every blocking
workflow/job, Dependabot, security policy, contribution guide, code of conduct,
license, notice and third-party attribution handling, release automation,
changelog, repository topics, and complete adoption documentation.

## Execution Plan

1. Freeze composition, ordering, ownership, optional-interface, error, limit,
   and package-boundary contracts.
2. Implement chain and foundational request ID, recovery, body limit, deadline,
   proxy, and observation middleware.
3. Implement standards-backed CORS, security headers, compression, content
   negotiation, admission, and response policies.
4. Integrate with router, service, authentication, authorization, limiter,
   idempotency, logging, and telemetry packages without duplicate ownership.
5. Complete standards, security, fuzz, race, mutation, leak, and performance
   hardening.
6. Publish complete adoption, ordering, migration, and deployment documentation
   and release v1.

## Acceptance Criteria

- Every middleware is explicit, independently configured, and inspectable.
- Chain order and response unwind behavior are deterministic and documented.
- Trusted proxy, CORS, compression, timeout, and header behavior is standards-
  aligned, bounded, and adversarially tested.
- ResponseWriter capabilities are preserved or limitations are explicit.
- No concern-specific policy is duplicated from another owned package.
- Track, Postal, and Location can build consistent server chains without a
  framework, container, sessions, templates, or hidden magic.
- Meaningful 100% coverage and every required local and CI gate pass.
