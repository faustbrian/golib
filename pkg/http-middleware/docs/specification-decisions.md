# HTTP Middleware Specification Decisions

This register records the observable choices made where HTTP, Fetch, Go, and
deployment policy leave multiple credible server behaviors. The package claims
only the narrow behavior named here; it does not claim to implement an HTTP
wire stack, browser, proxy, cache, authentication system, or complete Fetch
implementation.

Every resolved entry names executable evidence. A change to a selected
behavior requires compatibility, security, resource, conformance, API, and
changelog review. Superseded entries remain in this file and link to their
replacement.

## HTTPMIDDLEWARE-DEC-001: Explicit composition order and duplicate ownership

- **Status, owner, and classification:** `resolved`; `http-middleware`
  maintainers; application policy and defensive composition behavior.
- **Source and issue:** Go 1.26.5 [`net/http.Handler`](https://cs.opensource.google/go/go/+/refs/tags/go1.26.5:src/net/http/server.go)
  defines handler execution,
  but no standard defines middleware declaration order, duplicate concern
  ownership, or ordering metadata. Applying decorators in declaration order
  reverses request order unless the chain deliberately resolves them backward.
- **Interpretations and peer behavior:** Declare request order, declare wrapping
  order, permit duplicate names, or rely on framework registration. Go routers
  and middleware libraries use both order conventions and commonly leave
  duplicate concern ownership implicit.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Descriptors are immutable and listed
  in request execution order; responses unwind in reverse. Chain depth is 256,
  duplicate names fail unless both descriptors opt in, and named before/after
  constraints are validated at construction. There is no registry or global
  default. This makes security-sensitive order inspectable at startup.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestChainExecutesInDeclaredOrderAndUnwindsInReverse`,
  `TestExplicitDuplicateAndOrderingPoliciesAreInspectable`, and
  `TestRecommendedStackHasExactRequestAndResponseOrder` cover `Chain`,
  `Descriptor`, and `When`. There is no upstream ambiguity to resolve.
  Reconsider only if Go adopts a standard middleware composition contract.

## HTTPMIDDLEWARE-DEC-002: Request and correlation identifier trust

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  field policy, not an HTTP requirement.
- **Source and issue:** RFC 9110 [field syntax and limits](https://www.rfc-editor.org/rfc/rfc9110.html#section-5)
  permit application-defined fields but do not standardize `X-Request-ID` or
  `X-Correlation-ID`, their trust, format, uniqueness, or propagation.
- **Interpretations and peer behavior:** Always trust one inbound value, always
  generate a replacement, reject malformed trusted input, or accept arbitrary
  Unicode. Reverse proxies and frameworks disagree on names and precedence.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Inbound identifiers are untrusted by
  default. Explicitly trusted input must be one printable ASCII value within a
  configured bound; invalid trusted input is replaced or rejected by named
  policy. Generated values are validated, stored by distinct request,
  correlation, or operation kind, and copied to the configured response field.
  Identifiers are metadata and never authorization evidence.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestUntrustedInboundIdentifierIsReplacedAndPropagated`,
  `TestTrustedInvalidIdentifiersFollowNamedPolicy`,
  `TestConfigurationAndIdentifierBoundaries`, and `FuzzInboundIdentifier`
  cover `requestid.Policy`, `New`, and `FromContext`. Reconsider if an adopted
  standard defines compatible identifier syntax and trust semantics.

## HTTPMIDDLEWARE-DEC-003: Panic recovery after response commitment

- **Status, owner, and classification:** `resolved`; maintainers; Go runtime
  interoperability and defensive response policy.
- **Source and issue:** Go 1.26.5 [`net/http`](https://cs.opensource.google/go/go/+/refs/tags/go1.26.5:src/net/http/server.go)
  defines `ErrAbortHandler` and
  response commitment, while `recover` permits interception of other panics.
  It does not define a reusable middleware's error body, observer data, stack
  policy, or behavior after bytes were committed.
- **Interpretations and peer behavior:** Recover every panic, re-panic every
  panic, replace a response regardless of commitment, or emit a safe response
  only before commitment. Frameworks vary and can leak panic values or produce
  mixed responses.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Re-panic `http.ErrAbortHandler`;
  classify other panics without exposing values; clear prepared headers and
  write a minimal safe error only before commitment; never rewrite committed
  output. Optional stacks are bounded and caller-observed. Observer panics are
  contained and normal cancellation is not reclassified as a panic.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestRecoveryWritesSafeResponseAndObservesBoundedClass`,
  `TestRecoveryDoesNotRewriteCommittedResponse`,
  `TestRecoveryRepanicsAbortHandler`, and
  `TestNilAndPanickingObserversCannotCorruptRecovery` cover `recovery.Policy`
  and `New`. Reconsider if `net/http` changes panic or commitment semantics.

## HTTPMIDDLEWARE-DEC-004: Request body limit accounting and ownership

- **Status, owner, and classification:** `resolved`; maintainers; Go transport
  behavior with defensive resource policy.
- **Source and issue:** RFC 9110 [content](https://www.rfc-editor.org/rfc/rfc9110.html#section-6.4)
  describes message content, while Go 1.26.5
  [`http.MaxBytesReader`](https://cs.opensource.google/go/go/+/refs/tags/go1.26.5:src/net/http/request.go)
  bounds bytes
  visible to a handler. Neither can recover bytes consumed before middleware or
  define whether a generic limit applies before or after content decoding.
- **Interpretations and peer behavior:** Count decoded payload bytes, count
  transport content bytes, pre-reject only known lengths, drain every excess
  body, or wrap the remaining stream. Framework behavior differs for chunked,
  compressed, multipart, and already-read bodies.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Limit encoded transport content
  bytes before application decoding. Reject known oversize requests with 413
  and connection close; streaming overflow remains `*http.MaxBytesError` and
  receives a safe 413 only if uncommitted. The server retains close ownership,
  and installation after any reader can bound only unread bytes. No unbounded
  drain or payload parsing is performed.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestKnownOversizedBodyIsRejectedBeforeHandler`,
  `TestStreamingOverflowUsesStandardMaxBytesError`,
  `TestLimitCountsEncodedAndMultipartTransportBytes`,
  `TestLimitAppliesToUnreadBytesAndPreservesCancellation`, and `FuzzBodyLimit`
  cover `bodylimit.Policy` and `New`. Reconsider if Go exposes reliable
  whole-request byte accounting at this boundary.

## HTTPMIDDLEWARE-DEC-005: Deadlines versus buffered handler timeouts

- **Status, owner, and classification:** `resolved`; maintainers; Go context
  semantics plus explicit bounded timeout policy.
- **Source and issue:** Go 1.26.5 [`context`](https://cs.opensource.google/go/go/+/refs/tags/go1.26.5:src/context/context.go)
  and [`net/http`](https://cs.opensource.google/go/go/+/refs/tags/go1.26.5:src/net/http/server.go)
  distinguish context
  cancellation from interrupting handler code. Producing a timeout response
  before a handler returns requires buffering or concurrent execution, which
  changes streaming and resource ownership.
- **Interpretations and peer behavior:** Only attach a deadline, use
  `http.TimeoutHandler`, buffer without a cap, or expose both contracts.
  Frameworks often imply that timeout means handler termination.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Deadline middleware never extends a
  shorter parent and does not claim interruption. Buffered timeout has explicit
  response, worker, duration, and output bounds; withholds unsupported
  streaming/upgrade capabilities; preserves informational responses; rejects
  late writes; and propagates a panic completed before timeout. A handler that
  ignores cancellation can continue only within the configured concurrency
  bound.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestDeadlineNeverExtendsParent`,
  `TestDeadlineCancelsContextIgnoringNoCodeInterruptionPromise`,
  `TestBufferedTimeoutReturnsSafeBoundedResponse`,
  `TestTimeoutBoundsContextIgnoringHandlers`, and
  `TestRealTimeoutPreservesInformationalResponse` cover `deadline.New` and
  `deadline.NewTimeout`. Reconsider if Go gains safe handler interruption or a
  bounded standard timeout primitive with equivalent contracts.

## HTTPMIDDLEWARE-DEC-006: Forwarded fields and trusted-hop selection

- **Status, owner, and classification:** `resolved`; maintainers; RFC 7239
  parsing with defensive trust policy and a nonstandard compatibility mode.
- **Source and issue:** RFC 7239 [Forwarded](https://www.rfc-editor.org/rfc/rfc7239.html#section-4),
  [node identifiers](https://www.rfc-editor.org/rfc/rfc7239.html#section-6), and
  [security](https://www.rfc-editor.org/rfc/rfc7239.html#section-8) define field
  syntax but explicitly do not make client-supplied chains trustworthy.
  `X-Forwarded-*` has no single normative grammar.
- **Interpretations and peer behavior:** Trust the leftmost or rightmost value,
  trust any present field, accept `unknown` or obfuscated nodes, or walk inward
  from a trusted direct peer until the first untrusted address. Proxies differ
  in append/replace behavior and `X-Forwarded-*` alignment.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Direct socket information is the
  default. Only configured peer prefixes enable one selected syntax. Parse
  finite RFC 7239 or strict `X-Forwarded-*` input, walk from the direct peer to
  the first untrusted client, use nearest trusted host/scheme/prefix metadata,
  and fail the whole decision closed on malformed or ambiguous fields.
  Obfuscated and `unknown` nodes never become effective client data. The
  request itself is not mutated and redirect authorization remains caller-owned.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestUntrustedPeerCannotInfluenceEffectiveInformation`,
  `TestTrustedProxySelectsFirstUntrustedClient`,
  `TestMalformedForwardingMetadataFailsEntireDecisionClosed`,
  `TestRFCForwardedMalformedMatrix`, `TestXForwardedMetadataTruthTable`, and
  `FuzzForwardedField` cover `proxy.Policy`, `Info`, `New`, and `FromContext`.
  `X-Forwarded-*` is explicitly compatibility policy. Reconsider for a new
  standardized forwarding field or deployment profile.

## HTTPMIDDLEWARE-DEC-007: CORS origins, wildcards, and preflight ownership

- **Status, owner, and classification:** `resolved`; maintainers; scoped Fetch
  CORS behavior with explicit server policy.
- **Source and issue:** Fetch [CORS protocol](https://fetch.spec.whatwg.org/#http-cors-protocol),
  [CORS-preflight fetch](https://fetch.spec.whatwg.org/#cors-preflight-fetch),
  and the URL Standard [origins](https://url.spec.whatwg.org/#origin) define
  browser processing. A server library must still choose allowlists, dynamic
  predicates, malformed-request behavior, private-network extensions, and
  whether accepted preflights reach application `OPTIONS` routes.
- **Interpretations and peer behavior:** Reflect any origin, compare raw strings,
  permit credentialed wildcards, reject denied simple requests, or omit allow
  fields while passing the application response. Frameworks vary on default
  ports, `null`, wildcard methods/headers, and `Vary`.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Canonicalize HTTP(S) origins by URL
  origin semantics, including IDNA and default ports; accept opaque `null` only
  when explicitly listed; reject credentialed wildcard configurations; compare
  methods case-sensitively and header names case-insensitively; fail malformed
  preflights with 400 and denied preflights with 403; pass denied simple origin
  requests without CORS allow fields; and merge the required `Vary` fields.
  Accepted preflight defaults to bounded 204 short-circuit with explicit
  pass-through available. Private Network Access is opt-in extension behavior.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestCredentialedWildcardOriginIsRejected`,
  `TestSimpleOriginResponseAndVary`,
  `TestPreflightShortCircuitsWithValidatedMethodAndHeaders`,
  `TestPreflightTruthTable`, `TestCanonicalOriginLengthAndDefaultPortBounds`,
  and `FuzzOriginAndPreflight` cover `cors.Policy` and `New`. Reconsider when
  pinned Fetch/URL algorithms or Private Network Access standardization change.

## HTTPMIDDLEWARE-DEC-008: Security header policy and HSTS acknowledgement

- **Status, owner, and classification:** `resolved`; maintainers; RFC 6797
  grammar plus conservative deployment policy.
- **Source and issue:** RFC 6797 [STS field syntax](https://www.rfc-editor.org/rfc/rfc6797.html#section-6.1)
  and [server processing](https://www.rfc-editor.org/rfc/rfc6797.html#section-7),
  RFC 7034 [`X-Frame-Options`](https://www.rfc-editor.org/rfc/rfc7034.html#section-2.1),
  and the pinned W3C Referrer Policy
  [policy tokens](https://raw.githubusercontent.com/w3c/webappsec-referrer-policy/cc435b05ca4a94f7f1a139be5074b168d20014db/index.src.html)
  define their respective fields but cannot know whether every deployment host
  is HTTPS-ready.
  CSP, frame, referrer, and permissions policies are application-specific and
  cannot be safely inferred by generic API middleware.
- **Interpretations and peer behavior:** Enable common headers implicitly,
  preserve downstream fields, always replace them, infer CSP/nonces, or expose
  immutable field-specific policy. Framework security bundles differ and often
  retain obsolete headers.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  API defaults are explicit
  `nosniff`, `no-referrer`, and frame denial. Each configured field has named
  preserve or replace semantics and rejects invalid names, controls, and field
  grammar. HSTS requires `AcknowledgeHSTS`, emits canonical unquoted `max-age`,
  accepts the RFC `includeSubDomains` directive, and is never inferred from a
  request header. `preload` is accepted as a de facto extension rather than
  represented as RFC 6797 behavior. CSP and permissions policy are opt-in
  opaque application values; no templating or nonce lifecycle is owned.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestAPIDefaultsApplyBeforeDownstreamResponses`,
  `TestHSTSRequiresDeploymentAcknowledgement`,
  `TestHSTSAndFixedHeadersUseFieldSpecificGrammar`,
  `TestReplaceReassertsHeadersAtCommit`, and `FuzzConfiguredHeaderValues`
  cover `secureheader.Policy`, `APIDefaults`, and `New`. Reconsider when a
  supported field is standardized, obsoleted, or changes browser semantics.

## HTTPMIDDLEWARE-DEC-009: Content-coding negotiation and transformed metadata

- **Status, owner, and classification:** `resolved`; maintainers; RFC 9110
  content-coding behavior with bounded gzip policy.
- **Source and issue:** RFC 9110 [`Accept-Encoding`](https://www.rfc-editor.org/rfc/rfc9110.html#section-12.5.3),
  [content coding](https://www.rfc-editor.org/rfc/rfc9110.html#section-8.4), and
  [representation metadata](https://www.rfc-editor.org/rfc/rfc9110.html#section-8)
  define negotiation but not buffering thresholds, retained metadata, encoder
  pools, or server-side side-channel policy.
- **Interpretations and peer behavior:** Compress whenever gzip appears, ignore
  qvalues, return identity when forbidden, rewrite weak validators, or remove
  representation-specific metadata after transformation. Middleware differs
  on empty fields, wildcards, ranges, trailers, and streaming.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Parse the complete bounded field with
  RFC qvalues and explicit coding precedence. Return 406 before application
  execution when every available coding is forbidden. Skip no-body statuses,
  HEAD, ranges, upgrades, existing coding, `no-transform`, excluded media, and
  small responses. Gzip may stream after a finite buffer; changing coding
  removes identity-specific length, entity tag, and digest headers/trailers,
  preserves unrelated trailers, and closes encoders on return, panic, or
  cancellation. Sensitive compression remains explicit deployment policy.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestGzipNegotiationHonorsQualityAndMergesVary`,
  `TestCompressionSkipsNoBodyHeadRangeAndAlreadyEncodedResponses`,
  `TestMiddlewareRejectsNoAcceptableCodingAndCompressesPastBuffer`,
  `TestTrailerDeclarationAndRepresentationFiltering`,
  `TestStreamingCompressionClosesEncoderOnPanic`, and `FuzzAcceptEncoding`
  cover `compress.Policy` and `NewGzip`. Reconsider for additive codec modules
  or changed HTTP validator semantics.

## HTTPMIDDLEWARE-DEC-010: Request and response media negotiation

- **Status, owner, and classification:** `resolved`; maintainers; RFC 9110
  parsing with strict finite application policy.
- **Source and issue:** RFC 9110 [`Content-Type`](https://www.rfc-editor.org/rfc/rfc9110.html#section-8.3),
  [`Accept`](https://www.rfc-editor.org/rfc/rfc9110.html#section-12.5.1), 406,
  and 415 define semantics but do not select supported media types or require a
  server to reject duplicate singular fields and malformed irrelevant tails.
- **Interpretations and peer behavior:** Use the first valid value, let an early
  match hide malformed trailing values, ignore missing content type, or validate
  the complete field set before matching. Frameworks differ on parameters and
  empty bodies.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Validate all bounded field lines,
  media ranges, parameters, wildcards, and qvalues before accepting a match.
  Reject duplicate `Content-Type`; require it only for a non-empty body when a
  request policy is configured; return 415 for unsupported request media and
  406 for no acceptable response representation. The package neither decodes
  nor encodes payloads.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestUnsupportedRequestMediaTypeReturns415`,
  `TestMissingContentTypeIsAllowedOnlyForEmptyBodies`,
  `TestUnacceptableResponseTypeReturns406`,
  `TestDuplicateContentTypeIsRejected`,
  `TestAcceptBudgetsAccumulateAcrossHeaderLines`, and `FuzzAcceptMediaTypes`
  cover `content.Policy` and `New`. Reconsider for protocol-specific media
  selection, which belongs in the owning representation package.

## HTTPMIDDLEWARE-DEC-011: ResponseWriter capabilities and commitment

- **Status, owner, and classification:** `resolved`; maintainers; Go 1.26.5
  `net/http` interoperability policy.
- **Source and issue:** Go 1.26.5 [`ResponseWriter`](https://cs.opensource.google/go/go/+/refs/tags/go1.26.5:src/net/http/server.go),
  `Flusher`, `Hijacker`, `Pusher`,
  `ResponseController`, informational response, and trailer contracts are
  capability-sensitive. Wrappers can accidentally advertise an optional
  interface they cannot honor or lose one they could transparently preserve.
- **Interpretations and peer behavior:** Expose every optional interface, expose
  none, mirror the underlying writer exactly, or withhold capabilities for
  buffered transforms. Wrapper libraries differ and deprecated
  `CloseNotifier` complicates compatibility.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Transparent tracking/header wrappers
  preserve exactly the underlying standard optional-interface set. Buffered
  timeout and compression expose only capabilities they can honor. Statuses,
  informational responses, implicit commitment, bytes, trailers, flush,
  hijack, and disconnect behavior are tracked without fabricating support.
  `CloseNotifier` may pass through the helper but is not a public promise.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestTrackingWrappersPreserveExactOptionalInterfaces`,
  `TestNestedTransparentWrappersPreserveResponseControllerCapabilities`,
  `TestBufferedWrappersWithholdUnsupportedCapabilities`,
  `TestRealListenerHTTP1AndHTTP2PreserveFlushAndTrailers`, and
  `TestRealHTTP1HijackSurvivesTrackingWrappers` cover all public middleware
  wrappers. Reconsider when Go adds or changes optional writer capabilities.

## HTTPMIDDLEWARE-DEC-012: Completion observation and privacy

- **Status, owner, and classification:** `resolved`; maintainers; application
  observability and defensive privacy policy.
- **Source and issue:** RFC 9110 [message abstraction](https://www.rfc-editor.org/rfc/rfc9110.html#section-6)
  and OpenTelemetry [HTTP semantic conventions](https://opentelemetry.io/docs/specs/semconv/http/)
  do not require this package to emit an event or define its cardinality. Raw
  targets, headers, errors, panic values, identifiers, and tenant data can
  contain secrets or unbounded labels.
- **Interpretations and peer behavior:** Log the complete request, emit raw path
  and errors, create spans directly, dispatch observers asynchronously, or emit
  one bounded transport event to an injected observer. Framework request
  loggers commonly mix logging, tracing, and transport policy.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Emit one synchronous completion event
  with bounded method, protocol, route, client class, status, bytes, duration,
  and outcome. Exclude raw path, query, payload, fields, credentials, IDs,
  arbitrary errors, and panic values. Metadata extractor and observer panics
  are contained by default; no worker, exporter, logger, tracer, metric, or
  background lifecycle is created. Caller-owned observer latency remains
  visible.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestObserverReceivesOneBoundedCompletionEvent`,
  `TestObserverIncludesOnlyBoundedInjectedClientClass`,
  `TestMetadataExtractorPanicIsContained`,
  `TestObserverSlownessIsSynchronousAndCreatesNoWorker`, and
  `TestEventSchemaAndAllocationBudgets` cover `observe.Policy`, `Event`,
  `RecordRoute`, and `New`. Reconsider only through a versioned event schema or
  owning telemetry adapter requirement.

## HTTPMIDDLEWARE-DEC-013: Local admission, fairness, and retry guidance

- **Status, owner, and classification:** `resolved`; maintainers; local
  resource policy with RFC 9110 retry-field syntax.
- **Source and issue:** RFC 9110 [`Retry-After`](https://www.rfc-editor.org/rfc/rfc9110.html#section-10.2.3)
  defines field syntax but not local concurrency admission, waiter fairness,
  shutdown, or distributed quota semantics.
- **Interpretations and peer behavior:** Block indefinitely, reject immediately,
  queue without a bound, promise FIFO, or expose immediate and bounded-wait
  policies. Concurrency limiters differ in fairness and cancellation behavior.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  Bound active requests, wait duration,
  and waiter count. Support immediate rejection or finite cancellation-aware
  waiting; reject new and waiting requests on shutdown; release each permit
  exactly once; and make no FIFO or starvation-free guarantee beyond progress
  for finite waiter waves. Optional retry guidance must be valid bounded
  `Retry-After`. This is process-local backpressure, not rate limiting or a
  distributed quota.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestImmediateAdmissionRejectsAboveLimitAndReleasesPermit`,
  `TestBoundedWaitHonorsCancellationWithoutLeakingPermit`,
  `TestShutdownReleasesWaitingRequestAndQueueStaysBounded`,
  `TestOverloadStormStaysBoundedAndRecovers`, and
  `TestFiniteWaiterWaveMakesProgressWithoutOrderGuarantee` cover
  `admission.Policy`, `Controller`, and `New`. Reconsider if a stronger fairness
  contract can be provided without hidden queues or goroutines.

## HTTPMIDDLEWARE-DEC-014: Cache and maintenance response policy

- **Status, owner, and classification:** `resolved`; maintainers; RFC 9111
  directives plus transport-only application policy.
- **Source and issue:** RFC 9111 [`no-store`](https://www.rfc-editor.org/rfc/rfc9111.html#section-5.2.2.5)
  defines cache behavior, while HTTP does not define application maintenance
  readiness or whether middleware should own a health endpoint.
- **Interpretations and peer behavior:** Set policy only on successful output,
  let handlers replace it, own readiness routes, cache maintenance failures,
  or apply headers before every downstream/short-circuit commitment.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  `NoStore` applies on every downstream
  status. Maintenance admission reads an injected state and emits a finite 503
  with optional validated `Retry-After`; it does not own state persistence,
  health routes, server lifecycle, or response caching. Middleware order
  determines whether outer security/observation policy also applies.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestNoStoreAppliesToEveryDownstreamStatus`,
  `TestAdmissionStateShortCircuitsWithoutOwningHealthHandler`,
  `TestAdmissionConfigurationAndRetryAfterGrammar`, and
  `TestAdmissionReadyAndRetryResponse` cover `responsepolicy.NoStore`,
  `Admission`, and `NewAdmission`. Reconsider if service health ownership or
  HTTP cache requirements change.

## HTTPMIDDLEWARE-DEC-015: Concern ownership and sibling integration

- **Status, owner, and classification:** `resolved`; maintainers; architectural
  interoperability policy.
- **Source and issue:** Go's [package clause](https://go.dev/ref/spec#Package_clause)
  and RFC 9110 define language and HTTP boundaries, not package ownership among
  server lifecycle, routing, authentication, authorization, telemetry,
  idempotency, quotas, and generic middleware. Duplicate recovery, identifier,
  or body-limit layers can silently change observable behavior.
- **Interpretations and peer behavior:** Make this package a complete framework,
  import every sibling package, permit duplicate layers, or expose names and
  validate ownership without implementing sibling state machines.
- **Selected behavior, security and resource consequences, compatibility and wire consequences:**
  This package owns only generic HTTP
  transport middleware. `service` owns process/server lifecycle;
  `router` owns routing; concern packages own credentials, decisions, quotas,
  idempotency, tracing, and stores. Adapters contain immutable concern names and
  reject known duplicate service-owned layers without importing siblings.
  Representative sibling integrations are pinned outside the production
  module. No registry, controller, model binding, session, CSRF view, template,
  service container, or exporter is introduced.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestGoServiceOwnershipRejectsDuplicateCoreMiddleware`,
  `TestOwningPackageConcernsComposeWithoutPolicyDuplication`,
  `TestOwnershipErrorAndInvalidConcerns`,
  `TestRepresentativeJSONRPCProfile`, and
  `TestRepresentativeWebhookPreservesRawSignedBody` cover `adapter.Concern`,
  `ValidateOwnership`, and representative chains. Reconsider when an owning
  sibling publishes a changed integration contract.

## Unresolved and excluded behavior

No known material ambiguity in the current public surface is unresolved.
HTTP/3 transport behavior, browser-side CORS enforcement, complete Fetch,
forward-proxy rewriting, CSRF protection, authentication, authorization,
distributed rate limiting, response caching, trace propagation, exporter
lifecycle, and application representation encoding are outside this module's
claim. Adding any such behavior requires a new decision entry before runtime
implementation.
