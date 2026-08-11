# HTTP Message Signature Specification Decisions

This register records observable choices made while implementing RFC 8941,
RFC 9421, and RFC 9530. It complements the exhaustive
[normative conformance matrix](conformance.md) and the pinned
[source and errata inventory](../spec/errata-decisions.md); neither one replaces
the rationale, alternatives, and compatibility consequences recorded here.

Every resolved entry names executable evidence. A change to selected behavior
requires compatibility, security, resource, conformance, API, and changelog
review. Superseded entries remain in this file and link to their replacement.

## HTTP-SIG-DEC-001: RFC 8941 parsing model and dialect boundary

**Authoritative reference:** [RFC 8941 Sections 1.2 and 4.2](https://www.rfc-editor.org/rfc/rfc8941.html#section-4.2).

- **Status, owner, and classification:** `resolved`; `http-signature`
  maintainers; normative dependency and defensive interoperability policy.
- **Source and issue:** RFC 8941 says its parsing algorithms take precedence
  over ABNF and requires strict failure, while RFC 9651 later adds Date and
  Display String types. Accepting dependency defaults without a package
  boundary could silently widen RFC 9421 and RFC 9530 input.
- **Interpretations and peer behavior:** Follow ABNF alone, expose a permissive
  parser, inherit every newer Structured Fields type, or implement the pinned
  RFC 8941 algorithms exactly. Peers vary in strictness and supported dialect.
- **Selected behavior:** Every public signature and digest parser crosses one
  panic-contained RFC 8941 boundary. Invalid complete fields fail closed, the
  parsing algorithms control over illustrative ABNF, and RFC 9651-only values
  are rejected rather than treated as extensions.
- **Consequences:** The security consequence is elimination of parser
  differentials and dependency panics. The resource consequence is one bounded
  parse per complete field. The compatibility consequence is deliberate
  rejection of RFC 9651-only peers. The wire consequence is canonical RFC 8941
  serialization only.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestHTTPWGRFC8941Corpus`, `TestStructuredFieldDependencyPanicsBecomeParseErrors`,
  and `TestStrictStructuredFieldsRejectRFC9651OnlyValues` cover all public
  parse and serialization entry points. There is no unresolved upstream issue;
  reconsider when the package deliberately adopts a newer Structured Fields
  RFC as a separately reviewed compatibility change.

## HTTP-SIG-DEC-002: Hostile-input and canonical-base resource ceilings

**Authoritative reference:** [RFC 8941 Section 4.2](https://www.rfc-editor.org/rfc/rfc8941.html#section-4.2).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  application policy permitted above RFC minimum interoperability limits.
- **Source and issue:** RFC 8941 defines minimum implementation capabilities,
  not an obligation for protocol-specific parsers to accept unbounded members,
  parameters, strings, or fields. RFC 9421 can also amplify many components
  into a large signature base.
- **Interpretations and peer behavior:** Allocate until dependency failure,
  expose no limits, use immutable defaults, or require every caller to provide
  limits. General Structured Fields libraries are commonly less restrictive
  than security-bound protocol adapters.
- **Selected behavior:** Signature, digest, and negotiation parsers use explicit
  validated limits with bounded defaults. Signature-base construction defaults
  to a 1 MiB output ceiling and permits a smaller positive caller limit.
- **Consequences:** The security consequence is bounded hostile-input work. The
  resource consequence is predictable allocation and early rejection. The
  compatibility consequence is rejection of oversized otherwise grammatical
  fields. The wire consequence is none for values within documented limits.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestRawSyntaxLimitsAcceptExactLimitsAndRejectTheirNeighbors`,
  `TestStructuredFieldParsersAcceptEveryExactResourceLimit`, and
  `TestSignatureBaseRejectsOutputBeyondExplicitResourceLimit` cover
  `SyntaxLimits` and `MessageContext.MaxSignatureBaseBytes`. No upstream issue
  exists. Reconsider only from measured interoperability evidence without
  weakening mandatory process bounds.

## HTTP-SIG-DEC-003: Complete-field combination, ordering, and duplicates

**Authoritative reference:** [RFC 8941 Section 4.2](https://www.rfc-editor.org/rfc/rfc8941.html#section-4.2).

- **Status, owner, and classification:** `resolved`; maintainers; normative
  parsing and wire-order policy.
- **Source and issue:** HTTP field lines combine before Structured Fields
  parsing, dictionaries preserve member order, and duplicate keys are invalid.
  Map-backed parsing can lose order or silently overwrite labels across lines.
- **Interpretations and peer behavior:** Parse each line independently, retain
  first or last duplicate, sort labels, or combine then parse once. Peer APIs
  differ depending on whether they expose maps or ordered members.
- **Selected behavior:** All lines of one field are combined in received order
  and parsed as one value. Duplicate labels across or within lines fail, and
  valid signature, digest, and preference members retain wire order.
- **Consequences:** The security consequence is no duplicate-label confusion.
  The resource consequence is one complete bounded value. The compatibility
  consequence is rejection of peers relying on overwrite semantics. The wire
  consequence is deterministic preservation of received and configured order.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestParseSignaturesPreservesOrderAndCopiesBytes`,
  `TestParseDigestFieldsCombinesLinesAndRejectsCrossLineDuplicates`, and
  `TestParseAcceptSignaturesRejectsActualTimestampValuesAndDuplicateLabels`
  cover the field parsers. There is no upstream dispute. Reconsider if a future
  RFC assigns different combination semantics to a new field.

## HTTP-SIG-DEC-004: Covered-component identity and extension parameters

**Authoritative reference:** [RFC 9421 Section 2](https://www.rfc-editor.org/rfc/rfc9421.html#section-2).

- **Status, owner, and classification:** `resolved`; maintainers; normative
  component identity and extension-preservation policy.
- **Source and issue:** RFC 9421 requires each component identifier to occur
  once, treats parameter order as insignificant for identity, and allows
  parameterized variants. Unknown legal extension parameters must not create
  accidental duplicate or normalization behavior.
- **Interpretations and peer behavior:** Compare raw serialization, compare
  component names only, reject all extensions, or compare normalized parameter
  sets while retaining order. Existing implementations expose different data
  models.
- **Selected behavior:** Identity consists of the component name and an
  order-insensitive parameter set; an exact identity may occur only once.
  Parameterized variants may coexist, legal unknown parameters are preserved,
  and received order remains available for canonical serialization.
- **Consequences:** The security consequence is unambiguous component coverage.
  The resource consequence is bounded identity comparison. The compatibility
  consequence is support for legal extensions without duplicate acceptance.
  The wire consequence is preservation of original parameter order.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestParseSignatureInputsRejectsAmbiguityAndWrongStructuredTypes`,
  `TestVerificationProfileMatchesRequiredComponentParametersIndependentOfOrder`,
  and `TestStructuredFieldConversionAndValidationBoundaries` cover
  `ComponentIdentifier`, profile matching, and field parsing. No upstream issue
  is open. Reconsider when a registered parameter defines distinct equality
  semantics.

## HTTP-SIG-DEC-005: Request-target reconstruction and trusted proxy context

**Authoritative reference:** [RFC 9421 Section 2.2](https://www.rfc-editor.org/rfc/rfc9421.html#section-2.2).

- **Status, owner, and classification:** `resolved`; maintainers; normative
  derived-component behavior plus defensive deployment policy.
- **Source and issue:** RFC 9421 requires signer and verifier to use equivalent
  request context, but Go servers often see a rewritten target behind proxies.
  Forwarded fields are untrusted unless deployment policy authenticates them.
- **Interpretations and peer behavior:** Trust `Forwarded` automatically,
  reconstruct from `net/http` only, accept partial overrides, or require one
  coherent external target. Frameworks vary in implicit proxy trust.
- **Selected behavior:** The package derives origin, authority, path, query,
  and target URI from one coherent message context. It never trusts forwarded
  fields automatically. Profiles may require a complete trusted external
  context, which must not contradict an absolute-form request target and is
  validated before key access.
- **Consequences:** The security consequence is resistance to proxy-origin
  substitution. The resource consequence is bounded URI parsing. The
  compatibility consequence is explicit deployment wiring behind TLS
  termination. The wire consequence is one stable derived-component base.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestCreateSignatureBaseUsesExplicitExternalRequestTargetThroughout`,
  `TestCreateSignatureBaseRejectsContradictoryExternalAbsoluteTarget`, and
  `TestProfilesRequireTrustedExternalContextBeforeKeyAccess` cover
  `ExternalRequestContext`, signing, and verification profiles. No upstream
  issue changes the trust boundary. Reconsider only if `net/http` gains a
  standard authenticated external-target representation.

## HTTP-SIG-DEC-006: Query parameter decoding and duplicate values

**Authoritative reference:** [RFC 9421 Section 2.2.8](https://www.rfc-editor.org/rfc/rfc9421.html#section-2.2.8).

- **Status, owner, and classification:** `resolved`; maintainers; normative
  query-component interpretation tied to Go's HTTP form model.
- **Source and issue:** RFC 9421 defines `@query-param` through URL query
  parsing but leaves language APIs to perform decoding. Percent errors,
  plus-as-space, invalid UTF-8, empty values, and duplicates can diverge.
- **Interpretations and peer behavior:** Use raw substring matching, strict URI
  decoding, HTML form decoding, first-value selection, or all values. Peers
  differ around invalid UTF-8 and plus signs.
- **Selected behavior:** `@query-param` uses Go `net/url` HTML form parsing,
  preserves duplicate value order, permits an explicit empty value, applies
  UTF-8 replacement exactly as the standard library does, and rejects malformed
  escapes or missing requested names.
- **Consequences:** The security consequence is one documented decoder rather
  than parser differentials. The resource consequence is bounded query parsing.
  The compatibility consequence follows Go form semantics. The wire
  consequence is one signature-base line per decoded value in received order.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestQueryParameterUsesHTMLFormParsing`,
  `TestCreateSignatureBaseQueryParamUsesUTF8ReplacementFromHTMLFormParsing`, and
  `TestSignatureBaseAllowsEmptyRFCQueryParameterValue` cover `@query-param`.
  No upstream resolution selects a different Go decoder. Reconsider if RFC
  errata or Go's documented query semantics change.

## HTTP-SIG-DEC-007: Structured, binary, trailer, and related-request modes

**Authoritative reference:** [RFC 9421 Section 2.1](https://www.rfc-editor.org/rfc/rfc9421.html#section-2.1).

- **Status, owner, and classification:** `resolved`; maintainers; normative
  component-parameter behavior.
- **Source and issue:** The `sf`, `key`, `bs`, `tr`, `req`, and `name`
  parameters change source selection and canonicalization. Combining
  incompatible modes or silently falling back can sign different bytes.
- **Interpretations and peer behavior:** Ignore unsupported parameters, apply
  modes in arrival order, normalize all fields as Structured Fields, or reject
  invalid combinations. Peer support is uneven.
- **Selected behavior:** Every registered parameter is validated by component
  kind. `sf` and `key` require the declared Structured Field form, `bs` wraps
  raw field octets, `tr` selects trailers, `req` selects the related request,
  and `name` is limited to the component that defines it. Unsupported or
  incompatible combinations fail before cryptography.
- **Consequences:** The security consequence is exact source and byte binding.
  The resource consequence is bounded mode-specific parsing. The compatibility
  consequence is strict rejection instead of fallback. The wire consequence
  follows RFC 9421 canonical parameter serialization.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestComponentParameterAndResolutionBoundaries`,
  `TestCreateSignatureBaseCombinesFieldInstancesAndSeparatesTrailers`, and
  `TestResponseContentDigestCoverageExcludesRelatedAndTrailerFields` cover
  component resolution. No upstream issue is open. Reconsider when IANA
  registers a new component parameter and its semantics are implemented.

## HTTP-SIG-DEC-008: Application profiles and signature metadata policy

**Authoritative reference:** [RFC 9421 Section 1.1](https://www.rfc-editor.org/rfc/rfc9421.html#section-1.1).

- **Status, owner, and classification:** `resolved`; maintainers; required
  application-profile boundary.
- **Source and issue:** RFC 9421 deliberately leaves covered components,
  algorithms, creation and expiration requirements, nonce, tag, clock skew,
  key retrieval, and result use to an application profile. Library defaults
  would become an undocumented protocol.
- **Interpretations and peer behavior:** Supply permissive defaults, infer
  policy from inbound metadata, expose callbacks only, or require coherent
  immutable profiles. Many peers focus on syntax and leave policy implicit.
- **Selected behavior:** Signing and verification require explicit coherent
  profiles. Inbound values never widen allowed algorithms or coverage. Time
  comparisons use the configured clock, lifetime, skew, and key-validity
  window, and zero-length validity windows are invalid.
- **Consequences:** The security consequence is no attacker-selected policy.
  The resource consequence is prevalidated bounded profile work. The
  compatibility consequence is explicit configuration for every application.
  The wire consequence is deterministic required or forbidden metadata.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSigningProfileRequiresExplicitCoherentPolicy`,
  `TestVerificationProfileRejectsImplicitOrIncoherentPolicy`, and
  `TestVerifierTimeSelectionAndParameterBoundaries` cover `SigningProfile` and
  `VerificationProfile`. This is the RFC application model, not an upstream
  defect. Reconsider only through a named public profile with its own protocol
  and conformance evidence.

## HTTP-SIG-DEC-009: Multiple signatures, label matching, and selection

**Authoritative reference:** [RFC 9421 Section 4](https://www.rfc-editor.org/rfc/rfc9421.html#section-4).

- **Status, owner, and classification:** `resolved`; maintainers; normative
  field pairing and application selection policy.
- **Source and issue:** `Signature-Input` and `Signature` are separately
  transmitted dictionaries and may carry multiple labels. Selecting by map
  order, accepting unmatched members, or trying every key changes security and
  failure behavior.
- **Interpretations and peer behavior:** Require identical label sets, ignore
  extras, select the first label, or let callers select one explicit matching
  pair. Peer middleware often hides selection.
- **Selected behavior:** Parsing preserves all ordered members, verification
  requires coherent field pairing, and integration adapters require an
  explicit selected label. Unmatched or ambiguous selected members fail before
  body release or application delegation.
- **Consequences:** The security consequence is deterministic key and profile
  selection. The resource consequence avoids unbounded trial verification. The
  compatibility consequence rejects peers with mismatched dictionaries. The
  wire consequence preserves multiple valid labels in wire order.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestVerifierRejectsDifferentLabelSetsBeforeSelection`,
  `TestVerifyingRoundTripperRejectsUnmatchedSelectedLabel`, and
  `TestSigningRoundTripperAppendsExistingLabelsInWireOrder` cover parser,
  verifier, and HTTP adapter selection. No upstream issue is open. Reconsider
  only for a separately specified negotiation profile.

## HTTP-SIG-DEC-010: Algorithm registry, strict key binding, and randomness

**Authoritative reference:** [RFC 9421 Section 3.3](https://www.rfc-editor.org/rfc/rfc9421.html#section-3.3).

- **Status, owner, and classification:** `resolved`; maintainers; normative
  algorithm behavior plus defensive cryptographic policy.
- **Source and issue:** RFC 9421 and IANA define algorithm identifiers and wire
  encodings but leave implementation key validation and randomness ownership.
  Generic crypto APIs can accept wrong curves, weak HMAC material, oversized
  RSA keys, or mismatched advertised algorithms.
- **Interpretations and peer behavior:** Dispatch by key type, trust `alg`,
  support only one algorithm, or bind every identifier to exact key and hash
  requirements. Peer support and key checks vary.
- **Selected behavior:** All active IANA algorithms are explicit allow-list
  entries with exact key type, curve, size, encoding, and cancellation checks.
  HMAC material is copied and bounded. RSA-PSS and ECDSA use Go-managed
  cryptographically secure randomness; the legacy caller-reader parameters are
  ignored so weak or blocking readers cannot affect signing. Inbound `alg`
  never chooses an unconfigured implementation.
- **Consequences:** The security consequence is resistance to algorithm and key
  confusion. The resource consequence includes RSA size and signature bounds.
  The compatibility consequence rejects weak or malformed keys accepted by
  permissive peers. The wire consequence matches registered encodings.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestRegisteredAlgorithmsRoundTripWithStrictKeyTypes`,
  `TestAlgorithmsRejectWrongKeysRandomnessAndCancellation`,
  `TestRSAPSSSigningUsesGoManagedRandomness`, and
  `TestECDSASigningUsesGoManagedRandomness` cover `Algorithm`, key constructors,
  signer, and verifier. IANA registry changes are the upstream trigger.
  Reconsider after cryptographic review of a new, deprecated, or status-changed
  algorithm.

## HTTP-SIG-DEC-011: Key resolution, rotation, revocation, and safe failures

**Authoritative reference:** [RFC 9421 Section 5](https://www.rfc-editor.org/rfc/rfc9421.html#section-5).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  key-lifecycle and failure policy.
- **Source and issue:** RFC 9421 requires applications to define key retrieval
  but does not define resolver deadlines, cache freshness, rotation overlap,
  revocation, cancellation, or unknown backend outcomes.
- **Interpretations and peer behavior:** Hide network lookup in the verifier,
  accept stale cached keys, retry late success, expose backend errors, or use a
  bounded caller-owned resolver. Peer middleware varies widely.
- **Selected behavior:** Resolvers and providers are explicit context-aware
  seams with mandatory time bounds. Key validity and revocation are evaluated
  at the signed time. Late, canceled, stale, failed, and unknown results fail
  closed, and external causes are sanitized throughout the returned error
  chain.
- **Consequences:** The security consequence is fail-closed key lifecycle and
  secret-safe diagnostics. The resource consequence is bounded lookup time.
  The compatibility consequence requires adapters for external key services.
  The wire consequence is none beyond profile-required `keyid`.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestVerifierLifecycleKeyRotationAndRevocation`,
  `TestVerifierLifecycleResolverCacheRefreshRace`, and
  `TestVerifierSanitizesExternalResolverErrorsThroughoutErrorChain` cover
  provider, resolver, and typed failures. No upstream RFC behavior resolves
  operational lifecycle. Reconsider when a standardized key-resolution profile
  is adopted.

## HTTP-SIG-DEC-012: Replay identity and atomic nonce consumption

**Authoritative reference:** [RFC 9421 Section 7.2.6](https://www.rfc-editor.org/rfc/rfc9421.html#section-7.2.6).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  replay application policy.
- **Source and issue:** RFC 9421 describes nonce-based replay resistance but
  does not define storage identity, atomicity, capacity, expiration cleanup, or
  the order of cryptographic verification and consumption.
- **Interpretations and peer behavior:** Consume before verification, maintain
  an unbounded map, permit backend uncertainty, or atomically consume only
  after all other checks. Peer libraries often delegate this completely.
- **Selected behavior:** Profiles define nonce requirements. The verifier
  performs syntax, policy, base, key, and cryptographic checks before one
  atomic replay consume. Replay identity bytes and lifetime are bounded;
  duplicate, full-capacity, canceled, failed, or unknown outcomes fail closed.
  The process-local store never executes caller callbacks while holding state.
- **Consequences:** The security consequence is one successful consumer and no
  nonce burning by invalid signatures. The resource consequence is bounded
  storage and cleanup work. The compatibility consequence is a required durable
  adapter for cross-process enforcement. The wire consequence is profile-owned
  nonce metadata.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestVerifierDoesNotConsumeNonceForInvalidCryptographicSignature`,
  `TestMemoryReplayStoreAllowsExactlyOneConcurrentConsumer`, and
  `TestVerifierLifecycleReplayOutageAndUnknownResultFailClosed` cover
  `ReplayStore`, `MemoryReplayStore`, and verification ordering. No upstream
  issue specifies storage. Reconsider if a standardized replay profile defines
  a different identity or lifecycle.

## HTTP-SIG-DEC-013: Digest algorithms, deprecated names, and representation

**Authoritative reference:** [RFC 9530 Sections 2 and 3](https://www.rfc-editor.org/rfc/rfc9530.html#section-2).

- **Status, owner, and classification:** `resolved`; maintainers; normative
  digest semantics plus registry security policy.
- **Source and issue:** RFC 9530 distinguishes content from representation
  digests and registers active and deprecated algorithms. Parsing an extension
  is not equivalent to computing or trusting it, and reported erratum 8890
  disputes example Brotli bytes.
- **Interpretations and peer behavior:** Reject all unknown names, compute every
  registry entry, select the first digest, or require an explicit active set.
  Legacy implementations still emit obsolete `Digest` fields and algorithms.
- **Selected behavior:** Ordered parsing preserves unknown legal entries and
  parameters. Computation and required verification support only active
  SHA-256 and SHA-512 and require every selected algorithm. `Content-Digest`
  covers actual coded message content; `Repr-Digest` uses caller-supplied
  representation bytes. Published and proposed erratum bytes remain distinct.
- **Consequences:** The security consequence is no downgrade to deprecated
  hashes. The resource consequence is one bounded hash pass per selected
  algorithm. The compatibility consequence retains negotiation visibility
  without trusting obsolete values. The wire consequence preserves ordered
  legal dictionaries and uses RFC 9530 field names.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestComputeDigestsRejectsUnsupportedAndDuplicateAlgorithms`,
  `TestDigestFieldVerifyRequiresEverySelectedAlgorithm`, and
  `TestRFC9530ReportedBrotliErratumKeepsPublishedAndProposedBytesDistinct`
  cover digest APIs and erratum policy. Upstream record is RFC Editor erratum
  8890. Reconsider when its status changes or IANA changes an algorithm status.

## HTTP-SIG-DEC-014: Buffered body ownership and digest release

**Authoritative reference:** [RFC 9530 Section 3](https://www.rfc-editor.org/rfc/rfc9530.html#section-3).

- **Status, owner, and classification:** `resolved`; maintainers; transport and
  resource policy required for digest verification.
- **Source and issue:** A digest authenticates bytes only after they are read,
  while `net/http` bodies are stateful caller-owned streams. Silent pre-reading,
  partial delegation, close failures, transparent decompression, and retries
  can authenticate bytes different from those consumed by the application.
- **Interpretations and peer behavior:** Trust the signed field without reading
  the body, consume in place, buffer without a limit, or provide explicit
  bounded adapters. HTTP clients commonly decompress transparently.
- **Selected behavior:** Buffered adapters require a positive maximum, consume
  and close the owned source exactly once, fail before delegation on read,
  close, size, or digest errors, and expose a replayable replacement only after
  verification. Response verification rejects transparent decompression when
  content-digest bytes would no longer match the wire content.
- **Consequences:** The security consequence is authenticated released bytes.
  The resource consequence is explicit bounded buffering. The compatibility
  consequence requires callers to choose coded versus uncoded adapter order.
  The wire consequence is a digest over the exact adapter-boundary bytes.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestBufferedContentDigestVerificationMiddlewareVerifiesBeforeNext`,
  `TestBufferedBodyReadFailsClosedWhenOwnershipCannotBeReleased`, and
  `TestVerifyingRoundTripperRejectsTransparentDecompressionForContentDigest`
  cover buffered middleware and round trippers. No upstream issue defines Go
  body ownership. Reconsider only with a streaming API that preserves the same
  authenticated-release invariant.

## HTTP-SIG-DEC-015: Trailer finality, streaming, and retry ownership

**Authoritative reference:** [RFC 9421 Section 2.1.3](https://www.rfc-editor.org/rfc/rfc9421.html#section-2.1.3).

- **Status, owner, and classification:** `resolved`; maintainers; normative
  trailer semantics plus transport lifecycle policy.
- **Source and issue:** Trailer values are unavailable until EOF. A verifier
  that delegates early cannot know whether digest and signature trailers exist,
  while a signing transport cannot safely replay a consumed one-attempt stream.
- **Interpretations and peer behavior:** Trust declared trailer names, delegate
  before EOF, buffer secretly, or make streaming and replay constraints
  explicit. Intermediaries may drop trailers.
- **Selected behavior:** Trailer verification waits for EOF and authenticates
  required digest and signature trailers before releasing replayable content.
  Streaming signers declare trailers before transfer, finalize them at EOF,
  preserve values only for application trailer names declared before transfer,
  reject early responses, zero-progress readers, name changes, and incompatible
  protocol switching, clear replayability, and require retry policy to construct
  a fresh request.
- **Consequences:** The security consequence is trailer presence and content
  authentication before trust. The resource consequence is explicit buffering
  only on verification paths that promise pre-release trust. The compatibility
  consequence requires end-to-end trailer support. The wire consequence uses
  declared HTTP trailers and one-attempt transfer semantics.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestBufferedTrailerVerificationMiddlewareWaitsForEOFAndVerifiesDigestAndSignature`,
  `TestTrailerSigningRoundTripperStreamsDigestAndSignatureAtEOF`, and
  `TestTrailerSigningRoundTripperRejectsEachStreamingFailure` cover trailer
  adapters. No upstream issue removes trailer finality. Reconsider if a future
  transport API exposes authenticated trailers before body release.

## HTTP-SIG-DEC-016: Response signatures and immutable related requests

**Authoritative reference:** [RFC 9421 Section 2.4](https://www.rfc-editor.org/rfc/rfc9421.html#section-2.4).

- **Status, owner, and classification:** `resolved`; maintainers; normative
  request-response binding and transport snapshot policy.
- **Source and issue:** Response signatures may cover related-request
  components, but redirects and transport mutation mean the caller's original
  request may differ from the request actually sent. Binding to mutable caller
  state authenticates the wrong exchange.
- **Interpretations and peer behavior:** Bind the original request, reconstruct
  from the response, omit `req`, or capture the actual sent request. Go
  transports do not expose one universal immutable snapshot automatically.
- **Selected behavior:** Response verification that covers `req` components
  requires an immutable actual-sent request snapshot captured at the transport
  boundary. Missing snapshots fail closed. Signing and compatibility callbacks
  receive isolated metadata and cannot mutate the RFC identity used later.
- **Consequences:** The security consequence is binding to the actual exchange.
  The resource consequence is one bounded metadata snapshot. The compatibility
  consequence requires cooperating transport wrappers. The wire consequence is
  the RFC 9421 related-request component base.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestVerifyingRoundTripperBindsReqComponentsToImmutableActualSentRequest`,
  `TestVerifyingRoundTripperRejectsMissingActualSentRequestSnapshot`, and
  `TestBufferedTrailerVerifyingRoundTripperBindsReqToActualSentSnapshot` cover
  response verification adapters. No upstream issue changes the requirement.
  Reconsider if `net/http` exposes a stable standard actual-sent snapshot.

## HTTP-SIG-DEC-017: Cryptographic validity is not authorization

**Authoritative reference:** [RFC 9421 Section 5](https://www.rfc-editor.org/rfc/rfc9421.html#section-5).

- **Status, owner, and classification:** `resolved`; maintainers; application
  security boundary.
- **Source and issue:** RFC 9421 verification establishes a signature result
  under an application profile, not identity authorization or permission. A
  convenience middleware could accidentally turn valid cryptography into an
  access-control decision.
- **Interpretations and peer behavior:** Return a Boolean and authorize, attach
  verification metadata, call application policy, or combine authentication
  and authorization. Framework middleware often conflates these layers.
- **Selected behavior:** Verifiers return typed cryptographic and profile
  results. HTTP middleware stores bounded verification metadata and delegates
  only after verification, but never grants roles, capabilities, tenant access,
  or business authorization. Applications perform those decisions separately.
- **Consequences:** The security consequence is explicit separation of proof
  from permission. The resource consequence is minimal immutable metadata. The
  compatibility consequence requires an application authorization step. The
  wire consequence is none.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestRequestVerificationMiddlewareVerifiesWithoutReadingBodyAndMapsFailures`,
  `TestVerifierReturnsSafeTypedFailuresForPolicyTimeAndKeyErrors`, and
  `TestVerificationCallbackCannotContaminateRFCRequestIdentity` cover middleware
  and result APIs. This is an RFC application responsibility, not an upstream
  issue. Reconsider only in a separate named authentication or authorization
  integration package.

## HTTP-SIG-DEC-018: Legacy and vendor protocol isolation

**Authoritative reference:** [RFC 9421 Appendix D](https://www.rfc-editor.org/rfc/rfc9421.html#appendix-D).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  compatibility policy.
- **Source and issue:** Cavage drafts, AWS Signature V4, OAuth 1.0, and vendor
  schemes use incompatible fields, canonicalization, credentials, and trust
  models. Accepting them through RFC 9421 entry points would create downgrade
  and parser-confusion paths.
- **Interpretations and peer behavior:** Auto-detect formats, accept legacy
  aliases, embed every scheme, or isolate caller-supplied adapters. Compatibility
  libraries commonly dispatch multiple protocols behind one verifier.
- **Selected behavior:** The RFC 9421 parser and verifier accept only RFC 9421.
  The `compatibility` package exposes explicitly named transport and middleware
  seams whose caller supplies the protocol implementation. Outbound signing
  callbacks operate on body- and indirect-graph-isolated clones and can emit
  only non-RFC-signature vendor fields; inbound verification callbacks operate
  on mutation-isolated clones and communicate only through a returned error.
  Both return sanitized failures.
- **Consequences:** The security consequence is no silent downgrade or protocol
  confusion. The resource consequence remains adapter-owned and bounded by its
  contract. The compatibility consequence is explicit integration work per
  scheme. The wire consequence is complete separation from RFC 9421 fields.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestExplicitLegacySigningAdaptersRemainProtocolSeparated`,
  `TestExplicitLegacyVerificationMiddlewareRejectsBeforeApplication`, and
  `TestCompatibilityAdaptersSanitizeCallbackFailures` cover protocol selection
  and safe failures. `TestSigningCallbackCannotContaminateRFCRequestIdentity`,
  `TestVerificationCallbackCannotNormalizeArbitraryCoveredFields`, and
  `TestCompatibilityCallbacksCannotReachCallerBodyThroughGetBody`, and
  `TestCompatibilityCallbacksCannotReachSharedRequestGraphs` cover the
  composition boundary. There is no upstream issue because the protocols are
  intentionally distinct. Reconsider only as a separately specified adapter
  with independent vectors and security review.

## HTTP-SIG-DEC-019: Errata and registry changes do not silently change behavior

**Authoritative reference:** [RFC 9421 errata](https://www.rfc-editor.org/errata/rfc9421).

- **Status, owner, and classification:** `resolved`; maintainers; errata,
  registry, and source-governance policy.
- **Source and issue:** Verified errata 8102 and 8103 correct
  `@signature-params` wording, RFC 9530 errata include editorial and reported
  technical records, and IANA registries can change independently. Fetching
  latest data at build or runtime would silently alter behavior.
- **Interpretations and peer behavior:** Follow published RFC bytes only,
  automatically apply every erratum, track latest registries dynamically, or
  pin all sources and review changes. Peer release processes vary.
- **Selected behavior:** RFC texts, errata searches, registries, official
  corpora, and cryptographic vectors are digest-pinned. Verified errata are
  applied explicitly; reported errata remain non-normative. Any source or
  registry change fails the source gate and requires conformance,
  interoperability, compatibility, and security review before behavior changes.
- **Consequences:** The security consequence is auditable algorithm and errata
  policy. The resource consequence is build-time source verification only. The
  compatibility consequence is deliberate upgrades rather than ambient drift.
  The wire consequence changes only after an explicit reviewed decision.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestRFC9421AppendixB2Examples`,
  `TestRFC9530ReportedBrotliErratumKeepsPublishedAndProposedBytesDistinct`, and
  `TestHTTPWGRFC8941Corpus` provide executable evidence; source verification is
  owned by `scripts/check-spec-sources.sh`. Upstream records are RFC Editor
  errata 8102, 8103, 8158, 8273, and 8890 plus the IANA registries. Reconsider
  every time a pinned digest, erratum status, or registry entry changes.

## HTTP-SIG-DEC-020: Net/http wire identity is explicit and fail closed

**Authoritative references:** [RFC 9421 Section 2.1](https://www.rfc-editor.org/rfc/rfc9421.html#section-2.1), [RFC 9110 Section 15.3.6](https://www.rfc-editor.org/rfc/rfc9110.html#section-15.3.6), and [Go net/http](https://pkg.go.dev/net/http).

- **Status, owner, and classification:** `resolved`; maintainers; HTTP
  transformation and ambiguous-field safety policy.
- **Source and issue:** Go promotes or synthesizes `Host`, `Content-Length`,
  `Transfer-Encoding`, `Trailer`, `Connection`, and `User-Agent` outside the
  ordinary `Header` map. HTTP/2 also splits and rejoins `Cookie`. Signing stale
  map entries or ambiguous `Set-Cookie` instances can authenticate bytes that
  differ from the wire or collapse distinct messages to one base.
- **Interpretations and peer behavior:** Sign the map verbatim, reproduce each
  transport's implicit defaults, accept ambiguous instances, or derive only
  deterministic `net/http` wire values and reject unavailable identity. Go's
  HTTP/1 writer and HTTP/2 encoder transform these fields differently.
- **Selected behavior:** Request transport-owned values and response write
  values come from the `net/http` struct fields; stale outbound map aliases are
  ignored. `MessageContext.ResponseTransport` explicitly selects received
  identity or `Response.Write` identity. Its zero value accepts a managed field
  only when both models produce the same value and otherwise fails closed.
  Received `Content-Length` and HTTP/1.0 `Connection` identity comes from
  preserved header values, including the distinction between explicit zero
  and absence; received transfer coding accepts only the states `net/http`
  parsing can produce. Outbound trailer declarations are sorted exactly as
  the standard writer, while inbound declaration order is rejected because
  `net/http` discards it. A bodyless outbound request
  derives `content-length: 0` only where the standard writer emits it:
  POST, PUT, PATCH, or a non-GET/HEAD method with explicit identity transfer
  coding. Caller-populated `TransferEncoding` follows the writer's exact
  spelling and order: only a first lowercase `chunked` value is emitted, a
  sole lowercase `identity` value can request zero content length, and a nil
  body clears transfer coding and trailer declarations. Outbound responses
  likewise follow `Response.Write`: only a first lowercase `chunked` value is
  emitted, a nil body clears non-HEAD transfer coding and trailers, HEAD
  preserves explicit response metadata, and status and method rules determine
  whether zero content length is emitted. A zero-length response backed by a
  non-sentinel body is rejected because the writer determines its framing by
  probing body bytes that canonicalization cannot consume safely. Go 1.26.5
  `Response.Write` still classifies 205 as body-bearing despite RFC
  9110 Section 15.3.6 forbidding server-generated 205 content. The low-level
  `ResponseTransportWrite` model follows those actual Go wire bytes to prevent
  a signature/wire differential. The buffered server middleware applies the
  RFC rule before that transport model, rejects handler body writes, and signs
  empty 205 content; the trailer server middleware rejects 205 because its
  mandatory trailers cannot be emitted. Callers of the low-level message API
  remain responsible for not constructing an RFC-invalid 205 response. Outbound
  `Cookie` coverage requires one semicolon-space-canonical field value, and
  ordinary multi-line `Set-Cookie` coverage requires `bs`. An implicit default
  User-Agent and Unicode Host punycode are not guessed: profiles covering them
  must supply an explicit representable value or fail closed.
- **Consequences:** The security consequence is no signature/wire differential
  or known cookie-boundary collision. The resource consequence is bounded
  sorting of already bounded trailer keys. The compatibility consequence is
  explicit rejection of inbound `Trailer` declaration coverage, absent
  bodyless DELETE/OPTIONS/custom Content-Length coverage, non-emitted
  mixed-case or unsupported transfer codings, implicit User-Agent coverage,
  Unicode Host coverage, noncanonical Cookie, and multi-line ordinary
  Set-Cookie. The security consequence of Go's 205 divergence is bounded at
  the server integrations: neither signs or emits handler content as a valid
  205 response. The wire consequence matches deterministic Go HTTP/1 output
  and fails rather than guessing across unavailable transformations.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSignatureBaseUsesNetHTTPTransportManagedRequestFields`,
  `TestSignatureBaseUsesNetHTTPTransportManagedResponseFields`,
  `TestSignatureBaseContentLengthMatchesBodylessRequestWire`,
  `TestSignatureBaseMatchesRequestWriteForCallerTransferEncoding`,
  `TestSignatureBaseTrailerMatchesRequestWriteTransferSemantics`,
  `TestSignatureBaseMatchesResponseWriteFraming`,
  `TestSignatureBaseMatchesResponseWriteWithExplicitTransportMode`,
  `TestSignatureBaseRejectsResponseWriteProbeDependentContentLength`,
  `TestSignatureBaseDistinguishesReceivedZeroContentLengthFromAbsence`,
  `TestSignatureBaseRequiresResponseTransportModeForAmbiguousFields`,
  `TestSignatureBasePreservesReceivedHTTP10ConnectionFields`,
  `TestSignatureBaseRejectsImpossibleReceivedResponseTransferEncoding`,
  `TestSignatureBaseTrailerMatchesResponseWriteFraming`,
  `TestSignatureBaseRejectsHostThatNetHTTPSanitizesOffWire`, and
  `TestSignatureBaseRejectsAmbiguousCookieFieldInstances` cover the selected
  boundary. Reconsider if `net/http` exposes preserved inbound field-line
  identity or a public transport canonicalization API.

## Unresolved decisions

None. Newly observed ambiguity remains unresolved here until a reviewed entry,
executable evidence, and compatibility decision exist; it is never converted
into an undocumented default.
