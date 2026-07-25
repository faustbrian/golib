# Goal: Explicit HTTP Routing Foundation

## Objective

Build a production-grade open-source Go router that extends the standard
`net/http` programming model with deterministic route composition, groups,
names, reverse URL generation, metadata, introspection, and route-scoped
middleware.

The package MUST remain an explicit routing library, not an application
framework. It MUST preserve `http.Handler`, `http.HandlerFunc`,
`http.ResponseWriter`, `*http.Request`, and `Request.PathValue` as the public
runtime model.

The package MUST NOT introduce controller injection, model binding, sessions,
CSRF views, templates, a service container, reflection-driven handler
discovery, annotations, hidden global registration, or implicit application
bootstrapping.

## Product Principles

- Prefer standard-library HTTP semantics and types.
- Make every route, middleware layer, default, and conflict inspectable.
- Registration is explicit and startup-time; dispatch performs no discovery.
- Invalid configuration returns typed errors instead of panicking.
- Matching and URL generation are deterministic and registration-order safe.
- Route parameters remain available through `Request.PathValue`.
- Middleware is ordinary `func(http.Handler) http.Handler` composition.
- A compiled router is immutable and safe for concurrent request handling.
- No route registration occurs through `init`, package globals, reflection, or
  filesystem scanning.
- Features MUST justify their complexity beyond what `http.ServeMux` provides.

## Authoritative Semantics

The implementation MUST track and document:

- the supported Go `net/http` routing and request-path behavior;
- RFC 9110 HTTP method, status, `Allow`, HEAD, and OPTIONS semantics;
- RFC 3986 URI syntax and percent-encoding behavior where URL generation uses
  URI components;
- relevant URL and host parsing behavior from the Go standard library;
- Unicode, IDNA, proxy, and canonical path security considerations where
  applicable.

When this package deliberately differs from `http.ServeMux`, the difference,
rationale, compatibility impact, and migration behavior MUST be explicit.

## Route Definition

Provide an explicit route descriptor containing:

- optional stable name;
- HTTP method or method set;
- optional host pattern;
- path pattern;
- `http.Handler`;
- ordered route middleware;
- bounded immutable metadata;
- documentation and operation identifiers where supplied;
- source label for diagnostics without runtime reflection.

Construction MUST validate before runtime dispatch:

- empty and malformed methods, hosts, paths, names, and wildcard identifiers;
- duplicate names and duplicate semantic routes;
- ambiguous or conflicting patterns;
- missing or nil handlers and middleware;
- invalid prefix/group composition;
- metadata and route-count limits;
- unsupported path or host syntax.

Public registration APIs MUST return errors. A standard-library panic caused by
an invalid or conflicting registration MUST be contained and translated into a
typed startup error with safe route diagnostics.

## Matching Semantics

- Support method, host, literal path, single-segment wildcard, remainder
  wildcard, and exact trailing-slash matching required by current services.
- Preserve Go's `GET` and HEAD relationship unless an explicit route policy
  overrides response handling without violating HTTP semantics.
- Distinguish not found, known method not allowed, unsupported method, invalid
  target, and redirect behavior.
- Generate correct bounded `Allow` headers for 405 and OPTIONS behavior.
- Define automatic OPTIONS behavior separately from explicitly registered
  OPTIONS handlers.
- Define canonical path and trailing-slash redirects through explicit policy.
- Preserve escaped path-segment boundaries and avoid decoding `%2F` into a
  structural separator accidentally.
- Host matching MUST account for ports and validated authority values.
- CONNECT and asterisk-form request targets require explicit supported or
  rejected behavior.
- Matching MUST not mutate request URL or context unexpectedly.
- Path values MUST be available through `Request.PathValue` without a parallel
  parameter API becoming mandatory.

## Standard Library Strategy

- Use `http.ServeMux` as the matching core where its semantics satisfy the
  package contract.
- Do not copy Go's internal matcher merely to offer alternate method names.
- Add package-owned matching only where required for capabilities that cannot
  be implemented safely through `ServeMux`.
- Freeze the exact supported relationship with `ServeMux` in compatibility
  tests for the minimum Go version.
- Convert registration panics to errors without suppressing unrelated panics.
- Preserve standard `http.Handler` optional-interface and request behavior.
- Do not use `unsafe`, `go:linkname`, copied internal standard-library source,
  or unsupported access to `net/http` internals.

## Groups And Prefixes

- Grouping composes host, path prefix, middleware, metadata, and naming prefix.
- Nested groups produce one deterministic flattened route definition.
- Group builders are immutable or have explicit single-owner build semantics.
- Prefix joins MUST define slash, root, wildcard, escaped-segment, and empty
  behavior without `path.Clean` changing route meaning silently.
- Group middleware order is visible and stable from outermost to innermost.
- Route middleware is applied after inherited group middleware according to one
  documented execution order.
- Group metadata merge and conflict policy is explicit.
- A group has no runtime lifecycle and performs no dependency resolution.

## Middleware Composition

- Accept the standard middleware shape or a package alias directly assignable
  to it.
- Support router-wide, group, mounted-router, and route middleware.
- Expose the resolved middleware chain through introspection.
- Define request order and response unwind order precisely.
- Reject nil middleware and detect duplicate named middleware where requested.
- Allow explicit middleware exclusion only through visible route configuration,
  never string aliases resolved from global state.
- Middleware MUST NOT be instantiated through reflection or a container.
- Concern-specific middleware comes from `http-middleware`,
  `authentication`, `authorization`, `rate-limit`,
  `idempotency`, and `telemetry`.

## Named Routes And URL Generation

- Route names are optional, unique, stable, and SemVer-governed when published.
- Generate relative paths and absolute URLs from named routes.
- Require every path and host wildcard exactly once unless it has an explicit
  optional/default contract supported by the route syntax.
- Reject missing, unknown, duplicate, or unused parameters.
- Percent-encode each path segment correctly and preserve slash boundaries.
- Remainder wildcards require an explicit segmented or trusted-path input type;
  raw string interpolation MUST NOT enable traversal or structural injection.
- Query values use `url.Values` semantics with deterministic encoding.
- Scheme and host for absolute URLs are explicit inputs or immutable trusted
  configuration, never inferred from untrusted forwarding headers by default.
- URL generation MUST round-trip to the intended route in property tests.
- No implicit model-to-route binding or object identifier reflection.

## Mounting And Composition

- Mount standard `http.Handler` values at explicit host/path boundaries.
- Mount another compiled router without copying hidden global state.
- Define path stripping, preserved request target, and nested `PathValue`
  behavior explicitly.
- Detect route conflicts across mounted routers before serving.
- Preserve each mounted handler's lifecycle ownership outside the router.
- Mounting MUST not imply middleware, authorization, or dependency inheritance
  beyond explicitly composed configuration.
- JSON-RPC, OpenRPC discovery, webhooks, health endpoints, metrics, and debug
  handlers remain ordinary mounted handlers.

## Route Metadata And Introspection

- Provide an immutable route table after compilation.
- Expose name, methods, host, pattern, parameter names, middleware identifiers,
  operation identifier, and bounded caller metadata.
- Stable deterministic ordering MUST not depend on map iteration.
- Introspection MUST not expose handler pointers, function names, secrets, or
  mutable internal state.
- Route metadata MAY support `openrpc`, documentation, telemetry, and
  authorization adapters without making those dependencies mandatory.
- Provide safe route-table output for diagnostics and startup validation.
- Runtime request matching MAY expose the matched route descriptor through an
  explicit context value owned by this package, but handlers continue using
  `Request.PathValue` for parameters.
- Metadata MUST NOT become an unbounded telemetry label source.

## Error Handling

- Typed errors distinguish invalid routes, conflicts, duplicate names, invalid
  parameters, generation failures, unsupported behavior, and compile state.
- Errors support `errors.Is` and `errors.As` for caller policy.
- Error text is deterministic, bounded, and safe for startup logs.
- Custom not-found and method-not-allowed handlers are explicit options.
- Default error responses are minimal, standards-aligned, and do not disclose
  route inventories or internal details.
- Partial response and panic handling belong to middleware, not router matching.

## Concurrency And Lifecycle

- Route construction and compilation occur before concurrent serving.
- A compiled router is immutable and supports lock-free or bounded-lock reads.
- Registration after compilation is rejected explicitly.
- No background goroutine, watcher, registry, cache refresh, or shutdown hook.
- Dynamic route mutation is out of scope for v1.
- Handler and middleware lifecycle remains caller-owned.
- Repeated compile and failed compile behavior is documented and tested.

## Security And Resource Bounds

- Bound routes, groups, nesting, methods, wildcards, pattern bytes, name bytes,
  metadata, middleware depth, URL parameters, query values, and generated URL
  length.
- Threat-model path confusion, encoded slash handling, traversal, open
  redirects, host-header attacks, wildcard ambiguity, route shadowing, method
  confusion, metadata leaks, and denial of service.
- Validate host and absolute URL inputs against caller-supplied trusted policy.
- Avoid regex routing in core; any optional regex adapter requires explicit
  complexity and denial-of-service analysis.
- Do not log request paths, parameters, or metadata automatically.
- Production code MUST NOT use unsafe, cgo, `go:linkname`, hidden globals, or
  runtime monkey patching.

## Integration Boundaries

- `service` owns HTTP server and process lifecycle and accepts the compiled
  router as an `http.Handler`.
- `http-middleware` owns reusable generic server middleware.
- Authentication, authorization, rate limiting, idempotency, telemetry, RPC,
  webhooks, and application policy remain in their owning packages.
- `jsonrpc` supplies an HTTP handler that can be mounted at an explicit
  route; JSON-RPC methods are not HTTP routes.
- `openrpc` MAY consume route metadata through an optional adapter.
- `webhook` handlers mount explicitly and select their own authentication.
- No dependency on a logger, telemetry exporter, database, cache, queue,
  configuration loader, DI framework, or application architecture.

## Explicit Non-Goals

- No Laravel-style controller resolution or method injection.
- No model binding, ORM lookup, validation, serialization, or resources.
- No sessions, cookies-as-session state, flash state, or CSRF form/view system.
- No templating, HTML rendering, assets, frontend pipeline, or redirects based
  on named controller actions.
- No service container, dependency injection container, facade, service
  locator, auto-wiring, reflection discovery, annotation scanning, or `init`
  registration.
- No full-stack web framework, RPC dispatcher, API gateway, reverse proxy,
  load balancer, service discovery, or ingress controller.
- No hidden default router or process-global mutable route table.
- No automatic business authorization or middleware inferred from handler type.

## Package Shape

- Root: route descriptors, builder/compiler, router, errors, and introspection.
- `urlroute`: named-route URL generation if separation improves dependency
  clarity.
- `routertest`: route builders, match assertions, tables, and test helpers.
- `adapter/openrpc`: optional route-metadata integration if justified.
- `internal/compat`: standard-library compatibility fixtures only.

Keep the root comprehensible. Do not create subpackages merely to imitate a
framework directory structure.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Tests MUST prove
routing semantics and failure behavior rather than only execute statements.

Required evidence includes:

- differential matching against supported `http.ServeMux` semantics;
- exhaustive method, host, literal, wildcard, remainder, exact-root,
  trailing-slash, redirect, HEAD, OPTIONS, 404, and 405 matrices;
- conflict, specificity, duplicate, invalid-pattern, and panic-conversion tests;
- nested group and middleware execution/unwind ordering properties;
- named-route generation and match round-trip property tests;
- escaped path, encoded slash, traversal, host-header, Unicode, IDNA, and
  malformed target security tests;
- fuzzing for route patterns, host values, grouping, compilation, URL
  parameters, and request targets;
- mutation testing for method, precedence, conflict, wildcard, middleware, and
  URL-encoding decisions;
- race tests for concurrent dispatch and introspection;
- aliasing and immutability tests for descriptors, metadata, and route tables;
- benchmarks with allocations for startup compilation, large route sets,
  dispatch, middleware depth, introspection, and URL generation;
- integration tests with `service`, `http-middleware`, `jsonrpc`, and
  representative Track webhook routes.

## Documentation Deliverables

- Five-minute route, group, middleware, mount, and named-URL quickstarts.
- Complete API reference for every exported type, option, policy, and error.
- Matching, precedence, HEAD/OPTIONS, redirects, path values, escaping, and
  middleware ordering reference.
- Adoption guides for REST-like APIs, JSON-RPC mounts, webhooks, health probes,
  and mixed services.
- Migration guide from `http.ServeMux`, Laravel routes, and common third-party
  Go routers without promising unsupported compatibility.
- Security guide for paths, hosts, proxy boundaries, redirects, and metadata.
- Performance, compatibility, FAQ, troubleshooting, architecture, examples,
  cookbook, contribution, release, and maintained changelog documentation.
- Every user-facing scenario and exported API MUST be documented sufficiently
  for adoption without reading implementation source.

## Repository And Release Requirements

Use the latest stable Go release as the minimum at implementation time. Pin all
tools and dependencies. GitHub Actions MUST run formatting, vet, Staticcheck,
strict golangci-lint, advisory NilAway, tests, meaningful 100% coverage, race,
fuzz smoke, mutation checks, standard-library compatibility, vulnerability
scans, benchmarks, docs, API compatibility, and signed releases.

Every blocking command MUST be reproducible locally through documented `make`
targets. Repository setup MUST include README badges for every blocking
workflow/job, Dependabot, security policy, contribution guide, code of conduct,
license, notice and third-party attribution handling, release automation,
changelog, repository topics, and complete adoption documentation.

## Execution Plan

1. Freeze matching, registration, conflict, middleware, metadata, URL, error,
   limit, and standard-library compatibility contracts.
2. Implement explicit route construction, immutable compilation, dispatch, and
   path values without hidden registration.
3. Implement groups, middleware composition, mounts, route tables, names, and
   URL generation.
4. Add optional integration adapters without dependency cycles.
5. Complete differential, security, fuzz, race, mutation, and performance
   hardening.
6. Publish complete adoption and migration documentation and release v1.

## Acceptance Criteria

- Applications can register and inspect every route without reflection or
  global state.
- Compiled routing is immutable, deterministic, concurrency-safe, and bounded.
- Standard-library semantics are preserved or divergences are explicit.
- Middleware ordering and route metadata are visible and stable.
- Named URL generation is injection-safe and round-trips correctly.
- Track, Postal, and Location can mount HTTP, JSON-RPC, webhook, and health
  handlers without framework magic.
- Meaningful 100% coverage and every required local and CI gate pass.
