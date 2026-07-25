# Hardening Goal: Explicit HTTP Routing Foundation

## Objective

Prove that `router` is standards-aligned, deterministic, immutable, bounded,
panic-free, concurrency-safe, and interoperable across every route pattern,
method, host, wildcard, conflict, middleware chain, mount, metadata value, URL
generation input, request target, and standard-library compatibility boundary.

## Required Audits

### Standard Library Compatibility Audit

- Differential-test every supported pattern and request class against the
  minimum Go version's `http.ServeMux`.
- Reconcile GET/HEAD, OPTIONS, 404, 405, `Allow`, specificity, host precedence,
  wildcard, escaped segment, redirect, and canonical path behavior.
- Verify registration panic conversion catches only controlled route errors.
- Record every deliberate divergence with rationale and migration guidance.

### Registration And Conflict Audit

- Exhaust malformed, empty, duplicate, overlapping, shadowed, ambiguous, host,
  method, prefix, wildcard, exact-root, and mounted pattern combinations.
- Prove results never depend on map or registration order where the contract
  promises order independence.
- Mutation-test every validation, conflict, precedence, and compile decision.
- Prove failed compilation cannot leave a partially usable router.

### Dispatch And HTTP Semantics Audit

- Exhaust methods, hosts, paths, escaped values, trailing slashes, HEAD,
  OPTIONS, CONNECT, asterisk-form targets, and unsupported methods.
- Verify `PathValue`, request URL, context, response status, and headers at every
  dispatch outcome.
- Test custom and default not-found/method-not-allowed handlers under partial
  writes, panic middleware, cancellation, and malformed requests.
- Prove route matching never performs hidden I/O, dependency lookup, or runtime
  discovery.

### Groups, Middleware, And Mounts Audit

- Property-test nested prefix, host, name, metadata, and middleware composition.
- Prove exact request and response unwind order for router, group, mount, and
  route middleware.
- Test nil, duplicate, short-circuiting, panicking, re-entering, streaming,
  hijacking, flushing, and cancellation-aware middleware.
- Verify mounted handlers and routers preserve explicit path, path-value,
  metadata, and lifecycle boundaries.

### URL Generation Audit

- Fuzz route names, parameter names and values, remainders, schemes, hosts,
  ports, query values, Unicode, IDNA, percent escapes, separators, and limits.
- Property-test generated URL to intended route round trips.
- Attack traversal, encoded slash, open redirect, host injection, query
  injection, double escaping, missing parameters, and extra parameters.
- Prove generation performs no model reflection or implicit identifier lookup.

### Immutability, Concurrency, And Resources

- Race/stress concurrent dispatch, introspection, URL generation, cancellation,
  and handler behavior against one compiled router.
- Prove route descriptors, metadata, middleware lists, and route tables cannot
  be mutated through caller-owned values.
- Enforce route, group, nesting, middleware, metadata, pattern, parameter,
  query, and output budgets under hostile input.
- Prove no hidden goroutine, global registry, cache, watcher, unsafe, cgo,
  `go:linkname`, reflection discovery, or leak remains.

## Required Deliverables

- Standard-library compatibility and divergence matrix.
- Pattern, conflict, precedence, method, redirect, and path-value truth tables.
- Middleware ordering, mount, metadata, and URL-generation behavior matrices.
- Fuzz, mutation, race, aliasing, security, and benchmark evidence.
- Enforced resource budgets and worst-case complexity documentation.
- Updated API, adoption, migration, security, performance, FAQ, and
  troubleshooting documentation.

## Release Blockers

- Any wrong route, unstable match, missing method, incorrect HEAD/OPTIONS/Allow
  behavior, path confusion, conflict panic, URL injection, mutable alias, race,
  leak, hidden registration, unbounded work, or undocumented divergence.
- Any controller injection, model binding, session, templating, container,
  service-locator, annotation, reflection-discovery, or global-router magic.
- Any public behavior without meaningful tests and complete documentation.
- Missing meaningful 100% coverage, mutation evidence, or green blocking CI.

## Completion Criteria

- Compatibility, registration, dispatch, group, middleware, mount, metadata,
  URL, security, and integration suites pass.
- Every operation survives enforced limits and adversarial inputs.
- Race, fuzz, mutation, vulnerability, compatibility, and performance gates
  pass.
- NilAway runs visibly as advisory without blocking findings.
- No release blocker remains and the changelog is current.
