# Webhook Specification Decisions

This register records the observable choices made by the generic `v1`
signature protocol and its HTTP, replay, delivery, envelope, and endpoint
security boundaries. `webhook` defines a local protocol. It does not claim
compatibility with RFC 9421, CloudEvents, an Idempotency-Key draft, or any
vendor webhook scheme unless a future isolated adapter says so explicitly.

Every resolved entry names executable evidence. A change to a selected
behavior requires compatibility, security, resource, conformance, API, and
changelog review. Superseded entries remain in this file and link to their
replacement.

## WEBHOOK-DEC-001: Protocol identity and HTTP Message Signatures

**Authoritative reference:** [RFC 9421](https://www.rfc-editor.org/rfc/rfc9421.html).

- **Status, owner, and classification:** `resolved`; `webhook` maintainers;
  local wire protocol and compatibility policy.
- **Source and issue:** RFC 9421 [HTTP Message Signatures](https://www.rfc-editor.org/rfc/rfc9421.html)
  defines signature parameters, covered components, derived components, and
  Structured Fields serialization. Existing webhook providers instead use
  mutually incompatible proprietary canonical forms. The package must not
  imply that a custom webhook MAC is RFC 9421 or vendor compatible.
- **Interpretations and peer behavior:** Implement RFC 9421 directly, clone a
  provider format, expose an unversioned helper, or define a small versioned
  local protocol. Providers disagree on headers, timestamps, body treatment,
  key selection, and canonical request targets.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  `Webhook-Signature` `v1` is a local,
  versioned HMAC protocol specified by `docs/signatures.md`. It neither parses
  nor emits RFC 9421 `Signature` or `Signature-Input` fields and makes no
  provider claim. Negotiation is not implicit: unknown versions fail closed.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestCanonicalizeProducesStableVersionedBytes`,
  `TestParseSignatureHeadersRejectsMalformedOrAmbiguousInput`, and
  `TestIndependentInteroperabilityVectors` cover `Canonicalize`, `Signature`,
  and the HTTP helpers. There is no upstream issue because this is deliberate
  protocol ownership. Reconsider only through a separately versioned RFC 9421
  or provider adapter with independent vectors.

## WEBHOOK-DEC-002: HMAC algorithms and body digest

**Authoritative reference:** [RFC 2104](https://www.rfc-editor.org/rfc/rfc2104.html).

- **Status, owner, and classification:** `resolved`; maintainers; normative
  cryptographic profile plus local algorithm policy.
- **Source and issue:** RFC 2104 defines HMAC, while RFC 4231 supplies
  HMAC-SHA-256 and HMAC-SHA-512 test cases. Neither chooses a webhook algorithm,
  body digest, downgrade policy, or algorithm-negotiation mechanism.
- **Interpretations and peer behavior:** Support one digest, permit arbitrary
  hash names, derive the body digest from the MAC algorithm, or fix a body
  digest independently. Provider schemes use several incompatible choices.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  A signer and verifier are configured
  for exactly one allow-listed `sha256` or `sha512` HMAC algorithm. The canonical
  body component is always SHA-256, including under HMAC-SHA-512. The algorithm
  name is signed, unknown algorithms are rejected, and no inbound header can
  widen the configured algorithm policy.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSignerAndVerifierSupportSHA256AndSHA512`,
  `TestVerifierRejectsMutationOfEverySignedComponent`, and the RFC-independent
  Python vectors cover `Algorithm`, `Signer`, and `Verifier`. Reconsider for a
  concrete cryptographic migration with a new protocol version and downgrade
  analysis.

## WEBHOOK-DEC-003: Canonical framing, encoding, and Unicode

**Authoritative reference:** [RFC 4648](https://www.rfc-editor.org/rfc/rfc4648.html).

- **Status, owner, and classification:** `resolved`; maintainers; local
  wire-format policy informed by RFC 4648.
- **Source and issue:** RFC 4648 Section 5 defines base64url and Section 3.2
  discusses padding, but no standard defines canonical webhook field framing,
  line endings, Unicode normalization, or empty-field representation.
- **Interpretations and peer behavior:** Concatenate raw values with separators,
  serialize JSON, use length prefixes, normalize Unicode, or encode every
  variable field. Ambiguous concatenation and platform line endings can create
  cross-language disagreement.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  `v1` uses fixed ASCII labels in a
  fixed order, LF delimiters, a final empty line, and unpadded base64url for
  every variable byte field. It signs caller string bytes exactly and performs
  no Unicode normalization. Empty values remain explicit empty encoded fields.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestCanonicalizeProducesStableVersionedBytes`, `FuzzCanonicalize`, and
  `TestIndependentInteroperabilityVectors` cover `Canonicalize` and the golden
  wire bytes. Reconsider only with a new version and cross-language vectors for
  every byte-level change.

## WEBHOOK-DEC-004: Method, path, and query canonicalization

**Authoritative reference:** [RFC 3986](https://www.rfc-editor.org/rfc/rfc3986.html).

- **Status, owner, and classification:** `resolved`; maintainers; Go URL
  interoperability and local request-target policy.
- **Source and issue:** RFC 3986 defines URI components, while Go 1.26.5
  `net/url` defines `EscapedPath`, form-style query parsing, and `Values.Encode`.
  Neither selects canonical webhook semantics for method case, percent escapes,
  plus signs, duplicate values, or an empty path.
- **Interpretations and peer behavior:** Sign the raw request target, decoded
  values, a reconstructed URL, an uppercased method, or a provider-specific
  subset. Peers differ especially around duplicate query parameters.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Sign the exact nonempty method with
  case preserved; use `EscapedPath` with `/` for an empty path; parse the raw
  query using Go form semantics; sort keys through `Values.Encode`; preserve
  duplicate value order; and emit Go's canonical escaping. Semantically
  equivalent raw query spellings can therefore share canonical bytes, while
  reordered duplicate values do not.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestVerifierBindsDuplicateQueryValueOrder`,
  `TestVerifierRejectsMutationOfEverySignedComponent`,
  `TestSignAndVerifyRequestUsesRawBodyAndRestoresIt`, and the Python vectors
  cover `Message`, `Canonicalize`, `SignRequest`, and `VerifyRequest`. Reconsider
  if Go changes these URL contracts or a raw-target protocol version is added.

## WEBHOOK-DEC-005: Host and behavior-changing header coverage

**Authoritative reference:** [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  HTTP field and canonicalization policy.
- **Source and issue:** RFC 9110 defines the target URI, media type field, and
  field combination rules but does not define a webhook canonical host or the
  application-defined `Idempotency-Key` field used here.
- **Interpretations and peer behavior:** Exclude host and application fields,
  lowercase the full authority, remove default ports, combine duplicate field
  lines, or bind exact values. Intermediaries can otherwise alter decoding or
  deduplication semantics without changing the body.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Lowercase the request host while
  preserving an explicit port. Bind exactly one UTF-8 `Content-Type` and
  `Idempotency-Key` value, each bounded to 256 bytes; absence is an explicit
  empty canonical field. Duplicate, line-breaking, oversized, or invalid UTF-8
  values fail before body capture. Trace fields remain outside the signature.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestVerifyRequestRejectsDuplicateSignedHeaderBeforeBodyRead`,
  `TestVerifyRequestRejectsMutationOfFixedSignedHeaders`, and
  `TestFixedHeadersAndEventIDsPreserveExactBoundaries` cover `RequestOptions`,
  `SignRequest`, and `VerifyRequest`. Reconsider for a new signed-field profile
  or if the package adopts a standardized idempotency field contract.

## WEBHOOK-DEC-006: Timestamp precision and tolerance

**Authoritative reference:** [RFC 3339](https://www.rfc-editor.org/rfc/rfc3339.html).

- **Status, owner, and classification:** `resolved`; maintainers; local replay
  freshness policy using Go time semantics.
- **Source and issue:** HTTP dates in RFC 9110 and Internet timestamps in RFC
  3339 do not define a webhook signature timestamp, allowed skew, precision, or
  whether tolerance endpoints are inclusive.
- **Interpretations and peer behavior:** Use milliseconds, seconds, HTTP-date,
  asymmetric age limits, or exact equality. Providers vary and often leave
  boundary behavior undocumented.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  `v1` signs a canonical nonnegative
  Unix-second integer. Signer inputs are truncated to that precision. Verifiers
  accept absolute skew less than or equal to the configured tolerance, compare
  caller-provided timestamps by Unix second, and reject negative,
  noncanonical, overflowing, or outside-window values.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestVerifierTimestampToleranceBoundaries`,
  `TestVerifierComparesCallerTimestampAtProtocolSecondPrecision`,
  `FuzzTimestampVerification`, and header parser tests cover `Signature`,
  `Signer`, and `Verifier`. Reconsider only with a new protocol version or a
  demonstrated need for asymmetric age policy.

## WEBHOOK-DEC-007: Nonces, key rotation, and revocation

**Authoritative reference:** [RFC 2104](https://www.rfc-editor.org/rfc/rfc2104.html).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  cryptographic lifecycle policy.
- **Source and issue:** HMAC standards do not require a nonce, key identifier,
  rotation overlap, validity interval, ordering, or revocation behavior for
  webhook messages.
- **Interpretations and peer behavior:** Emit deterministic MACs, sign with one
  current key, emit every active key, select keys at wall-clock or signed time,
  and accept revoked keys until expiry. Provider rotation behavior varies.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Every signature carries a nonempty
  valid UTF-8 nonce bounded to 128 bytes. The default is 18 random bytes encoded
  as base64url. One nonce is shared across all signatures in one operation.
  Signers emit every non-revoked key active at the normalized signed timestamp,
  newest first with key-ID tie-breaking; verifiers use that same timestamp.
  Duplicate IDs and inverted validity windows are configuration errors.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSignerUsesOneInjectedNonceAcrossRotationSignatures`,
  `TestRotationSignsAllActiveKeysAndAcceptsOverlap`,
  `TestSignerSelectsRotationKeyAtSignedTimestamp`, and
  `TestSignerOrdersKeysWithEqualActivationByID` cover `SigningKey`,
  `VerificationKey`, and configuration. Reconsider for an external key service
  contract or a separately versioned single-signature negotiation scheme.

## WEBHOOK-DEC-008: Signature header grammar and set rejection

**Authoritative reference:** [RFC 8941](https://www.rfc-editor.org/rfc/rfc8941.html).

- **Status, owner, and classification:** `resolved`; maintainers; local HTTP
  field grammar and defensive parsing policy.
- **Source and issue:** RFC 9110 permits repeated field lines and field-specific
  combination rules. RFC 9421 uses
  [RFC 8941 Structured Fields](https://www.rfc-editor.org/rfc/rfc8941.html),
  but local `v1` does not. A custom field must define order, duplicates,
  whitespace, unknown parameters, padding, and malformed sibling behavior.
- **Interpretations and peer behavior:** Accept parameters in any order, ignore
  unknowns, split comma-combined fields, salvage valid siblings, or require
  exact serialization. Lenient parsers create cross-implementation ambiguity.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Each active key produces one separate
  `Webhook-Signature` field line with fixed parameter order and exact lowercase
  names. Comma combination, padding, whitespace variation, duplicate or unknown
  parameters, duplicate key IDs, noncanonical timestamps, invalid encodings,
  and any malformed sibling reject the entire bounded set.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSignatureHeadersRoundTripMultipleRotationSignatures`,
  `TestParseSignatureHeadersRejectsMalformedOrAmbiguousInput`,
  `TestParseSignatureHeadersAppliesLimitsBeforeDecoding`, and
  `FuzzParseSignatureHeaders` cover `SetSignatureHeaders` and
  `ParseSignatureHeaders`. Reconsider only in a new version with independent
  parser differential evidence.

## WEBHOOK-DEC-009: Exact request body, ordering, and ownership

**Authoritative reference:** [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html).

- **Status, owner, and classification:** `resolved`; maintainers; Go HTTP body
  lifecycle plus defensive resource policy.
- **Source and issue:** RFC 9110 defines message content and Go `net/http`
  exposes a stream. Neither recovers bytes consumed by earlier middleware nor
  decides whether compressed content is decoded before authentication.
- **Interpretations and peer behavior:** Hash decoded JSON, hash decompressed
  content, trust `Content-Length`, buffer without a cap, or hash exact bytes.
  Middleware ordering can otherwise create unverifiable behavior.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Bound declared size before reading,
  read at most the configured limit plus one byte, hash the exact remaining
  stream bytes without decoding or normalization, close the original body, and
  restore an independent reader. Verification must be first; previously
  consumed bytes cannot be reconstructed and only the remaining bytes are
  authenticated.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestCaptureBodyPreservesExactBytesAndRestoresRequest`,
  `TestCaptureBodyPreservesEmptyCompressedTrailersAndPartialReads`,
  `TestCaptureBodyAfterPriorReadAuthenticatesOnlyRemainingBytes`, and body
  limit tests cover `CaptureBody`, `SignRequest`, and `VerifyRequest`.
  Reconsider if Go provides an earlier immutable raw-message boundary.

## WEBHOOK-DEC-010: Candidate verification and external errors

**Authoritative reference:** [RFC 2104](https://www.rfc-editor.org/rfc/rfc2104.html).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  verification and diagnostic policy.
- **Source and issue:** RFC 2104 defines MAC verification but does not define
  multi-key candidate processing, timing exposure across public key IDs, safe
  HTTP errors, or internal diagnostics.
- **Interpretations and peer behavior:** Fail on the first invalid candidate,
  expose exact causes, try configured candidates, or return a boolean. Detailed
  errors can disclose key lifecycle and parsing state.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  After strict set parsing, the
  verifier examines candidates in wire order and accepts the first active
  configured key whose MAC passes `hmac.Equal`. Invalid candidates do not alter
  later candidate state. External errors expose stable categories and the
  fixed message `webhook verification failed`; internal diagnostics remain
  separate and secret-safe. This is not a constant-runtime claim across public
  candidate metadata.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestVerifierSkipsMalformedAndInactiveSignaturesDeterministically`,
  `TestVerificationErrorMethodsAreNilSafe`,
  `TestMiddlewareReturnsOnlySafeFailureAndSkipsHandler`, and mutation tests
  cover `Verifier`, `VerificationError`, and middleware. Reconsider only if a
  stronger blinded key-selection design is required and measured.

## WEBHOOK-DEC-011: Replay identity and atomic storage

**Authoritative reference:** [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  replay policy, not an exactly-once guarantee.
- **Source and issue:** HTTP and HMAC standards do not define webhook replay
  identity, tenant scoping, atomic persistence, TTL, or rotation behavior.
- **Interpretations and peer behavior:** Deduplicate by signature, nonce,
  provider event ID, key ID, or payload hash; check then insert; fail open on
  store outage; or delegate entirely to the application.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Optional replay protection hashes a
  length-prefixed domain string, required namespace, and authenticated event ID
  with SHA-256. It deliberately excludes key ID so overlap signatures share one
  replay identity. `ReplayStore.CheckAndRecord` must atomically create only an
  absent key with expiry. Duplicate and backend error both fail closed. This
  prevents concurrent acceptance but does not claim exactly-once processing.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestVerifyAndRecordAtomicallyRejectsReplay`,
  `TestVerifyAndRecordHashesNamespacedReplayKey`,
  `TestReplayIdentitySurvivesSecretRotation`, and store adapter tests cover
  `ReplayStore`, `VerifyAndRecord`, and replay configuration. Reconsider for a
  versioned replay-key migration or a durable application transaction seam.

## WEBHOOK-DEC-012: Event ID extraction occurs after authentication

**Authoritative reference:** [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  sequencing and application extension policy.
- **Source and issue:** No standard defines where a generic event ID resides.
  Extracting attacker-controlled JSON or headers before authentication can
  consume resources, leak parser behavior, or poison replay state.
- **Interpretations and peer behavior:** Require one fixed header, decode JSON
  before verification, let every handler deduplicate, or inject an extractor
  after authentication.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  `VerifyRequest` authenticates strict
  fields and exact body first, then invokes the configured extractor, then
  atomically records replay state. `HeaderEventID` requires one bounded UTF-8
  field. Extractor errors map to a safe missing-ID category and unauthenticated
  requests cannot touch replay storage.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestVerifyRequestExtractsEventIDAfterAuthentication`,
  `TestHeaderEventIDRejectsDuplicateAndOversizedValues`, and
  `TestVerifyRequestAndReplayErrorPaths` cover `EventIDExtractor`,
  `HeaderEventID`, and `VerifyRequest`. Reconsider only for an isolated provider
  profile with an authoritative event-ID location.

## WEBHOOK-DEC-013: Envelope is CloudEvents-shaped but not CloudEvents

**Authoritative reference:** [CloudEvents 1.0.2](https://github.com/cloudevents/spec/blob/v1.0.2/cloudevents/spec.md).

- **Status, owner, and classification:** `resolved`; maintainers; local JSON
  wire policy with an explicit non-conformance boundary.
- **Source and issue:** CloudEvents 1.0.2 defines context attributes, extension
  attributes, data content, and constraints. `Envelope` uses familiar field
  names and emits `specversion: "1.0"` but also requires time, permits an
  unvalidated source string, and nests arbitrary metadata rather than exposing
  CloudEvents extension attributes.
- **Interpretations and peer behavior:** Claim structured CloudEvents JSON,
  remove familiar names, validate the complete CloudEvents model, or preserve
  the existing small local envelope while stating the boundary.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  The output is a deterministic local
  v1 JSON envelope, not a CloudEvents implementation or interoperability claim.
  It requires ID, type, source, nonzero time, and valid JSON data; emits UTC
  RFC3339Nano time and `application/json`; preserves raw data; and orders fields
  through a fixed Go struct. Metadata is a nested local object.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestEnvelopeMarshalIsDeterministicAndPreservesData`,
  `TestEnvelopeRejectsInvalidRequiredFieldsAndData`, and `FuzzEnvelope` cover
  `Envelope.MarshalJSON`. Reconsider by moving true CloudEvents support to the
  existing `cloudevents` package or by introducing a distinctly versioned
  envelope without misleading overlap.

## WEBHOOK-DEC-014: Outbound method, content type, and idempotency field

**Authoritative reference:** [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html).

- **Status, owner, and classification:** `resolved`; maintainers; application
  delivery profile on top of RFC 9110.
- **Source and issue:** HTTP permits many methods and media types and does not
  standardize receiver idempotency semantics. A generic sender still needs one
  deterministic request profile.
- **Interpretations and peer behavior:** Preserve arbitrary caller method and
  content type, infer JSON, use PUT for idempotency, or define a fixed POST
  profile. Webhook receivers overwhelmingly vary by provider.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  `Deliverer` always emits POST with
  `Content-Type: application/json`, preserves other cloned caller headers, and
  emits `Idempotency-Key` only when explicitly supplied. Both fixed field
  values are signed. The package does not claim that a receiver honors that
  application-defined idempotency field.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestDeliverRetriesNetworkFailureAndStopsAtBound`,
  `TestDeliverPreservesRequestStatusAndResponseBoundaries`, and fixed-header
  mutation tests cover `DeliveryRequest` and `Deliverer`. Reconsider with an
  explicit configurable request profile and corresponding signature version.

## WEBHOOK-DEC-015: Retryable outcomes and Retry-After

**Authoritative reference:** [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html).

- **Status, owner, and classification:** `resolved`; maintainers; RFC 9110
  interpretation plus bounded application policy.
- **Source and issue:** RFC 9110 defines `Retry-After` and status semantics but
  does not require clients to retry, define transport failure treatment, or
  select a webhook status allowlist.
- **Interpretations and peer behavior:** Retry all 4xx/5xx, only 429/503, every
  transport error, or caller-configured statuses. Clients also disagree on
  invalid, past, overflowing, and excessive `Retry-After` values.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Retry transport failures and exactly
  408, 425, 429, 500, 502, 503, and 504. Any 2xx succeeds; every other status is
  terminal. Parse nonnegative delta-seconds or a future HTTP-date, cap at
  `MaxDelay`, and otherwise use capped exponential delay. Overflow saturates
  before capping; cancellation interrupts backoff.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestDeliverRetriesRetryableStatusAndHonorsRetryAfter`,
  `TestRetryAfterSupportsHTTPDateAndCapsDelay`,
  `TestRetryPolicyCapsOverflowingRetryAfterSeconds`, and cancellation tests
  cover `RetryPolicy` and `Deliver`. Reconsider when receiver contracts require
  an explicit configurable classifier rather than changing this list silently.

## WEBHOOK-DEC-016: Retry ownership and ambiguous receipt

**Authoritative reference:** [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html).

- **Status, owner, and classification:** `resolved`; maintainers; delivery
  lifecycle and idempotency safety policy.
- **Source and issue:** HTTP cannot reveal whether a timed-out request caused a
  side effect. Layered HTTP, queue, and outbox retries can multiply attempts.
- **Interpretations and peer behavior:** Retry regardless, require an
  idempotency key, let every layer retry, or make durable infrastructure the
  sole retry owner.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  `Deliver` forces one attempt when no
  explicit idempotency key exists. `DeliverOnce` always performs one attempt
  and is mandatory for queue/outbox consumers, leaving durable retry ownership
  outside the core. This reduces duplicate risk but still makes no exactly-once
  claim about a remote endpoint.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestDeliverOnceDisablesInternalRetries`,
  `TestHandleUsesSingleDeliveryAttempt`,
  `TestPublisherPerformsSingleAttemptForRelay`, and ambiguous failure tests
  cover `Deliver`, `DeliverOnce`, and adapters. Reconsider only with a durable
  cross-boundary protocol that proves receiver deduplication.

## WEBHOOK-DEC-017: Response bounds and failure classification

**Authoritative reference:** [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  resource and result policy.
- **Source and issue:** RFC 9110 does not define how much response content a
  webhook sender retains, whether body close failures matter, or how attempts
  map to stable application classifications.
- **Interpretations and peer behavior:** Ignore response bodies, read without a
  bound, truncate silently, expose transport errors verbatim, or preserve a
  bounded result and stable category.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Require positive request and response
  bounds that leave room for a sentinel byte. Read at most response limit plus
  one, close on every path, reject missing bodies and read/close failures, and
  classify each attempt as none, retryable, terminal, or exhausted. Diagnostic
  strings remain fixed and payloads or sensitive fields are not recorded.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestDeliverBoundsResponseBody`, `TestReadResponseRejectsMissingReadAndCloseFailures`,
  `TestDeliverClosesResponseReturnedWithTransportError`, and classification
  tests cover `DeliveryAttempt`, `DeliveryResult`, and `Deliver`. Reconsider if
  streaming response consumption becomes an explicit separate API.

## WEBHOOK-DEC-018: Dead letters and operator replay are hooks

**Authoritative reference:** [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html).

- **Status, owner, and classification:** `resolved`; maintainers; application
  lifecycle boundary.
- **Source and issue:** HTTP defines neither dead-letter persistence nor an
  operator replay audit. Implementing storage or a queue in the core would
  duplicate `outbox` and `queue` ownership.
- **Interpretations and peer behavior:** Persist internally, drop terminal
  results, expose callbacks, or require one external orchestration package.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Terminal and exhausted deliveries
  invoke an optional bounded-result `DeadLetterFunc`. Operator `Replay` invokes
  its audit hook before creating a new delivery ID and never reuses an attempt
  ID. Hook failures are returned; observer panics are contained, while
  lifecycle hook ownership remains explicit. The core stores nothing.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestDeliverDoesNotRetryTerminalStatusAndDeadLetters`,
  `TestDeliveryPreservesDeadLetterHookFailure`, and
  `TestReplayAuditsBeforeStartingNewDelivery` cover `DeadLetterFunc`,
  `ReplayHook`, and `Replay`. Reconsider only if a durable adapter contract
  cannot express a required atomic transition.

## WEBHOOK-DEC-019: Endpoint URL syntax and scheme policy

**Authoritative reference:** [RFC 3986](https://www.rfc-editor.org/rfc/rfc3986.html).

- **Status, owner, and classification:** `resolved`; maintainers; RFC 3986
  parsing with defensive SSRF policy.
- **Source and issue:** RFC 3986 permits URI forms and components that are not
  safe outbound webhook endpoints. Go `net/url` accepts opaque URLs, userinfo,
  fragments, non-ASCII hosts, trailing dots, and varied port spellings.
- **Interpretations and peer behavior:** Accept anything `url.Parse` accepts,
  allow HTTP by default, normalize risky forms, or reject unless explicitly
  enabled. SSRF filters often disagree on canonical host treatment.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Require an absolute hierarchical URL
  with host; exact lowercase `https` is default and lowercase `http` is opt-in.
  Reject opaque forms, userinfo, fragments, empty hosts, non-ASCII hosts,
  trailing-dot hosts, malformed ports, and ports outside 1-65535. Do not
  silently IDNA-convert or rewrite attacker input.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSSRFPolicyRejectsUnsafeURLsAndAddresses`,
  `TestSSRFPolicyConfigurationAndAddressFailures`, and `FuzzSSRFPolicy` cover
  `SSRFPolicyConfig`, `NewSSRFPolicy`, and `Validate`. Reconsider if explicit
  IDNA policy is added with hostile normalization vectors.

## WEBHOOK-DEC-020: Special-purpose address registry policy

**Authoritative reference:** [IANA IPv4 special-purpose registry](https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry.xhtml).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  network policy based on pinned IANA registry snapshots.
- **Source and issue:** IANA IPv4 and IPv6 special-purpose registries classify
  ranges with several properties, while Go address predicates do not reject
  every documentation, benchmark, shared, reserved, or future-use range needed
  by a conservative internet-delivery policy.
- **Interpretations and peer behavior:** Allow every global-unicast address,
  block only RFC 1918, mirror registry forwarding flags, or deny a conservative
  reviewed set. Library filters commonly miss mapped IPv4 and mixed DNS answers.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Unmap IPv4-mapped IPv6; deny invalid,
  private, loopback, link-local, multicast, unspecified, and non-global-unicast
  addresses; additionally deny the explicit pinned special-purpose prefixes in
  `reservedPrefixes`. Caller deny prefixes win over caller allow prefixes.
  Explicit allow prefixes are narrow operator exceptions. Every DNS answer
  must pass and answer count is bounded.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSSRFPolicyRejectsUnsafeURLsAndAddresses`,
  `TestSSRFPolicyRejectsMixedAndOversizedDNSAnswers`, and
  `TestSSRFPolicyAllowsExplicitPrefixOnlyWhenConfigured` cover address policy.
  Reconsider whenever either pinned IANA registry digest changes; review the
  range diff before changing runtime acceptance.

## WEBHOOK-DEC-021: DNS rebinding, redirects, proxies, and transport

**Authoritative reference:** [Go 1.26.5 net/http source](https://cs.opensource.google/go/go/+/refs/tags/go1.26.5:src/net/http/).

- **Status, owner, and classification:** `resolved`; maintainers; defensive Go
  HTTP transport policy.
- **Source and issue:** Go `net/http` owns redirect, proxy, DNS, connection
  reuse, and protocol behavior. URL validation before a request alone does not
  prevent DNS rebinding, redirect pivots, or environment proxy bypass.
- **Interpretations and peer behavior:** Validate only the original URL, follow
  redirects with revalidation, trust environment proxies, pin one DNS answer,
  or own a direct dial path. Generic clients often validate too early.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Validate immediately before every
  attempt and again in the transport; re-resolve at dial time; validate every
  answer; dial only validated addresses; disable environment proxies; return
  redirects without following; and disable automatic HTTP/2 on this custom
  transport so its direct validated dial ownership remains explicit. TLS
  certificate and hostname verification remain with `net/http`.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSecureHTTPClientRevalidatesDNSAtDialTime`,
  `TestSecureHTTPClientRejectsRedirectWithoutContactingTarget`,
  `TestSecureHTTPClientConfigurationAndTransportFailures`, and transport
  configuration tests cover `NewSecureHTTPClient`. Reconsider if Go exposes a
  protocol-independent validated-address dial contract with equivalent proof.

## WEBHOOK-DEC-022: Fan-out concurrency and ordering

**Authoritative reference:** [Go memory model](https://go.dev/ref/mem).

- **Status, owner, and classification:** `resolved`; maintainers; bounded
  orchestration policy.
- **Source and issue:** Neither HTTP nor webhook conventions define fan-out
  concurrency, result ordering, cancellation, or durability. One goroutine per
  endpoint is an unbounded resource risk.
- **Interpretations and peer behavior:** Spawn freely, run serially, return
  completion order, stop on first failure, or use a fixed worker set.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  `FanOut` rejects input beyond
  `MaxFanOut`, requires a positive worker limit, uses a fixed worker bound, and
  returns one result per input in input order. Cancellation prevents new useful
  work but does not claim to interrupt a remote side effect already in flight.
  Fan-out is not a durable queue.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestFanOutBoundsConcurrencyAndPreservesResultOrder`,
  `TestFanOutRejectsUnboundedInputs`, and identifier boundary tests cover
  `FanOut`. Reconsider only with an explicit streaming-result API or durable
  orchestration adapter.

## WEBHOOK-DEC-023: Observation fields and trace propagation

**Authoritative reference:** [W3C Trace Context](https://www.w3.org/TR/2021/REC-trace-context-1-20211123/).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  privacy policy plus optional W3C Trace Context interoperability.
- **Source and issue:** W3C Trace Context defines propagation fields but not
  webhook signature coverage or application telemetry schemas. Logging raw
  payloads, signatures, URLs, IDs, or attacker strings can leak credentials and
  create high-cardinality telemetry.
- **Interpretations and peer behavior:** Sign trace fields, log full requests,
  attach raw errors, disable propagation, or expose a closed semantic schema.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Core observations contain only fixed
  operation, outcome, reason, algorithm, status class, attempt, and bounded
  duration fields. They exclude payload, signature, secret, endpoint, event ID,
  replay key, and raw error text. The optional telemetry wrapper injects trace
  context after signing, so trace rotation does not invalidate the MAC, and
  preserves the caller's secure transport policy.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestVerifierObservesVerificationAndReplayWithoutSensitiveData`,
  `TestObserverWritesOnlyFixedSecretSafeAttributesThroughGoLog`, and
  `TestInstrumentHTTPClientInjectsTraceAndPreservesClientPolicy` cover
  `Observer` and adapters. Reconsider if a new signed trace-binding profile is
  explicitly required and its propagation lifecycle is defined.

## WEBHOOK-DEC-024: Provider presets remain unsupported

**Authoritative reference:** [RFC 2104](https://www.rfc-editor.org/rfc/rfc2104.html).

- **Status, owner, and classification:** `resolved`; maintainers; conformance
  claim and extension policy.
- **Source and issue:** Vendor webhook schemes differ in secret encoding,
  timestamp grammar, canonical bytes, multiple signatures, rotation, replay,
  and provider retry behavior. Similar use of HMAC is not interoperability.
- **Interpretations and peer behavior:** Market generic HMAC as compatible,
  embed provider switches in core, copy SDK snippets, or require isolated
  authoritative profiles and vectors.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  No provider preset is supported.
  Vendor names cannot enter the supported matrix until an isolated package has
  authoritative versioned documentation, independent positive vectors,
  negative mutations, rotation and retry semantics, and a maintenance owner.
  Generic `v1` remains provider-independent.
- **Evidence, public surface, upstream, and reconsideration:**
  `docs/providers.md`, `TestIndependentInteroperabilityVectors`, and absence of
  provider production packages are the current evidence. There is no upstream
  issue. Reconsider one provider at a time when complete authoritative evidence
  and ownership exist.

## WEBHOOK-DEC-025: Delivery queue wire encoding

**Authoritative reference:** [RFC 8259](https://www.rfc-editor.org/rfc/rfc8259.html).

- **Status, owner, and classification:** `resolved`; maintainers; local JSON
  persistence boundary based on RFC 8259 and Go encoding contracts.
- **Source and issue:** RFC 8259 defines JSON but not webhook delivery fields,
  URL encoding, unknown-member policy, deterministic output, or size limits for
  queue/outbox transport.
- **Interpretations and peer behavior:** Serialize the public Go struct
  directly, use gob, preserve arbitrary headers, accept unknown fields, or
  define a private bounded versioned shape.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Marshal a dedicated `v1` JSON shape
  with endpoint string, copied body, IDs, headers, and metadata under an exact
  byte limit. Decode one JSON value with unknown-field rejection and no trailing
  data, reconstruct and validate the endpoint, copy mutable collections, and
  reject unsafe required fields. Deterministic Go JSON output is contractual
  for equivalent values.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestDeliveryRequestWireRoundTripIsDeterministic`,
  `TestDeliveryRequestWireRejectsLimitsAndMalformedData`,
  `TestDeliveryWireRejectsEachUnsafeFieldIndependently`, and
  `FuzzDeliveryWire` cover `MarshalDeliveryRequest` and
  `UnmarshalDeliveryRequest`. Reconsider with a new wire version and migration
  plan before changing persisted bytes.

## WEBHOOK-DEC-026: Compatibility and reconsideration policy

**Authoritative reference:** [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

- **Status, owner, and classification:** `resolved`; maintainers; SemVer and
  change-control policy.
- **Source and issue:** None of the external specifications determines which
  local webhook choices are public compatibility surfaces or how security
  tightening is communicated.
- **Interpretations and peer behavior:** Treat only Go identifiers as API,
  silently tighten parsers, evolve canonical bytes in place, or version every
  observable protocol and operational classification.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below.
  Canonical bytes, field grammar,
  algorithm identifiers, envelope and delivery JSON, exported error identity,
  retry classification, replay identity, endpoint defaults, and established
  provider profiles are compatibility surfaces. Wire changes require a new
  protocol version and normally major impact. Security fixes may reject
  formerly accepted unsafe input but require explicit changelog and migration
  review. Unknown or unresolved behavior cannot become a silent default.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestIndependentInteroperabilityVectors`,
  `TestDeliveryRequestWireRoundTripIsDeterministic`, API baselines, golden
  vectors, `docs/migration.md`, and conformance checks enforce the current
  surfaces. Reconsider when a superseding decision records source, migration,
  executable evidence, and release impact; preserve this entry as history.

## Unresolved decisions

None for the currently supported provider-independent webhook surfaces. New
provider profiles, algorithms, canonicalization rules, transport mappings,
errata, or peer divergences MUST be registered before observable behavior is
selected. An unresolved wire, security, resource, or lifecycle decision blocks
the affected release claim.
