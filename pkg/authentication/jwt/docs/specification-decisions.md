# JWT specification decisions

This register records observable choices where JOSE, JWT, JSON, HTTP, or the
package's defensive policy permits more than one implementation. RFC text and
the IANA registry remain authoritative; peer behavior is interoperability
evidence, not a vote. The pinned vectors and their provenance are documented in
[`../specification/`](../specification/README.md).

## JWT-DEC-001: Compact signed JWTs are the only accepted serialization

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication/jwt` maintainers |
| Source | RFC 7519 [Section 7.2](https://www.rfc-editor.org/rfc/rfc7519.html#section-7.2), RFC 7515 [Sections 3.1 and 5.2](https://www.rfc-editor.org/rfc/rfc7515.html#section-3.1), and RFC 8725 [Section 3.12](https://www.rfc-editor.org/rfc/rfc8725.html#section-3.12) |
| Classification | Optional serialization support and defensive application policy |
| Issue | JWT permits JWS or JWE representations and JOSE defines compact and JSON serializations. Accepting all forms would make token kind, nesting, and parser behavior dependent on upstream defaults. |
| Credible interpretations | Accept every JOSE serialization; accept compact JWS and JWE; unwrap nested tokens; or require one signed compact JWS with exactly three segments. |
| Known peer behavior | JWX and golang-jwt both parse compact signed tokens, while their broader JOSE capabilities and permissive options differ. |
| Selected behavior | Accept exactly three non-empty canonical unpadded base64url segments representing one compact JWS. Reject JWE, JSON serialization, nested compact payloads, padding, non-zero pad bits, truncation, and trailing data. |
| Security and resource consequences | One bounded parse path avoids implicit decryption, nesting amplification, and ambiguous token-kind dispatch. `MaxTokenBytes`, claim-count, and depth limits apply before signature verification. |
| Compatibility and wire consequences | Valid compact JWS input interoperates with peers. Other standards-valid JOSE serializations are intentionally outside this package and fail as invalid credentials rather than being normalized. |
| Executable evidence | `TestValidatorRejectsNonCanonicalBase64TruncationAndNestedPayloads`, `TestInspectCompactJWTRejectsEachBoundary`, `TestRFC7515AppendixA2RS256CompactJWS`, and `FuzzInspectCompactJWT` |
| Public surface | `Config`, `New`, `Validator.ValidateBearer`, and `Validator.Authenticate` |
| Upstream record | RFC 7519 delegates cryptographic representation to JOSE; this package narrows that optional surface to signed compact JWT authentication. |
| Reconsider when | A concrete service contract requires another JOSE serialization and can preserve equivalent algorithm, nesting, resource, and token-kind controls. |

## JWT-DEC-002: JSON members, Unicode, numbers, and shape are inspected strictly

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication/jwt` maintainers |
| Source | RFC 8259 [Sections 4, 6, and 8.2](https://www.rfc-editor.org/rfc/rfc8259.html#section-4) and RFC 8725 [Sections 2.6 and 3.7](https://www.rfc-editor.org/rfc/rfc8725.html#section-2.6) |
| Classification | JSON interoperability ambiguity and defensive parsing policy |
| Issue | JSON only says object names SHOULD be unique, parsers disagree on duplicate members and invalid Unicode, and very large numbers can be accepted with implementation-specific precision or allocation cost. |
| Credible interpretations | Keep first or last duplicate; inherit the upstream JSON decoder; replace malformed Unicode; coerce quoted NumericDate values; or reject ambiguous and excessive encodings before JOSE parsing. |
| Known peer behavior | Go JSON and JWT libraries commonly accept last-member-wins objects and may replace invalid UTF-8, creating differential interpretation risk across validators. |
| Selected behavior | Protected headers and claims must be UTF-8 JSON objects with unique members, paired UTF-16 escapes, bounded nesting and collection size, and JSON numbers no longer than 128 encoded bytes. NumericDate claims must be JSON numbers, not strings. |
| Security and resource consequences | Duplicate-key smuggling, Unicode replacement mismatches, excessive nesting, and number-size amplification fail before trust decisions. Inspection adds one bounded pass over each decoded segment. |
| Compatibility and wire consequences | Canonical peer-produced JSON remains compatible. Inputs accepted by permissive last-member-wins or coercing implementations are deliberately rejected without rewriting their wire form. |
| Executable evidence | `TestValidatorRejectsDuplicateAndOversizedClaims`, `TestInspectJSONObjectRejectsNonInteroperableUnicodeAndHugeNumbers`, `TestJSONUnicodeEscapeValidationAcceptsPairsAndRejectsMalformedPairs`, and `TestValidatorRejectsMalformedNumericDates` |
| Public surface | `Config.MaxClaims`, `Config.MaxClaimDepth`, `Config.MaxTokenBytes`, and `Validator.ValidateBearer` |
| Upstream record | RFC 8725 identifies multiplicity of JSON encodings as a substitution and validation risk; no upstream erratum mandates first- or last-member precedence. |
| Reconsider when | JSON or JWT publishes a mandatory duplicate-member and Unicode handling rule that is at least as deterministic and fail closed. |

## JWT-DEC-003: Algorithms are explicit, allowlisted, and never inferred

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication/jwt` maintainers |
| Source | RFC 8725 [Sections 2.1, 3.1, and 3.2](https://www.rfc-editor.org/rfc/rfc8725.html#section-3.1), RFC 7518 [Section 3](https://www.rfc-editor.org/rfc/rfc7518.html#section-3), and the [IANA JOSE registry](https://www.iana.org/assignments/jose/jose.xhtml) |
| Classification | Normative verification requirement plus defensive algorithm policy |
| Issue | A token declares `alg`, libraries expose changing registries, and algorithm confusion occurs if verification derives policy from attacker-controlled headers or key shape. Registry status also changes independently of package releases. |
| Credible interpretations | Trust the token algorithm; accept every library-supported algorithm; infer an algorithm from the JWK; use a deployment allowlist; or pin a smaller package-supported matrix. |
| Known peer behavior | JWX and golang-jwt support configurable algorithm sets. Both can be configured more broadly than this package and therefore require caller discipline when used directly. |
| Selected behavior | Configuration must explicitly allow one or more supported algorithms. The token `alg`, configured allowlist, JWK `alg`, JWK type, and key material must all agree. `none`, deprecated generic `EdDSA`, ES256K, unknown algorithms, and algorithms outside the documented matrix are rejected. |
| Security and resource consequences | Header-driven downgrade and symmetric/asymmetric confusion fail closed. The supported matrix bounds cryptographic implementations and upgrade review. |
| Compatibility and wire consequences | HS256/384/512, RS256/384/512, PS256/384/512, ES256/384/512, and Ed25519 compact JWS are accepted when explicitly configured. Other JOSE algorithms remain wire-incompatible by policy. |
| Executable evidence | `TestSupportedAlgorithmAndKeyMatrix`, `TestGolangJWTAlgorithmInteroperability`, and `TestValidatorRejectsAlgorithmKeyAndHeaderAttacks` |
| Public surface | `Config.Algorithms`, `New`, and `Validator.ValidateBearer` |
| Upstream record | The IANA registry is reviewed on dependency and release updates; registry presence does not itself opt an algorithm into this package. |
| Reconsider when | A new algorithm has stable Go support, an acceptable registry status, concrete adoption demand, and complete key-policy and interoperability evidence. |

## JWT-DEC-004: Verification keys require explicit identity, purpose, and strength

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication/jwt` maintainers |
| Source | RFC 7517 [Sections 4.2 through 4.7 and 5](https://www.rfc-editor.org/rfc/rfc7517.html#section-4.2), RFC 7518 [Sections 3.2 through 3.4](https://www.rfc-editor.org/rfc/rfc7518.html#section-3.2), and RFC 8725 [Section 3.4](https://www.rfc-editor.org/rfc/rfc8725.html#section-3.4) |
| Classification | Defensive cryptographic-input and key-selection policy |
| Issue | JWK fields are partly optional, multiple keys may otherwise match, and standards specify minimums without defining this package's complete operational work bounds. |
| Credible interpretations | Try every key; accept absent `kid` or `alg`; ignore `use` and `key_ops`; accept private asymmetric keys; enforce only library minimums; or require one unambiguous bounded verification key. |
| Known peer behavior | General JOSE libraries support flexible selectors and may try multiple keys. Their flexibility is useful outside this narrower authentication boundary but increases configuration ambiguity here. |
| Selected behavior | Every JWK needs a unique non-empty `kid` and explicit allowed `alg`. If present, `use` is `sig` and `key_ops` includes `verify`. Asymmetric keys are public only. HMAC keys meet algorithm output size, RSA moduli are 2048 through 8192 bits, and EC curves match the selected algorithm exactly. |
| Security and resource consequences | Deterministic key selection prevents key confusion and trial amplification. Minimum strength rejects weak keys; the RSA upper bound prevents attacker- or operator-supplied excessive verification work. |
| Compatibility and wire consequences | Well-formed verification JWKS interoperate unchanged. Sets with duplicate IDs, metadata omissions, signing-only operations, private keys, weak keys, or oversized RSA keys are rejected at configuration or refresh time. |
| Executable evidence | `TestValidatorRejectsCryptographicallyUnsafeKeys`, `TestValidateKeyMaterialRejectsEveryInvalidRepresentation`, `TestRemoteJWKValidationRejectsEveryKeyPolicyViolation`, and `TestRFC7520HMACJWKInteroperability` |
| Public surface | `Config.KeySet`, `Config.MaxKeys`, `KeyProvider`, `Remote.KeySet`, and `New` |
| Upstream record | RFC 7517 permits application-specific JWK selection policy; this package records its stricter authentication profile here. |
| Reconsider when | A standards profile requires key selection without `kid` or `alg` and supplies an equally unambiguous bounded selector. |

## JWT-DEC-005: Token-controlled key references and unknown critical headers are rejected

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication/jwt` maintainers |
| Source | RFC 7515 [Sections 4.1.2 through 4.1.11](https://www.rfc-editor.org/rfc/rfc7515.html#section-4.1.2) and RFC 8725 [Sections 2.9 and 3.11](https://www.rfc-editor.org/rfc/rfc8725.html#section-2.9) |
| Classification | Optional JOSE-header support and defensive trust-boundary policy |
| Issue | JOSE permits embedded or URL-referenced keys and critical extensions, but blindly honoring them lets an untrusted token influence trust material, network access, or parsing semantics. |
| Credible interpretations | Resolve `jku` or `x5u`; trust embedded `jwk` or `x5c`; ignore unknown `crit`; delegate every extension to the JOSE library; or reject token-controlled trust and unsupported critical behavior. |
| Known peer behavior | General JOSE libraries expose hooks for remote and embedded key resolution. Safe use depends on application-supplied allowlists and extension handlers. |
| Selected behavior | Reject `jku`, `jwk`, `x5u`, `x5c`, `x5t`, and `x5t#S256` in token headers. Reject `crit` rather than claiming support for extensions this package does not implement. Keys come only from the configured static set or provider. |
| Security and resource consequences | Validation performs no token-directed network I/O and cannot replace configured trust with attacker material. Unsupported extension semantics fail before cryptographic work proceeds. |
| Compatibility and wire consequences | Tokens relying on certificate chains, embedded keys, remote key URLs, or critical extensions are intentionally incompatible. Ordinary `alg` and `kid` headers are unchanged. |
| Executable evidence | `TestValidatorRejectsAlgorithmKeyAndHeaderAttacks` and `TestInspectCompactJWTRejectsEachBoundary` |
| Public surface | `Validator.ValidateBearer`, `Config.KeySet`, and `Config.Provider` |
| Upstream record | JOSE makes these headers optional and requires critical parameters to be understood; this package supports none of those trust-bearing extensions. |
| Reconsider when | A required profile defines one extension with a complete trust, retrieval, resource, and interoperability contract. |

## JWT-DEC-006: Identity and lifetime claims are mandatory and deployment-bound

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication/jwt` maintainers |
| Source | RFC 7519 [Sections 4.1.1 through 4.1.7 and 7.2](https://www.rfc-editor.org/rfc/rfc7519.html#section-4.1.1) and RFC 8725 [Sections 3.8 and 3.9](https://www.rfc-editor.org/rfc/rfc8725.html#section-3.8) |
| Classification | Application authentication profile over optional registered claims |
| Issue | JWT marks registered claims optional and leaves application validation requirements to deployments, while an authentication principal needs stable issuer, audience, subject, and lifetime semantics. |
| Credible interpretations | Require only signature validity; require `exp`; accept any audience or subject; infer defaults; or require a complete configured identity and lifetime profile. |
| Known peer behavior | JWX and golang-jwt expose claim validators as options. Direct defaults can accept claim sets that this package rejects. |
| Selected behavior | Require non-empty exact `iss`, configured `aud`, non-empty `sub`, numeric `iat`, and numeric `exp`. Optionally require an exact subject allowlist and unique deployment claims. `nbf`, when present, is numeric and enforced. |
| Security and resource consequences | Tokens cannot cross issuer or recipient boundaries or become timeless bearer credentials. Required claim counts are validated against configured resource limits before construction. |
| Compatibility and wire consequences | Tokens missing a mandatory claim or using another issuer, audience, or subject policy are rejected even though generic JWT parsers may accept them. Audience string and array forms remain supported through standards-aware validation. |
| Executable evidence | `TestValidatorRejectsInvalidJWTTrustDecisions`, `TestValidatorEnforcesSubjectAndRequiredClaimPolicy`, `TestConfigurationReservesCapacityForMandatoryAndRequiredClaims`, and `TestValidatorRejectsInvalidPrincipalClaims` |
| Public surface | `Config.Issuer`, `Config.Audience`, `Config.Subjects`, `Config.RequiredClaims`, and `Validator.ValidateBearer` |
| Upstream record | RFC 7519 explicitly leaves required claims to applications; this package's authentication profile is the application policy. |
| Reconsider when | A distinct token profile needs different mandatory claims; it should normally use a separate validator configuration rather than weaken this profile. |

## JWT-DEC-007: NumericDate edges use one injected clock and bounded non-negative skew

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication/jwt` maintainers |
| Source | RFC 7519 [Sections 2, 4.1.4, 4.1.5, and 4.1.6](https://www.rfc-editor.org/rfc/rfc7519.html#section-2) |
| Classification | NumericDate boundary interpretation and defensive clock policy |
| Issue | RFC 7519 permits small leeway but does not define its value, and edge comparisons can differ at exact expiration, not-before, or issued-at instants. |
| Credible interpretations | Use wall clock directly; accept arbitrary leeway; treat expiration equality as valid; ignore future `iat`; or use one injected instant and explicit skew equations. |
| Known peer behavior | JWT libraries expose time functions and leeway options, but defaults and optional `iat` validation differ. |
| Selected behavior | Read one injected clock per validation. Enforce `nbf <= now + skew`, `iat <= now + skew`, and `now < exp + skew`; equality at `exp` without skew is expired. Skew is explicit, non-negative, and shared across the three boundaries. |
| Security and resource consequences | Deterministic checks prevent inconsistent multi-read clock edges. Bounded operator-selected skew limits replay extension while tolerating measured clock error. |
| Compatibility and wire consequences | NumericDate values preserve second-based JWT semantics. Tokens exactly at expiration are rejected; permissive peers or deployments with larger leeway may differ intentionally. |
| Executable evidence | `TestValidatorHonorsExactNumericDateBoundaries`, `TestNumericDateValidationChecksEveryPresentClaimAndDigitBoundary`, `TestValidatorRejectsMalformedNumericDates`, and `TestValidatorHonorsCancellationAndConfigurationBounds` |
| Public surface | `Config.Clock`, `Config.Skew`, `New`, and `Validator.ValidateBearer` |
| Upstream record | RFC 7519 permits only a small leeway and leaves its magnitude to implementers; this package requires callers to own that deployment decision. |
| Reconsider when | A profile specifies distinct skew rules per claim or higher-resolution time semantics. |

## JWT-DEC-008: Static and dynamic key providers preserve ownership and one trust source

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication/jwt` maintainers |
| Source | RFC 7517 [Section 5](https://www.rfc-editor.org/rfc/rfc7517.html#section-5) and RFC 8725 [Section 3.4](https://www.rfc-editor.org/rfc/rfc8725.html#section-3.4) |
| Classification | Implementation-defined key-source and ownership policy |
| Issue | Standards define JWK Sets but not Go ownership, mutation, provider errors, typed nil values, or precedence when static and dynamic sources are both configured. |
| Credible interpretations | Retain caller sets by reference; prefer one source silently; merge static and dynamic keys; expose provider-owned sets; or require exactly one source and copy at every boundary. |
| Known peer behavior | General libraries commonly retain sets or cache references according to their own ownership contracts. Those contracts do not establish this package's concurrency boundary. |
| Selected behavior | Configure exactly one of `KeySet` or `Provider`. Static sets are copied and validated during construction; provider sets are copied and revalidated for each attempt. Typed-nil providers fail configuration. Returned principals and private claims are caller-owned copies. |
| Security and resource consequences | Callers cannot mutate active trust state through aliases, and provider changes cannot bypass key limits or algorithm policy. Copying is bounded by `MaxKeys`, token, and claim limits. |
| Compatibility and wire consequences | This changes no JWT bytes. Applications that previously mutated a retained key set must publish a new provider snapshot or remote refresh instead. |
| Executable evidence | `TestConfigurationAndKeySetValidationBoundaries`, `TestValidateBearerAndProviderFailureBoundaries`, and `TestRemoteDoesNotTransferCachedKeyOwnership` |
| Public surface | `Config.KeySet`, `Config.Provider`, `KeyProvider`, `KeyProviderFunc`, `Remote.KeySet`, and `authentication.Principal` |
| Upstream record | JWK standards do not define in-process ownership; this policy is package-owned and documented as stricter than direct library use. |
| Reconsider when | Go gains an immutable JWK representation with equivalent validation and lifecycle guarantees. |

## JWT-DEC-009: Remote JWKS retrieval is exact-authority, bounded, and redirect-free

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication/jwt` maintainers |
| Source | RFC 9110 [Sections 7.6 and 15.4](https://www.rfc-editor.org/rfc/rfc9110.html#section-15.4), RFC 8725 [Section 2.9](https://www.rfc-editor.org/rfc/rfc8725.html#section-2.9), and RFC 7517 [Section 5](https://www.rfc-editor.org/rfc/rfc7517.html#section-5) |
| Classification | Defensive remote-resource and SSRF policy |
| Issue | JWK Set retrieval standards do not define redirects, compression, private-network policy, response limits, endpoint mutation, or Go transport ownership. |
| Credible interpretations | Follow ordinary HTTP defaults; honor redirects and compression; permit any URL; ban private issuers globally; or require one configured exact URL with caller-owned network policy. |
| Known peer behavior | JWX provides a remote cache and HTTP customization. Default HTTP clients can follow redirects and transparently decompress unless hardened by the caller. |
| Selected behavior | `NewRemote` uses one configured URL, requires HTTPS except for an explicit development option, rejects user info and fragments, denies every redirect and compressed response, and bounds initialization, headers, body, keys, and concurrent operations. Private-address and DNS policy belongs to the supplied transport because legitimate deployments use private issuers. |
| Security and resource consequences | Token input never selects a URL. Exact-authority checks and disabled redirects prevent credential forwarding to a changed authority; byte, time, and operation bounds limit remote amplification. |
| Compatibility and wire consequences | Standards-compliant direct HTTPS JWKS responses work. Redirecting, compressed, oversized, or plain-HTTP production endpoints are intentionally incompatible unless the caller explicitly chooses the development exception. |
| Executable evidence | `TestRemoteRejectsRedirects`, `TestRemoteRejectsHostileJWKResponsesAtInitialization`, `TestRemoteConfigurationRejectsEachUnsafeBoundary`, `TestJWKResponseTransportRejectsBrokenAndOversizedResponses`, and `FuzzRemoteJWKResponseBoundary` |
| Public surface | `NewRemote`, `WithHTTPClient`, `WithInsecureHTTP`, `WithMaxJWKBodyBytes`, `WithMaxJWKHeaderBytes`, `WithMaxJWKKeys`, and `WithInitializationTimeout` |
| Upstream record | No JWT or JWK RFC defines a complete remote-fetch trust policy; the endpoint is trusted application configuration and the remaining network policy is explicit. |
| Reconsider when | A required discovery profile supplies stronger exact-origin redirect and endpoint rules that can be adopted without hidden network behavior. |

## JWT-DEC-010: HTTP freshness controls bounded refresh with fail-stale validation

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication/jwt` maintainers |
| Source | RFC 9111 [Sections 4.2, 5.2.2.1, 5.2.2.3, 5.2.2.4, 5.2.2.9, and 5.2.3](https://www.rfc-editor.org/rfc/rfc9111.html#section-4.2) |
| Classification | HTTP cache interpretation and application availability policy |
| Issue | JWKS rotation needs refresh, while HTTP freshness metadata may be absent, malformed, stale, or demand revalidation. JWT standards do not decide whether an issuer outage invalidates previously trusted keys. |
| Credible interpretations | Ignore cache headers; refresh every validation; trust arbitrary server lifetimes; drop keys on refresh failure; serve stale indefinitely; or combine HTTP freshness with bounded local policy. |
| Known peer behavior | Remote JWKS caches generally retain the last successful set and use cache metadata, but refresh timing, outage behavior, jitter, and unknown-key misses vary. |
| Selected behavior | `max-age` and `Expires` determine refresh time inside configured minimum and maximum bounds; `Age` reduces remaining `max-age`. Missing or unusable metadata uses the minimum. `no-cache`, `no-store`, and `must-revalidate` force the minimum. Successful refresh atomically replaces the set. Failed refresh reports unavailable but retains the last validated set for known keys; it never admits an unknown key. |
| Security and resource consequences | Bounds prevent an issuer from suppressing refresh indefinitely or causing a tight fetch loop. Fail-stale improves outage tolerance but extends trust in the last successful keys; applications needing a freshness deadline must enforce it by closing or withdrawing the provider. |
| Compatibility and wire consequences | No token wire form changes. Rotation becomes visible after bounded refresh, while immediate unknown-key refetch is deliberately absent to prevent attacker-driven fetch amplification. |
| Executable evidence | `TestRemoteJWKRotationAndIssuerOutage`, `TestRemoteRefreshTimingHonorsBoundsAndCacheHeaders`, `TestRemoteRefreshAndAuthenticationAreRaceSafe`, and `TestRemoteRefreshSchedulingHasFleetJitter` |
| Public surface | `WithRefreshBounds`, `WithRefreshJitter`, `Remote.Refresh`, and `Remote.KeySet` |
| Upstream record | HTTP defines freshness directives but not JWT outage policy; fail-stale is an explicit package decision rather than a standards claim. |
| Reconsider when | A deployment profile mandates maximum key age or an issuer supplies a reliable push/invalidation mechanism that preserves bounded work. |

## JWT-DEC-011: Remote lifecycle serializes fetches and makes shutdown explicit

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication/jwt` maintainers |
| Source | RFC 9110 [Section 9.2.2](https://www.rfc-editor.org/rfc/rfc9110.html#section-9.2.2) and Go's [`context` contract](https://pkg.go.dev/context) |
| Classification | Implementation-defined concurrency, cancellation, and lifecycle policy |
| Issue | Neither JOSE nor HTTP defines cache goroutine ownership, overlapping refreshes, constructor-context lifetime, waiter cancellation, or close behavior for a Go provider. |
| Credible interpretations | Let every miss fetch; overlap automatic and explicit refreshes; tie lifetime to constructor context; make close fire-and-forget; or serialize remote work under an owned lifecycle. |
| Known peer behavior | Caches and singleflight implementations coalesce work differently. Cancellation often stops only a waiter, not shared admitted work. |
| Selected behavior | Initial registration is bounded. Automatic and explicit refreshes share one hardened serialized transport; overlapping explicit refreshes share one result. The successful constructor context does not own provider lifetime. `Close` rejects new work, cancels admitted operations, joins them, and shuts down cache goroutines; a canceled close reports its context and may be retried. |
| Security and resource consequences | At most one remote fetch proceeds per provider and admitted operation count is bounded, preventing refresh herds and shutdown leaks. Cancellation cannot silently abandon owned goroutines. |
| Compatibility and wire consequences | No JWT or HTTP wire bytes change. Callers must explicitly close providers and must not assume canceling the constructor context destroys a successfully returned provider. |
| Executable evidence | `TestRemoteCoalescesConcurrentRefreshes`, `TestRemoteSerializesAutomaticAndExplicitRefreshWork`, `TestRemoteLifetimeIsOwnedByClose`, `TestRemoteCloseReportsCanceledJoin`, and `TestRemoteCloseDeadlineIsNotBlockedByRefreshLock` |
| Public surface | `NewRemote`, `Remote.Refresh`, `Remote.KeySet`, and `Remote.Close` |
| Upstream record | This is a Go lifecycle contract layered over standards-defined messages; no upstream protocol erratum controls it. |
| Reconsider when | The underlying cache exposes a stronger lifecycle contract that can replace this wrapper without observable cancellation or ownership regression. |

## JWT-DEC-012: Failures expose stable categories without retaining secrets

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `authentication/jwt` maintainers |
| Source | RFC 8725 [Sections 2.9 and 3.10](https://www.rfc-editor.org/rfc/rfc8725.html#section-2.9) and RFC 7519 [Section 7.2](https://www.rfc-editor.org/rfc/rfc7519.html#section-7.2) |
| Classification | Defensive diagnostics and application error-policy decision |
| Issue | Callers need actionable categories, but raw parse, provider, transport, token, key, claim, endpoint, and response errors can retain credentials or create a validation oracle. |
| Credible interpretations | Return upstream errors verbatim; collapse every failure; expose token and key diagnostics; retain only safe standards sentinels; or wrap redacted stable application categories. |
| Known peer behavior | JOSE libraries expose detailed parse and validation errors. Those are useful in controlled tooling but are unsafe as an authentication-boundary response or log payload without filtering. |
| Selected behavior | Credential shape uses `ErrCredentialsInvalid`; signature, claim, and trust rejection uses `ErrCredentialsRejected`; provider, cancellation, capacity, and lifecycle failure uses `ErrAuthenticationUnavailable`. Safe JWX standards sentinels remain in the chain. Raw provider and transport causes are replaced by `ErrKeyProviderUnavailable`, and public text contains no token, signature, key, claim, query, endpoint, or remote body. |
| Security and resource consequences | Stable low-cardinality errors avoid credential disclosure and attacker-controlled telemetry. Bounded safe codes remain sufficient for retry and incident policy. |
| Compatibility and wire consequences | HTTP or RPC adapters receive stable categories rather than JOSE-specific wire diagnostics. Consumers must use `errors.Is` and `errors.As`, not match strings or expect raw provider causes. |
| Executable evidence | `TestProviderFailureIsUnavailableAndSecretSafe`, `TestRemoteFailureRedactsEndpointQueryAndTransportError`, `TestValidatorPreservesSafeStandardsErrorCategories`, and `TestRejectedJWTFailureWithoutStandardsCategory` |
| Public surface | `Validator.Authenticate`, `Validator.ValidateBearer`, `ErrKeyProviderUnavailable`, and `authentication.Failure` |
| Upstream record | JWT validation steps are normative, but public diagnostic detail is not; this package deliberately separates safe classification from secret-bearing causes. |
| Reconsider when | A standard protocol profile requires a specific external error code that can map from these categories without exposing sensitive detail. |

## Unresolved decisions

None. New algorithms, JOSE serializations, critical headers, key-reference
mechanisms, claim profiles, remote-fetch behavior, or peer divergences MUST be
registered before observable support is selected. An unresolved security,
wire, validation, resource, or lifecycle decision blocks release of the
affected surface.
