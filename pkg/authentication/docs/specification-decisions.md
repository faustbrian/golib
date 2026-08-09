# Authentication specification decisions

This register records observable choices in the root `authentication` module.
JWT and OpenID Connect are independent modules and maintain separate registers
for their own standards. Normative protocol text controls over examples, peer
defaults, and historical implementation behavior. Exact positive
interoperability vectors and their digests are pinned in the
[specification manifest](../specification/manifest.tsv).

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

Statuses are `resolved`, `unresolved`, or `superseded`. Resolved decisions are
part of the compatibility contract. A changed interpretation requires protocol,
security, compatibility, executable-evidence, and changelog review.

## AUTH-DEC-001: Authentication scheme recognition

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication` maintainers |
| Source | RFC 9110 [Authentication Scheme](https://www.rfc-editor.org/rfc/rfc9110.html#section-11.1) and [Credentials](https://www.rfc-editor.org/rfc/rfc9110.html#section-11.4) |
| Classification | Normative parsing plus defensive unknown-scheme policy |
| Issue | Scheme names are case-insensitive, but one extractor can enable several schemes and can encounter an unsupported scheme. Treating an unsupported scheme as malformed would prevent another enabled source from recognizing it; accepting flexible whitespace would exceed the credentials grammar. |
| Credible interpretations | Match scheme bytes exactly; parse every scheme in one source; reject unknown schemes; or let each source ignore other schemes while enforcing its own grammar exactly. |
| Known peer behavior | HTTP middleware varies between first-match routing, permissive whitespace splitting, and rejection of every unrecognized Authorization value. Those defaults do not determine this package contract. |
| Selected behavior | Basic and Bearer scheme names compare case-insensitively. A source treats another scheme as absent so another explicitly enabled source can recognize it. A recognized scheme requires exactly one ASCII space and one non-empty credential value; tabs, extra whitespace, and embedded whitespace are invalid. A naked recognized scheme is invalid, while a naked unknown scheme is absent. |
| Security and resource consequences | Strict separators prevent parser differentials across proxies and applications. Authorization values remain byte-bounded by the selected source. |
| Compatibility and wire consequences | Scheme casing is interoperable. Requests depending on tab separators, repeated spaces, or an unknown scheme being treated as malformed are not accepted under this profile. |
| Executable evidence | `authhttp.TestBasicAuthorizationExtractionIsStrict`, `authhttp.TestBearerAuthorizationExtractionEnforcesGrammarAndBounds`, `authhttp.FuzzAuthorizationExtraction`, and `authhttp.FuzzCredentialHeaderSet` |
| Public surface | `authhttp.BasicAuthorization`, `authhttp.BearerAuthorization`, `authhttp.Extractor`, and `FailureInvalid` versus `FailureAbsent` |
| Upstream record | RFC 9110 supersedes the authentication framework previously published in RFC 7235. No package-specific erratum changes this interpretation. |
| Reconsider when | HTTP authentication grammar changes or a new package-owned scheme requires materially different dispatch semantics. |

## AUTH-DEC-002: Basic user-password octets and delimiters

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication` maintainers |
| Source | RFC 7617 [The Basic Authentication Scheme](https://www.rfc-editor.org/rfc/rfc7617.html#section-2) and [`charset` authentication parameter](https://www.rfc-editor.org/rfc/rfc7617.html#section-2.1) |
| Classification | Normative delimiter behavior and explicit character-encoding policy |
| Issue | RFC 7617 separates user-id and password at the first colon, forbids a colon in the user-id, and leaves the default encoding undefined unless a challenge advertises UTF-8. Go strings can preserve arbitrary bytes, but silently transcoding or normalizing them would change credentials. |
| Credible interpretations | Require UTF-8 always; assume ISO-8859-1; normalize Unicode; split at the last colon; or preserve decoded octets and split at the first colon without claiming a charset. |
| Known peer behavior | Basic implementations differ on default encodings and Unicode normalization. The official examples prove specific byte sequences, not a universal default charset. |
| Selected behavior | Decode canonical padded Base64 strictly, split at the first colon, require a non-empty user-id, and preserve all remaining password bytes including later colons. The extractor does not transcode or normalize. Control octets are rejected. A caller may construct a `charset="UTF-8"` challenge, but extraction does not infer that challenge state or validate UTF-8 implicitly. |
| Security and resource consequences | Decoded credentials have an inclusive byte limit and reject controls before storage or authentication. Exact bytes avoid normalization-based credential aliases. TLS remains mandatory deployment policy. |
| Compatibility and wire consequences | RFC examples and UTF-8 bytes round-trip. Invalid UTF-8 octets remain representable in a Go string for legacy interoperability; applications requiring UTF-8 must enforce it in their credential policy. |
| Executable evidence | `authhttp.TestRFC7617BasicCredentialVectors`, `authhttp.TestBasicAuthorizationExtractionIsStrict`, `authhttp.TestBasicAuthorizationPreservesOctetsAndPasswordColons`, and Basic extraction fuzzing |
| Public surface | `authhttp.BasicAuthorization`, `authentication.BasicCredential`, `basic.NewStatic`, and `authentication.NewChallenge` |
| Upstream record | RFC 7617 deliberately leaves the default encoding compatibility-sensitive and defines UTF-8 only through the challenge parameter. |
| Reconsider when | A supported application profile mandates one encoding or the package gains a stateful challenge-to-request negotiation API. |

## AUTH-DEC-003: Bearer token grammar and legacy pipe extension

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication` maintainers |
| Source | RFC 6750 [Authorization Request Header Field](https://www.rfc-editor.org/rfc/rfc6750.html#section-2.1) |
| Classification | Normative grammar with an explicit compatibility extension |
| Issue | RFC 6750 defines `b64token`, including trailing `=` padding, but some existing opaque-token contracts use a pipe delimiter that the grammar excludes. Globally relaxing the grammar would make default compliance misleading. |
| Credible interpretations | Accept any non-whitespace token; enforce `token68`; enforce `b64token`; or expose a narrow opt-in extension without changing defaults. |
| Known peer behavior | Bearer middleware frequently accepts arbitrary strings or JWT-shaped values. Neither behavior is the RFC 6750 `b64token` contract. |
| Selected behavior | Every bearer source enforces RFC 6750 `b64token`: ASCII letters, digits, `-._~+/`, followed only by optional trailing `=`. Unicode, embedded padding, whitespace, and other bytes are invalid. `WithBearerPipe` adds only `|` and is an explicit application compatibility profile applied consistently to header, query, and cookie sources. |
| Security and resource consequences | Syntax is checked before a validator receives the bounded token. The extension cannot be enabled globally or implicitly. |
| Compatibility and wire consequences | Strict mode interoperates with RFC 6750 tokens. Pipe-bearing credentials require explicit source configuration and are not described as RFC-compliant. |
| Executable evidence | `authhttp.TestRFC6750BearerHeaderVector`, `authhttp.TestBearerAuthorizationExtractionEnforcesGrammarAndBounds`, `authhttp.TestBearerAuthorizationPipeRequiresExplicitOptIn`, and `authhttp.TestPrivateTokenAndNameBoundaries` |
| Public surface | `authhttp.BearerAuthorization`, `authhttp.BearerQuery`, `authhttp.BearerCookie`, `authhttp.WithBearerPipe`, and `authhttp.WithBearerMaxBytes` |
| Upstream record | The extension is package-owned and is not an RFC 6750 erratum or extension. |
| Reconsider when | The legacy pipe contract is retired or a versioned external profile defines a different bearer grammar. |

## AUTH-DEC-004: Bearer transport locations

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication` maintainers |
| Source | RFC 6750 [Authorization header](https://www.rfc-editor.org/rfc/rfc6750.html#section-2.1), [form body](https://www.rfc-editor.org/rfc/rfc6750.html#section-2.2), and [URI query](https://www.rfc-editor.org/rfc/rfc6750.html#section-2.3) |
| Classification | Supported-transport subset and application extension policy |
| Issue | RFC 6750 defines three transmission methods, discourages URI query use, and forbids using more than one method per request. Generic middleware cannot safely read or replace arbitrary request bodies. Cookies are common application transport but are not an RFC 6750 bearer method. |
| Credible interpretations | Implement all RFC methods; accept any named location automatically; support only Authorization; or expose hazardous and non-standard locations explicitly while keeping body access out of authentication middleware. |
| Known peer behavior | Frameworks vary between header-only defaults and automatic header, query, form, or cookie fallback. Automatic fallback obscures credential precedence and retention risk. |
| Selected behavior | Authorization is the recommended default. Query extraction is explicit, caller-named, and deprecated for new designs. Cookie extraction is an explicit application extension. Form-body extraction is intentionally unsupported, and middleware never reads the request body. Multiple configured locations containing credentials are ambiguous rather than prioritized. |
| Security and resource consequences | Query credentials can be retained before this package sees them; callers must provide TLS, end-to-end log suppression, short lifetimes, and RFC 6750 cache controls. Cookie users own Secure, HttpOnly, SameSite, origin, and CSRF policy. Body independence preserves streaming and avoids hidden buffering. |
| Compatibility and wire consequences | Header use is RFC-aligned. Query use can implement `access_token` when explicitly named. Cookie use and custom query names are package profiles, not RFC 6750 compliance claims. |
| Executable evidence | `authhttp.TestBearerQueryAndCookieAreExplicitSources`, `authhttp.TestNamedBearerSourcesRejectAbsentDuplicateAndHostileValues`, `authhttp.FuzzBearerQueryExtraction`, and `authhttp.TestMiddlewareAuthenticatesWithoutReadingBodyOrWrappingWriter` |
| Public surface | `authhttp.BearerAuthorization`, `authhttp.BearerQuery`, `authhttp.BearerCookie`, `authhttp.Extractor`, and `authhttp.NewMiddleware` |
| Upstream record | RFC 6750 has no cookie transmission method. Cookie support is retained only as an explicit application-owned extension. |
| Reconsider when | The package adopts a separate form parser, removes query support, or standardizes a versioned cookie profile with CSRF semantics. |

## AUTH-DEC-005: API-key profile and source shape

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication` maintainers |
| Source | RFC 9110 [Authentication Scheme Registry](https://www.rfc-editor.org/rfc/rfc9110.html#section-16.4.1); no general API-key authentication scheme or wire format is standardized there |
| Classification | Package-defined application protocol |
| Issue | “API key” describes many incompatible one-field and two-field conventions. Pretending one convention is standard would hide identifier, rotation, routing, and secret-handling assumptions. |
| Credible interpretations | Use one Authorization scheme; accept one secret header; infer an identifier from the secret; or require caller-named identifier and secret fields under an explicit location. |
| Known peer behavior | APIs use proprietary headers, query parameters, cookies, and Authorization schemes with incompatible semantics. OpenAPI descriptions model locations but do not define authentication behavior. |
| Selected behavior | The package profile uses a non-empty key ID and a separate non-empty secret. Callers explicitly choose and name exactly one header, query, or cookie pair. Partial pairs are invalid; duplicate values are ambiguous. The ID is routing metadata and never authenticates alone. No proprietary Authorization scheme is inferred. |
| Security and resource consequences | IDs and secrets have independent byte bounds. Static authenticators compare keyed fixed-size digests across the bounded candidate set and never disclose which component mismatched. Query and cookie risks remain caller-owned as documented for bearer transport. |
| Compatibility and wire consequences | Integrations can map their existing two-field contract without global names. One-field or custom Authorization profiles require a caller-owned `Source` or validator adapter and must not be advertised as this built-in profile. |
| Executable evidence | `authhttp.TestAPIKeySourcesMustBeExplicitAndRejectDuplicates`, `authhttp.TestAPIKeySourcesRejectMalformedQueriesAndBoundedValues`, `apikey.TestStaticAuthenticatesKeyByDeterministicID`, `apikey.TestStaticRotationAtomicallyReplacesActiveKeys`, and `apikey.TestStaticRejectsDuplicateKeyConfiguration` |
| Public surface | `authhttp.APIKeyHeader`, `authhttp.APIKeyQuery`, `authhttp.APIKeyCookie`, `authentication.APIKeyCredential`, `apikey.New`, and `apikey.NewStatic` |
| Upstream record | No IANA-registered generic API-key scheme defines this package profile. |
| Reconsider when | A supported external profile standardizes one wire format or a separate package adds a named proprietary scheme. |

## AUTH-DEC-006: Credential multiplicity and proxy separation

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication` maintainers |
| Source | RFC 9110 [Credentials](https://www.rfc-editor.org/rfc/rfc9110.html#section-11.4), [Proxy-Authorization](https://www.rfc-editor.org/rfc/rfc9110.html#section-11.7.2), and RFC 6750 [Authenticated Requests](https://www.rfc-editor.org/rfc/rfc6750.html#section-2) |
| Classification | Defensive ambiguity and HTTP protection-space policy |
| Issue | Duplicate fields, repeated named values, multiple enabled sources, and origin versus proxy credentials can produce different first-value or last-value interpretations across components. |
| Credible interpretations | First source wins; last value wins; prefer Authorization over query or cookie; merge partial API keys; or reject every competing credential while separating proxy authentication. |
| Known peer behavior | Frameworks often expose only one parsed value or apply undocumented source precedence. Proxies and origin servers have distinct authentication contexts. |
| Selected behavior | Extraction succeeds only when exactly one complete credential exists across all enabled origin sources. Duplicate Authorization fields, repeated query or cookie values, repeated API-key components, partial pairs, and multiple populated sources fail closed. Source declaration order does not select a winner. `Proxy-Authorization` is ignored by origin credential sources. |
| Security and resource consequences | Rejecting ambiguity prevents request smuggling between components with different precedence. Proxy credentials cannot accidentally authenticate to the origin application. All source counts are bounded by request parsing and configured source count. |
| Compatibility and wire consequences | Clients must send one origin credential using one configured method. Deployments relying on precedence must migrate to an explicit caller-owned adapter rather than depend on ordering. |
| Executable evidence | `authhttp.TestBasicAuthorizationExtractionIsStrict`, `authhttp.TestAPIKeySourcesMustBeExplicitAndRejectDuplicates`, `authhttp.TestExtractorSeparatesOriginAndProxyCredentials`, and `authhttp.FuzzCredentialHeaderSet` |
| Public surface | `authhttp.Extractor`, every built-in `Source`, `ErrAmbiguousCredentials`, and `ErrCredentialsInvalid` |
| Upstream record | This is a stricter defensive profile where HTTP permits extensible authentication schemes but does not define application source precedence. |
| Reconsider when | A versioned protocol profile requires deterministic multi-credential composition and defines downgrade-safe semantics. |

## AUTH-DEC-007: Challenge serialization and mandatory 401 challenges

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication` maintainers |
| Source | RFC 9110 [WWW-Authenticate](https://www.rfc-editor.org/rfc/rfc9110.html#section-11.6.1), [Challenge and Response](https://www.rfc-editor.org/rfc/rfc9110.html#section-11.3), and [401 Unauthorized](https://www.rfc-editor.org/rfc/rfc9110.html#section-15.5.2) |
| Classification | Normative response requirement plus deterministic serialization profile |
| Issue | HTTP permits token or quoted auth-parameter values and multiple challenges, while map ordering is unstable. More importantly, every 401 response must contain at least one applicable challenge, but a generic authenticator might return no valid challenge. |
| Credible interpretations | Emit map order; combine challenges into one field; omit malformed challenges; emit 401 without a challenge; invent a generic scheme; or treat missing challenge metadata as server unavailability. |
| Known peer behavior | Middleware often emits one combined field, several fields, or a bare 401. A bare 401 is not compliant with RFC 9110. |
| Selected behavior | `FormatChallenge` validates bounded token names and values, quotes every value, escapes quote and backslash, and sorts parameter names bytewise. Middleware emits each applicable challenge as its own `WWW-Authenticate` field value, preferring failure-specific challenges over configured fallbacks. If a classified credential failure has no valid challenge, middleware emits 503 instead of a non-compliant 401 and does not invent a scheme. |
| Security and resource consequences | Controls, CRLF, oversized values, excessive parameters, and invalid challenges cannot reach response fields. The 503 path exposes a configuration/provider defect without downgrading to an unauthenticated request. |
| Compatibility and wire consequences | Valid 401 responses always carry at least one challenge. Existing deployments that relied on bare 401 responses must configure `WithChallenges` or return valid challenges from their authenticator. Parameter ordering is stable but not a semantic precedence signal. |
| Executable evidence | `authhttp.TestFormatChallengeSortsAndEscapesParameters`, `authhttp.FuzzChallengeFormatting`, `authhttp.TestMiddlewareFailsClosedWithChallengesAndRedaction`, `authhttp.TestMiddlewareUsesChallengeFromFailure`, and `authhttp.TestMiddlewareTreatsMissingOrInvalidFailureChallengesAsUnavailable` |
| Public surface | `authentication.NewChallenge`, `authhttp.FormatChallenge`, `authhttp.WithChallenges`, `authhttp.NewMiddleware`, and HTTP status/header behavior |
| Upstream record | RFC 9110 requires at least one challenge on 401 and does not define a universal fallback scheme. |
| Reconsider when | Middleware gains a typed extractor-to-challenge contract that can guarantee a scheme-specific fallback at construction. |

## AUTH-DEC-008: HTTP failure mapping and anonymous access

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication` maintainers |
| Source | RFC 9110 [401 Unauthorized](https://www.rfc-editor.org/rfc/rfc9110.html#section-15.5.2), [403 Forbidden](https://www.rfc-editor.org/rfc/rfc9110.html#section-15.5.4), and RFC 6750 [Error Codes](https://www.rfc-editor.org/rfc/rfc6750.html#section-3.1) |
| Classification | Transport mapping and authentication-versus-authorization boundary |
| Issue | Missing, malformed, rejected, ambiguous, and unavailable credentials have different operational meaning. Optional routes must not turn malformed credentials into anonymous access, and authentication middleware must not emit authorization decisions such as insufficient scope. |
| Credible interpretations | Map every failure to 401; expose provider errors; allow anonymous fallback after any failure; use 403 for rejected credentials; or separate credential failures, provider failures, and absent-only anonymity. |
| Known peer behavior | Frameworks differ on 401 versus 403 and often make “optional authentication” ignore invalid credentials. That can hide attacks and stale client configuration. |
| Selected behavior | Absent, invalid, rejected, and ambiguous credentials map to 401 only when a valid challenge is available. Unavailable, unclassified, invalid-result, and missing-challenge failures map to a secret-safe 503. `WithOptionalAnonymous` permits anonymous access only for `ErrCredentialsAbsent`; any supplied invalid or rejected credential still fails. The package never emits 403 or `insufficient_scope` because authorization owns those decisions. |
| Security and resource consequences | Optional routes cannot be downgraded by malformed credentials. Provider details, tokens, and causes never enter the response body. Unavailability is distinguishable for retries and operations. |
| Compatibility and wire consequences | Clients receive `authentication failed` for challenged credential failures and `authentication unavailable` for server-side inability to complete a conforming decision. Authorization middleware remains responsible for 403. |
| Executable evidence | `authhttp.TestMiddlewareFailsClosedWithChallengesAndRedaction`, `authhttp.TestOptionalMiddlewareAllowsOnlyAbsentCredentials`, `authhttp.TestMiddlewareRejectsInvalidConfigurationAndResult`, `authhttp.TestMiddlewarePropagatesRequestCancellation`, and `authhttp.TestMiddlewareTreatsMissingOrInvalidFailureChallengesAsUnavailable` |
| Public surface | `FailureKind`, exported sentinel errors, `authhttp.WithOptionalAnonymous`, `authhttp.NewMiddleware`, and HTTP response behavior |
| Upstream record | RFC 6750 `insufficient_scope` applies to authorization and is intentionally outside this package boundary. |
| Reconsider when | The package introduces a distinct transport adapter with a different standardized error mapping. |

## AUTH-DEC-009: Composite authenticator fallback

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication` maintainers |
| Source | No HTTP authentication specification defines in-process composition across several validators for one credential kind; this package-owned policy is constrained by the [RFC 9110 authentication framework](https://www.rfc-editor.org/rfc/rfc9110.html#section-11) and [RFC 6750 bearer error taxonomy](https://www.rfc-editor.org/rfc/rfc6750.html#section-3.1) |
| Classification | Application composition and downgrade-prevention policy |
| Issue | During rotation or migration, several authenticators can recognize the same credential kind. Falling through after malformed input, provider outage, cancellation, or an unclassified error can convert a terminal failure into acceptance by a weaker validator. |
| Credible interpretations | First authenticator always decides; fall through after every error; race all validators; or fall through only after an explicit credential rejection while making every other outcome terminal. |
| Known peer behavior | Authentication chains vary between first-success, first-result, and exception-driven fallback. Those models do not share this package's typed failure contract. |
| Selected behavior | Bindings are grouped by credential kind and evaluated in declaration order. Only `ErrCredentialsRejected` falls through. Absent, invalid, ambiguous, unavailable, cancellation, and unclassified errors are terminal; unclassified errors become unavailable. A successful result must contain an authenticated principal. If all validators reject, their valid challenges are retained in declaration order. |
| Security and resource consequences | A stronger validator's outage or parser rejection cannot downgrade to a weaker fallback. Work is bounded by the configured binding count and remains synchronous and context-aware. |
| Compatibility and wire consequences | Declaration order is observable only among validators for the same kind. Rotation chains must return rejection only when another validator is genuinely safe to try. |
| Executable evidence | `authentication.TestCompositeFallsThroughOnlyRejectedAuthenticators`, `authentication.TestCompositeStopsOnNonRejectedFailure`, `authentication.TestCompositeCombinesRejectedChallenges`, and `authentication.TestCompositeRejectsInvalidConfigurationAndResults` |
| Public surface | `authentication.Binding`, `authentication.Composite`, `authentication.NewComposite`, `FailureKind`, and challenge aggregation |
| Upstream record | This policy is intentionally package-owned and does not claim to extend RFC 9110. |
| Reconsider when | A separate versioned composition profile defines parallel validation, quorum, or another downgrade-safe strategy. |

## AUTH-DEC-010: Static secret matching and rotation snapshots

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication` maintainers |
| Source | No wire specification defines in-memory credential storage or rotation; [RFC 7617 security considerations](https://www.rfc-editor.org/rfc/rfc7617.html#section-4) and [RFC 6750 security threats](https://www.rfc-editor.org/rfc/rfc6750.html#section-5.1) require protection appropriate to reusable credentials but leave server implementation policy to the application |
| Classification | Defensive cryptographic comparison and lifecycle policy |
| Issue | Direct string lookup leaks representation and can expose timing differences between known and unknown identifiers. In-place rotation can expose partial sets or revoke the previous set after a failed update. |
| Credible interpretations | Store plaintext map keys; compare until first match; hash only secrets; mutate sets in place; or publish complete validated digest snapshots and compare every candidate. |
| Known peer behavior | Static middleware often uses map lookup plus direct equality. That is simple but does not provide this package's fixed-size comparison and atomic replacement contract. |
| Selected behavior | Static Basic, bearer, and API-key authenticators derive per-instance HMAC-SHA-256 digests with random keys and compare the complete bounded candidate snapshot in constant work relative to entry count. Duplicate credentials and invalid principals reject configuration. Bearer and API-key replacement validates a complete new snapshot before atomic publication; failed replacement leaves the previous snapshot active. Basic rotation uses construction and deployment of a new immutable authenticator. |
| Security and resource consequences | Raw configured secrets are not retained as lookup keys, comparisons use fixed-size digests, and active-set size is bounded. This reduces but does not claim to eliminate all process-level timing or memory-observation risk. |
| Compatibility and wire consequences | Credential bytes remain exact. Rotation overlap and removal are explicit; no hidden expiry, persistence, remote refresh, or background goroutine exists. |
| Executable evidence | `basic.TestStaticAuthenticatesConfiguredBasicCredential`, `basic.TestStaticRejectsUnsafeConfiguration`, `basic.TestStaticAcceptsExactEntryLimitAndKeepsEarlierMatch`, `bearer.TestStaticRotatesBoundedBearerKeysAtomically`, `bearer.TestStaticBearerRotationIsRaceSafe`, `bearer.TestStaticBearerFailedReplacementKeepsPreviousSet`, `apikey.TestStaticAuthenticatesKeyByDeterministicID`, `apikey.TestStaticRotationAtomicallyReplacesActiveKeys`, `apikey.TestStaticRotationIsRaceSafe`, `apikey.TestStaticRejectsDuplicateKeyConfiguration`, and the static-authentication benchmarks in all three packages |
| Public surface | `basic.NewStatic`, `bearer.NewStatic`, `bearer.Static.Replace`, `apikey.NewStatic`, and `apikey.Static.Replace` |
| Upstream record | This is package-owned implementation policy rather than a protocol conformance claim. |
| Reconsider when | A vetted secret-storage primitive replaces the digest snapshots or rotation moves behind a durable provider contract. |

## Unresolved decisions

None for the root module's currently supported surfaces. New ambiguity,
contradiction, erratum, extension, or peer divergence MUST be registered before
selecting observable behavior. JWT and OIDC decisions remain release-blocking
in their respective nested-module registers rather than being hidden here.
