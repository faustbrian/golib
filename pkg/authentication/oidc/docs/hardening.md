# OIDC validation hardening matrix

This matrix defines the observable validation contract and the evidence used to
protect it. References are to OpenID Connect Core 1.0 and OpenID Connect
Discovery 1.0, both incorporating errata set 2, unless another specification is
named.
Observable ambiguities and package-owned policy choices are maintained in the
[specification decision register](specification-decisions.md).

## Discovery and provider metadata

| Boundary | Accepted contract | Rejected or unavailable contract | Primary evidence |
| --- | --- | --- | --- |
| Discovery location | Exact configured issuer plus `/.well-known/openid-configuration` | Query, fragment, userinfo, unsupported scheme, non-loopback HTTP | `TestConfigurationRejectsEachInvalidBoundary`, `TestRemoteURLValidationRejectsEachUnsafeComponent` |
| HTTP response | Status 200, one JSON media type, bounded headers and body | Redirect, missing or conflicting content type, oversized response, timeout, cancellation | `TestHTTPHardeningAndBoundedReaders`, `TestDiscoveryAndJWKRequestsAreBoundedAndCancelable` |
| JSON shape | One bounded object with unique, correctly typed specification members and bounded depth and collections | Duplicate members, explicit null or wrong standard member types, trailing JSON, excessive depth or collection size | `TestNewRejectsDuplicateDiscoveryMembersBeforeFetchingKeys`, `TestNewRejectsNullOptionalProviderMetadata`, fuzz targets |
| Issuer | Returned `issuer` is byte-for-byte equal to the configured issuer | Aliases, case/port/path/trailing-slash normalization, cross-issuer metadata | `TestNewPreservesDiscoveryDeadlineAndRejectsIssuerMismatch`, `TestValidatorRequiresExactIssuerDespiteUpstreamCompatibilityAliases` |
| Endpoints | HTTPS; HTTP only for loopback providers with explicit `InsecureHTTP` | Userinfo, fragments, unsupported schemes, non-loopback HTTP | `TestProviderMetadataValidationMatrix`, `TestRemoteURLValidationRejectsEachUnsafeComponent` |
| Response types | Unique non-empty values separated by one ASCII space | Tabs, Unicode whitespace, repeated/leading/trailing separators | `TestProviderMetadataValidationMatrix` |
| Subject types and scopes | Unique supported subject types; advertised scopes include `openid` | Unknown/duplicate subject types; advertised scopes without `openid` | `TestProviderMetadataValidationMatrix` |
| Algorithms | Provider advertises `RS256` and every locally configured algorithm | Missing, duplicate, or downgraded advertised algorithms | `TestNewRejectsSigningAlgorithmsNotAdvertisedByProvider` |

Discovery may advertise an HTTPS JWKS origin different from the issuer, as the
specification permits and Google currently requires. Applications that do not
fully trust provider-controlled egress must supply an `HTTPClient` transport
that restricts DNS, IP ranges, ports, and origins. The validator still binds
every accepted token to the exact configured issuer; a provider trusted to
publish metadata and keys is necessarily trusted to issue tokens.

## JWKS, caching, and refresh

| Boundary | Accepted contract | Rejected or unavailable contract | Primary evidence |
| --- | --- | --- | --- |
| JWK set | Bounded set with at least one permitted public signature key; standard metadata is typed and an advertised `key_ops` contains only `verify`; unrelated valid public encryption keys are ignored | Null, empty, duplicated, private, encryption-only, unsupported, or ambiguous candidate metadata and key shapes | `TestRemoteFetchRejectsAmbiguousJWKMetadata`, `TestRemoteFetchIgnoresUnrelatedEncryptionKeys`, `TestRemoteFetchRejectsTransportAndJWKFailures` |
| Key selection | Exact `kid`, algorithm, use/operation, and key-shape match | Missing `kid` with multiple keys, algorithm/key confusion, ambiguous candidates | `TestVerifyWithKeysRejectsMissingKeyIDForAmbiguousSet`, `TestJOSEKeyAlgorithmFamilies` |
| Rotation | Unknown `kid` triggers one synchronized probe after cooldown | Unbounded probes, stale key use after freshness expiry | `TestRemoteKeySetRefreshesRotationMissBeforeCacheExpiry`, `TestRemoteKeySetCachesRefreshFailureWithinRateLimit` |
| Rollback | Forward rotation and stable current sets | Reintroduction of key material retired by this validator instance | `TestRemoteKeySetRejectsRetiredKeyRollback` |
| Outage | Known key remains usable only while fresh | Expired cache or failed required refresh | `TestDiscoveryValidatorRotatesKeysAndFailsClosedWhenCacheExpiresDuringOutage` |
| Cancellation | Cancels only the initiating validation | Canceled owner poisoning the shared cooldown | `TestRemoteKeySetCancellationDoesNotPoisonSharedRefresh` |
| Process burst | One network owner; bounded admitted waiters | Retry storm or unbounded waiter growth | `TestRemoteKeySetSynchronizesLargeRefreshBurst`, `TestRemoteKeySetBoundsRefreshWaiters` |
| Fleet spread | Per-instance early refresh jitter for freshness windows above the minimum interval | Concentrated refresh for ordinary positive cache windows | `TestRemoteRefreshJitterSpreadsReplicaFleet` |

Retired-key history is process-local and bounded. Reconstructing a validator
resets that history. Provider cache lifetimes at the configured minimum cannot
be refreshed earlier without violating the request-rate floor; deployments
requiring coordination at that exact boundary need an external shared cache or
egress coordinator.

## ID-token and claim validation

| Boundary | Accepted contract | Rejected contract | Primary evidence |
| --- | --- | --- | --- |
| Compact token | Three non-empty strict base64url segments within `MaxTokenBytes`; unique bounded JSON objects and correctly typed standard JOSE header members | Empty/padded/malformed segments, null or malformed standard headers, duplicate members, excessive depth/member/collection count | `TestValidatorRejectsMalformedBoundedAndDuplicateTokens`, `TestInspectCompactTokenRejectsEachBoundary`, `FuzzInspectCompactToken` |
| Signature | Explicit asymmetric allowlist and matching public key | `none`, HMAC confusion, unconfigured algorithm, wrong key or tampered signature | `TestInspectCompactTokenRejectsEachBoundary`, remote key-selection tests |
| Issuer and subject | Exact issuer; non-empty subject of at most 255 ASCII bytes | Normalized issuer aliases, Unicode or oversized subject | `TestValidatorRequiresExactIssuerDespiteUpstreamCompatibilityAliases`, `TestValidatorRejectsNonASCIIOrOversizedSubject` |
| Audience and `azp` | Client ID present; all audiences trusted and unique; correct `azp` when present and required for multiple audiences | Missing client, untrusted/duplicate audience, missing or wrong `azp` | `TestValidatorEnforcesOIDCClaimsAndAuthorizedParty`, `TestValidatorRejectsUntrustedAdditionalAudiences` |
| Numeric dates | Required `iat` and `exp`; optional `nbf` and `auth_time`; fractional seconds and exact skew edges retained | Missing, malformed, out-of-range, expired, or future values outside skew | `TestValidatorAppliesExactFractionalClockEdges`, `TestNumericDateBoundariesAndFraction` |
| Nonce | Caller callback atomically consumes the nonce after every other claim is valid | Missing/replayed/expired nonce according to callback; callback error or panic | `TestValidatorAllowsExactlyOneConcurrentNonceConsumption`, `TestValidatorValidatesAllClaimsBeforeConsumingNonce` |
| Token binding | Supplied access token/code matches `at_hash`/`c_hash` for the signing hash family | Missing, malformed, mismatched, or unsupported hash binding | `TestValidateIDTokenBindsAccessTokenAndAuthorizationCode`, `TestTokenHashAlgorithmsAndMalformedHeaders` |
| Claims | Lossless JSON decoding; typed optional protocol claims; bounded scope/tenant shapes; registered fields excluded from arbitrary claims | Null or wrong protocol types, float-overflow values, invalid shapes, unsupported distributed claims | `TestValidatorRejectsInvalidPrincipalClaimShapes`, `TestValidatorRejectsClaimsThatCannotBeDecodedLosslessly`, `TestValidatorRejectsDistributedClaimsWithoutRetainingTokens` |

The caller owns authorization-flow state. For implicit or hybrid flows it must
call `ValidateIDToken` with every returned access token and authorization code,
and must compare paired hybrid ID tokens as required by Core. The validator
cannot infer omitted front-channel values. Nonce presence and replay policy are
owned by `NonceValidator` because they depend on the initiating request.

## Public option matrix

| Option | Security effect | Boundary evidence |
| --- | --- | --- |
| `Issuer` | Exact discovery and token trust identity | configuration, discovery, issuer tests |
| `ClientID` | Required audience and authorized party | audience/`azp` tests |
| `TrustedAudiences` | Explicit additional audience allowlist | additional-audience and copy tests |
| `Algorithms` | Asymmetric signature allowlist also required in metadata | configuration, metadata, header, and JWK tests |
| `Clock` | Deterministic time source; invoked outside refresh synchronization | clock and refresh tests |
| `ClockSkew` | Symmetric temporal tolerance | exact fractional edge tests |
| `NonceValidator` | Caller-owned presence and atomic replay consumption | rejection, panic, cancellation, and concurrent replay tests |
| `MaxTokenBytes` | Compact-token allocation/input ceiling | exact/oversized token tests and fuzz cap |
| `MaxClaims`, `MaxClaimDepth` | Claim member/depth ceilings | hostile JSON tests and fuzzing |
| `ScopeClaim`, `TenantClaim` | Typed principal extraction and protocol-field exclusion | principal claim-shape tests |
| `InsecureHTTP` | Loopback-only development provider exception | issuer and remote URL matrices |
| `HTTPClient` | DNS, TLS, egress, proxy, and shorter timeout policy | transport, redirect, timeout, and snapshot tests |
| `MaxHTTPBodyBytes` | Decompressed discovery/JWKS body ceiling | bounded body tests |
| `DiscoveryTimeout` | Initialization deadline | discovery deadline tests |
| `MaxKeys` | JWK count and parsing allocation ceiling | JWK count tests and fuzzing |
| `MinRefreshInterval` | Refresh retry floor and failure cooldown | cache and rate-limit tests |
| `MaxRefreshInterval` | Provider freshness ceiling | cache-header matrix |
| `MaxRefreshWaiters` | Per-process refresh admission bound | waiter cancellation/burst tests |
| `TokenBinding` | `at_hash` and `c_hash` inputs used only for one call | token-binding tests |

Configuration slices and the HTTP client value are copied at construction.
Collaborators referenced through interfaces or transports remain caller-owned
and must be concurrency-safe.

## Error and lifecycle matrix

| Condition | Classification | Data exposure |
| --- | --- | --- |
| Invalid configuration or metadata | invalid configuration | stable boundary name only |
| Empty/wrong credential kind/oversized bearer | credentials invalid | no token or claim text |
| Signature, issuer, claim, nonce, or binding rejection | credentials rejected | no provider, callback, token, nonce, or key text |
| Discovery/JWKS outage, rollback, timeout, waiter saturation, or cancellation | authentication unavailable | stable sentinel; cancellation identity retained |

The module starts no goroutines or timers. Refresh work is synchronous, one
caller owns each network operation, waiters retain their own cancellation, all
accepted/rejected response bodies are closed, and raw tokens are call-local.

## Conformance, interoperability, fuzzing, and performance

- The conformance target runs the complete specification-derived package test
  matrix and pinned Core section 2 example. Full OpenID Foundation RP profiles
  are recorded as not directly applicable because they require a complete HTTP
  authorization client beyond this package's boundary.
- Interoperability uses a dated Google discovery snapshot, synthetic metadata
  compatibility profiles, and an ephemeral provider-issued token from the
  pinned Keycloak 26.3.2 OCI image and checksummed realm fixture. The token is
  never persisted outside the task-owned temporary directory.
- `specification/manifest.tsv` records source URLs, versions, byte counts, and
  SHA-256 digests for pinned fixtures.
- Fuzz targets cover compact headers/claims, provider metadata, JWK sets, URLs,
  NumericDate values, Unicode, and the complete public validation path under
  explicit input ceilings.
- Benchmarks measure static warm validation, discovery initialization, rotation
  misses, parallel remote-cache contention, and hostile-input rejection.
