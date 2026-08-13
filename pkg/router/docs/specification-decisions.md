# Router Specification Decisions

This register records observable routing choices where HTTP, URI syntax, Go's
`net/http`, and package policy permit different outcomes. The package claims
only the scoped behavior below; it does not implement an HTTP wire stack,
proxy, application framework, or alternate copy of Go's matcher.

Each resolved entry names executable evidence. Changing one requires
compatibility, security, resource, API, conformance, and changelog review.
Superseded decisions remain linked from their replacements.

## ROUTER-DEC-001: ServeMux as the matching authority

- **Status, owner, and classification:** `resolved`; router maintainers; Go
  interoperability policy.
- **Source and issue:** Go 1.26.6 [`ServeMux`](https://cs.opensource.google/go/go/+/refs/tags/go1.26.6:src/net/http/server.go)
  defines patterns, specificity, wildcards, path values, and redirects, but a
  router could copy or reinterpret those rules.
- **Interpretations and peer behavior:** Delegate, copy internals, implement a
  separate trie, or support only literal paths. Third-party routers commonly
  differ on precedence and escaped segments.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Delegate supported literal,
  `{name}`, `{name...}`, and `{$}` path parsing, specificity, conflict,
  extraction, GET-to-HEAD, and redirects to the pinned `ServeMux`. Package
  extensions operate outside that matcher; no copied internal source, unsafe,
  or registration-order tie-break exists.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSupportedMatchingIsDifferentialWithServeMux`,
  `TestSupportedMethodsAndLiteralHostsAreDifferentialWithServeMux`, and
  `FuzzRoutePatternCompilation` cover `Builder.Register`, `Compile`, and
  `Router.ServeHTTP`. Reconsider when the minimum Go routing contract changes.

## ROUTER-DEC-002: Registration panics, conflicts, and publication

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  startup policy.
- **Source and issue:** Go 1.26.6
  [`ServeMux.Handle`](https://cs.opensource.google/go/go/+/refs/tags/go1.26.6:src/net/http/server.go)
  panics for malformed or conflicting patterns. A reusable startup API must
  decide which panics become errors and whether failed compilation mutates
  publication state.
- **Interpretations and peer behavior:** Preserve every panic, recover every
  panic, preimplement conflict logic, or convert only controlled synchronous
  registration failures. Frameworks vary between panic and error APIs.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Validate and sort the complete table
  before constructing middleware; convert only non-runtime panics produced by
  package-owned `ServeMux.Handle` calls into bounded typed errors; propagate
  unrelated panics. Failed compile publishes nothing and leaves the builder
  repairable; successful compile freezes it.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestCompileReturnsTypedConflictsAndFreezesOnlyOnSuccess`,
  `TestConflictFailureDoesNotConstructAnyMiddleware`,
  `TestRegistrationOrderDoesNotChangeDispatchOrIntrospection`, and
  `TestPatternValidationPropagatesUncontrolledPanics` cover `Compile` and
  `Error`. Reconsider if Go replaces registration panics with a typed API.

## ROUTER-DEC-003: Method, HEAD, OPTIONS, Allow, and miss outcomes

- **Status, owner, and classification:** `resolved`; maintainers; RFC 9110
  semantics with explicit `ServeMux` divergences.
- **Source and issue:** RFC 9110 [methods](https://www.rfc-editor.org/rfc/rfc9110.html#section-9),
  [HEAD](https://www.rfc-editor.org/rfc/rfc9110.html#section-9.3.2),
  [OPTIONS](https://www.rfc-editor.org/rfc/rfc9110.html#section-9.3.7), and
  [`Allow`](https://www.rfc-editor.org/rfc/rfc9110.html#section-10.2.1) do not
  dictate a router's automatic OPTIONS or unsupported-method policy.
- **Interpretations and peer behavior:** Mirror `ServeMux`, synthesize OPTIONS,
  treat every miss as 404, or distinguish known path 405 from unsupported 501.
  Routers disagree on implied HEAD and OPTIONS in `Allow`.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Require explicit uppercase method
  tokens. GET implies HEAD unless explicit HEAD wins. Explicit OPTIONS wins;
  otherwise enabled automation emits 204 and a sorted, deduplicated `Allow`
  including implied HEAD and OPTIONS. A known path with another method is 405;
  a valid method absent from the compiled table is 501 only when no known path
  establishes 405. Disabling automation restores default ServeMux 404/405.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestCompiledRouterPreservesHTTPMethodSemantics`,
  `TestExplicitOptionsAndHeadRoutesWin`,
  `TestDefaultNotFoundAndMethodNotAllowedMatchServeMux`, and
  `TestDocumentedServeMuxDispatchDivergences` cover router options and dispatch.
  Reconsider only through a versioned compatibility change.

## ROUTER-DEC-004: Request-target forms and malformed input

- **Status, owner, and classification:** `resolved`; maintainers; RFC 9110/9112
  transport-boundary policy.
- **Source and issue:** RFC 9112 [request-target](https://www.rfc-editor.org/rfc/rfc9112.html#section-3.2)
  permits origin, absolute, authority, and asterisk forms. Go supplies parsed
  requests, but routing support for CONNECT authority form and `OPTIONS *`
  remains a package choice.
- **Interpretations and peer behavior:** Accept every parsed target, pass all
  forms to ServeMux, reject non-origin forms, or support only the server-wide
  asterisk case. Routers differ on malformed authority and CONNECT.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Accept valid origin and absolute
  forms supplied by `net/http`; support only `OPTIONS *`, returning automatic
  204 or the explicit not-found policy when automation is disabled; reject
  other asterisk use and CONNECT authority-form dispatch with 400. CONNECT
  registration fails at startup. Invalid methods, URLs, and authorities bypass
  route middleware and custom miss handlers.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestAsteriskOptionsAndMalformedAuthority`,
  `TestMalformedRequestsBypassCustomHandlersAndRouteMiddleware`,
  `TestUnsupportedConnectRouteFailsAtStartup`, and `FuzzRequestTargets` cover
  dispatch and registration. Reconsider when a concrete CONNECT service needs
  a separately threat-modeled contract.

## ROUTER-DEC-005: Canonical redirects and escaped path structure

- **Status, owner, and classification:** `resolved`; maintainers; Go-compatible
  redirect behavior with explicit defensive override.
- **Source and issue:** Go 1.26.6 ServeMux canonicalizes paths and subtree roots;
  RFC 3986 [path syntax](https://www.rfc-editor.org/rfc/rfc3986.html#section-3.3)
  distinguishes separators from percent-encoded data. Rejecting redirects by
  decoded path can misclassify encoded slash or dot text.
- **Interpretations and peer behavior:** Always follow ServeMux redirects,
  disable all canonicalization, clean decoded paths, or classify structural
  changes using escaped paths. Routers disagree on `%2F` and trailing slash.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Follow ServeMux canonical and subtree
  redirects by default before route/method miss selection. `RejectRedirects`
  converts structural redirects to 404 using escaped-path semantics and
  standard patterns. Encoded separators and dot text inside a wildcard remain
  data; literal and percent-encoded dot segments in registered patterns are
  rejected.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestCanonicalRedirectsPrecedeRouteAndMethodSelection`,
  `TestRejectRedirectPolicyTreatsEncodedSeparatorsAsWildcardData`, and
  `TestRejectRedirectPolicyRejectsSemanticSubtreeRoots` cover `RedirectPolicy`.
  Reconsider when Go canonicalization behavior changes.

## ROUTER-DEC-006: Host patterns, ports, wildcard labels, and IDNA

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  authority policy and documented ServeMux extension.
- **Source and issue:** RFC 9110 [authority](https://www.rfc-editor.org/rfc/rfc9110.html#section-7.2)
  and RFC 3986 [host](https://www.rfc-editor.org/rfc/rfc3986.html#section-3.2.2)
  allow forms unsuitable for implicit tenant routing. ServeMux supports literal
  hosts but not this package's single-label wildcard precedence.
- **Interpretations and peer behavior:** Match raw Host, normalize Unicode,
  include ports, allow arbitrary wildcard suffixes, or require validated ASCII
  DNS labels. Host-routing libraries vary on proxy and IDNA trust.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Route patterns accept bounded ASCII
  DNS labels and one-label `{name}` wildcards; ports and IP literals are not
  patterns. Request ports are removed for matching; exact hosts precede wildcard
  hosts, then hostless fallback. Malformed, non-ASCII, user-info, and invalid
  port authorities fail 400. No forwarding header or implicit IDNA conversion
  is used; callers normalize trusted IDNA at their boundary.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestHostPatternsMatchPortsAndSingleLabels`,
  `TestHostSpecificityFallbackAndEquivalentPatterns`,
  `TestAmbiguousHostPatternsAndUnsafeAuthoritiesAreRejected`, and
  `FuzzHostMatching` cover host registration, dispatch, and generation.
  Reconsider for a versioned IDNA policy or multi-label wildcard design.

## ROUTER-DEC-007: Group composition and transactional callbacks

- **Status, owner, and classification:** `resolved`; maintainers; application
  composition policy.
- **Source and issue:** Go's
  [`net/http.Handler`](https://cs.opensource.google/go/go/+/refs/tags/go1.26.6:src/net/http/server.go)
  and HTTP do not define route groups, prefix joining, metadata merge, naming,
  or callback failure semantics.
- **Interpretations and peer behavior:** Mutate a shared group, clean paths,
  silently override metadata, retain partial callback routes, or flatten one
  validated transaction. Framework groups commonly hide mutation and merge
  precedence.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Flatten nested host, path, name,
  metadata, and middleware state at registration. Join prefixes without
  `path.Clean`; reject empty/dot/wildcard-invalid segments; require equal
  repeated hosts; reject metadata collisions; and publish no child route when
  callback or validation fails. Group and nesting budgets apply even to empty
  groups.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestNestedGroupsFlattenComposition`,
  `TestGroupCompositionRejectsInvalidAndPartialState`,
  `TestNestedGroupPrefixesUseComposedBudgets`, and `FuzzGroupComposition` cover
  `Builder.Group` and `GroupOptions`. No upstream issue exists; reconsider only
  for a new explicit merge policy.

## ROUTER-DEC-008: Middleware order, exclusions, and runtime ownership

- **Status, owner, and classification:** `resolved`; maintainers; explicit
  composition and lifecycle policy.
- **Source and issue:** Go's
  [`http.Handler`](https://cs.opensource.google/go/go/+/refs/tags/go1.26.6:src/net/http/server.go)
  permits decorators but defines no router, group, mount, route order,
  exclusion, duplicate-name, panic, or recovery behavior.
- **Interpretations and peer behavior:** Resolve aliases globally, instantiate
  by reflection, apply route-first, silently deduplicate, or freeze explicit
  values at compile. Frameworks differ and often own recovery implicitly.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Execute router, outer group, inner
  group, then route middleware; unwind in reverse. Named exclusions remove only
  inherited layers and are explicit on the route. Nil, duplicate resolved names,
  and nil constructed handlers fail before publication. Serving panics,
  cancellation, short circuits, re-entry, writer capabilities, and handler
  lifecycle remain caller-owned; no recovery or wrapper is injected.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestMiddlewareOrderAndIntrospectionAreStableAndImmutable`,
  `TestRouteMayExcludeNamedGroupMiddleware`,
  `TestMiddlewareMayShortCircuitPanicCancelAndReenter`, and
  `TestRouterPreservesResponseWriterOptionalInterfaces` cover middleware APIs.
  Reconsider only through explicit versioned ordering policy.

## ROUTER-DEC-009: Mount stripping and request identity

- **Status, owner, and classification:** `resolved`; maintainers; Go request
  interoperability and defensive path policy.
- **Source and issue:** Go's
  [`StripPrefix`](https://cs.opensource.google/go/go/+/refs/tags/go1.26.6:src/net/http/server.go)
  and HTTP do not define this package's mounted-handler prefix stripping,
  `RawPath`, `RequestURI`, inherited path values, or mutation ownership.
- **Interpretations and peer behavior:** Mutate the original request, use
  `http.StripPrefix`, discard escaped form, copy routers internally, or clone
  only the URL view supplied to the mounted handler.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Mount one ordinary remainder route.
  Optional stripping clones request and URL, compares encoded literal prefixes
  in decoded path space, preserves the escaped suffix in `RawPath`, and leaves
  caller URL and `RequestURI` untouched. A compiled router mounts as an ordinary
  handler. Outer path values survive unless an inner wildcard reuses the name.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestMountStripsPathOnCloneAndPreservesRequestTarget`,
  `TestMountStripsEscapedLiteralPrefixWithoutLosingRawPath`, and
  `TestCompiledRouterMountIsAnOrdinaryHandler` cover `Mount` and `MountOptions`.
  Reconsider if Go publishes richer mount semantics.

## ROUTER-DEC-010: Path values, matched metadata, and introspection

- **Status, owner, and classification:** `resolved`; maintainers; Go-compatible
  parameter behavior plus bounded application metadata policy.
- **Source and issue:** Go 1.26.6
  [`Request.PathValue`](https://cs.opensource.google/go/go/+/refs/tags/go1.26.6:src/net/http/request.go)
  owns route parameters but does not expose a public immutable route table or
  matched-route descriptor.
  A parallel parameter API risks divergence; raw metadata risks disclosure and
  telemetry cardinality.
- **Interpretations and peer behavior:** Copy parameters into a custom context,
  expose handler pointers, return internal maps, or retain standard path values
  and add a defensive metadata snapshot.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Handlers use `Request.PathValue`
  directly. `MatchedRoute` adds a copied bounded `RouteInfo` only inside the
  selected chain; route tables are deterministically sorted and copied, omit
  handlers/function names, and retain bounded caller metadata. Misses install
  neither route metadata nor path values.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestCompiledRouterDispatchesWithPathValuesAndMatchedRoute`,
  `TestMiddlewareOrderAndIntrospectionAreStableAndImmutable`, and
  `TestConcurrentDispatchIntrospectionAndGeneration` cover `MatchedRoute` and
  `Routes`. Reconsider if Go exposes equivalent route metadata.

## ROUTER-DEC-011: Named path generation and percent encoding

- **Status, owner, and classification:** `resolved`; maintainers; RFC 3986
  component encoding with Go URL interoperability.
- **Source and issue:** RFC 3986 [percent encoding](https://www.rfc-editor.org/rfc/rfc3986.html#section-2.1)
  and [path segments](https://www.rfc-editor.org/rfc/rfc3986.html#section-3.3)
  permit many strings, but interpolating raw values can inject separators,
  traversal, or double decoding.
- **Interpretations and peer behavior:** Raw substitution, escape the entire
  path, stringify arbitrary models, accept one raw remainder, or require typed
  explicit segment inputs. Reverse routers differ on extra parameters.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Require every wildcard exactly once;
  reject missing, duplicate, unknown, unused, or wrong-kind parameters. Escape
  each segment once with `url.PathEscape`; a remainder is an explicit non-empty
  segment list and rejects empty/dot segments. Query follows bounded
  deterministic `url.Values.Encode`. Generated paths must round-trip to the
  intended ServeMux path values; no reflection or model binding occurs.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestNamedPathGenerationEscapesSegmentsAndRoundTrips`,
  `TestRemainderGenerationRequiresExplicitSafeSegments`,
  `TestGenerationRejectsParameterSetErrors`, and `FuzzNamedRouteRoundTrip`
  cover `Param`, `Remainder`, and `Router.Path`. Reconsider for new typed
  component kinds, not raw interpolation.

## ROUTER-DEC-012: Absolute URL bases and route hosts

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  origin and host trust policy.
- **Source and issue:** RFC 3986 [authority](https://www.rfc-editor.org/rfc/rfc3986.html#section-3.2)
  allows userinfo and broad host syntax. Inferring a base from request or proxy
  fields can produce open redirects or cross-tenant URLs.
- **Interpretations and peer behavior:** Infer request scheme/host, trust
  forwarding fields, accept arbitrary schemes, or require one validated
  immutable base. Framework route helpers commonly depend on ambient requests.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  `NewBaseURL` accepts only explicit
  bounded HTTP(S) scheme and validated authority without userinfo, controls, or
  malformed ports. Generation never reads a request or forwarding field. A
  literal or rendered route host replaces the base hostname while preserving
  its explicit trusted port. Unicode hosts require caller-selected ASCII IDNA.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestAbsoluteURLGenerationValidatesBaseHostAndQuery`,
  `TestGenerationEnforcesOutputAndQueryLimits`,
  `TestTrustedBaseAuthorityIsBounded`, and `FuzzURLGenerationInputs` cover
  `BaseURL`, `NewBaseURL`, and `Router.URL`. Reconsider only with an explicit
  trusted-proxy integration contract.

## ROUTER-DEC-013: Finite limits, errors, and custom miss handlers

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  resource, disclosure, and handler-ownership policy.
- **Source and issue:** RFC 9110
  [`414 URI Too Long`](https://www.rfc-editor.org/rfc/rfc9110.html#section-15.5.15)
  and Go's `ServeMux` do not bound route tables, metadata, patterns, targets,
  diagnostics, middleware, parameters, or generated output, and do not define
  custom miss-handler panic/partial-write behavior.
- **Interpretations and peer behavior:** Leave all input unbounded, truncate
  semantic values, expose route inventories in errors, recover custom handlers,
  or reject at exact finite boundaries with safe diagnostics.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Validate positive immutable limits
  and reject syntactically valid excess with `ErrLimitExceeded`; reject runtime
  target excess with 414 before matching. Errors are typed, deterministic,
  bounded, single-line valid UTF-8, and omit route inventories and values.
  Custom 404/405 handlers own cancellation, partial responses, and panics;
  malformed input bypasses them. No hidden I/O, goroutine, global registration,
  or mutable compiled state exists.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestRegisterEnforcesLimitsAndBoundsDiagnostics`,
  `TestFineGrainedInputByteBudgets`,
  `TestDiagnosticsAreSingleLineValidUTF8`,
  `TestCustomErrorHandlerOwnsPartialResponsesAndPanics`, and
  `TestEveryLimitMustBePositive` cover `Limits`, `Error`, and custom options.
  Reconsider limits only with measured compatibility and resource evidence.

## Unresolved and excluded behavior

No known material ambiguity in the current public surface is unresolved.
Dynamic routes, regex matching, CONNECT tunneling, proxy trust, implicit IDNA,
controller resolution, model binding, sessions, CSRF, templates, dependency
injection, authentication, authorization, RPC dispatch, and server lifecycle
are outside the v1 claim. Adding one requires a new decision before runtime
implementation.
