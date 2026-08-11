# Identity Platform Protocol Baselines

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Purpose and change control

This file is the normative protocol baseline for the identity-platform
program. A package MUST NOT advertise conformance to a protocol, extension,
profile, algorithm, binding, response mode, or field that is not selected here
or in a package goal. Requirements in a later specification override an older
specification only where the later document explicitly updates or replaces it.
Only verified errata IDs recorded in the conformance manifest before the first
affected assignment are part of an RFC baseline; later errata require an
explicit baseline change and evidence invalidation.

The coordinator MUST create a checked-in conformance manifest before assigning
the first affected protocol unit. For every source below, the manifest MUST
record the immutable URL, title, publication or draft revision, retrieved
SHA-256 digest, license or redistribution terms, and the package behaviors that
depend on it. Mutable aliases such as `/TR/webauthn-3/` or an unversioned test
suite branch MUST NOT be evidence inputs. Changing any selected source,
profile, fixture, tool, or algorithm invalidates affected evidence
fingerprints.

## OAuth authorization and protected resources

The OAuth authorization-server, social OAuth, and enterprise OAuth profiles
MUST implement the applicable requirements from this closed baseline:

- OAuth 2.1 Internet-Draft
  [`draft-ietf-oauth-v2-1-15`](https://www.ietf.org/archive/id/draft-ietf-oauth-v2-1-15.html),
  2 March 2026. The implementation MUST record that this is a draft claim and
  MUST NOT describe it as a published RFC.
- OAuth 2.0 Authorization Framework, RFC 6749, as constrained by OAuth 2.0
  Security Best Current Practice, RFC 9700. RFC 9700 security requirements take
  precedence over permissive legacy behavior in RFC 6749.
- PKCE, RFC 7636, with `S256` only. `plain` MUST NOT be accepted or advertised.
- Authorization Server Issuer Identification, RFC 9207. Authorization responses
  MUST include and clients MUST validate `iss`, unless a named client profile
  uses distinct redirect URIs per issuer and proves equivalent mix-up defense.
- Authorization Server Metadata, RFC 8414; Bearer Token Usage, RFC 6750;
  Token Revocation, RFC 7009; Token Introspection, RFC 7662; and OAuth Resource
  Indicators, RFC 8707 where multiple resources or audiences are supported.
- OAuth 2.0 Protected Resource Metadata, RFC 9728. The reference resource
  publishes `/.well-known/oauth-protected-resource`; its exact `resource`,
  `authorization_servers`, bearer-token methods, and scopes MUST match the
  configured `oauth_server.protected_resource.resource`, authorization-server
  issuer, and access-token audience/resource checks. The resource identifier is
  one canonical absolute HTTPS origin, equals the origin of
  `http.external_base_url`, and is compared byte-for-byte at metadata issuance,
  token issuance, introspection, and resource verification.
  Metadata retrieval and parsing obey the same bounded duplicate-key and HTTPS
  policy as authorization-server metadata. Path-bearing resource identifiers
  are unsupported by the reference profile and MUST be rejected.
- OAuth 2.0 for Native Apps, RFC 8252, only for an explicitly enabled native
  client profile. Claimed custom URI schemes and loopback redirects MUST follow
  that RFC exactly; otherwise they MUST be rejected.
- Device Authorization Grant, RFC 8628, for `oauth-server/device`.
- Dynamic Client Registration, RFC 7591, only under explicit reference-profile
  enablement. RFC 7591 registration is selected only when
  `oauth_server.dynamic_registration.enabled=true`. RFC 7592 management remains
  an unselected future profile and is not selected by enabling RFC 7591.
- Token Exchange, RFC 8693, only if the implementation advertises the standard
  token-exchange grant. The authenticated-session JWT exchange is otherwise a
  private extension and MUST use a namespaced grant identifier and explicit
  non-standard metadata.

Authorization code and callback parsing MUST reject duplicated or conflicting
`code`, `state`, `iss`, `error`, `error_description`, and `error_uri`
parameters. Redirect comparison MUST be byte-for-byte exact after validating
the registered URI at registration time; fragments, userinfo, wildcards,
backslash confusion, and silent URI normalization are forbidden. An invalid or
untrusted redirect URI MUST produce a local error and MUST NOT redirect.
For the RFC 8252 loopback redirect exception, the scheme, loopback host, path,
query, and every other URI component MUST exactly match the registered URI;
only the port may vary. The actual listener port MUST be bound into the
authorization transaction and MUST match again when the code is redeemed.

The reference token endpoint supports `none` for public clients and
`client_secret_basic` for confidential clients. A request MUST use exactly one
registered authentication method and one credential channel. Public clients
MUST NOT have a client secret. Other methods, including `private_key_jwt`,
mutual TLS, and DPoP, are unsupported until separately selected and evidenced.
Token-bearing responses MUST send `Cache-Control: no-store` and
`Pragma: no-cache`.

The canonical authorization-server scope catalog is the exact sorted set
`email`, `identity:read`, `identity:write`, `offline_access`, `openid`, and
`profile`. The reference protected resource supports exactly `identity:read`
and `identity:write`; its RFC 9728 `scopes_supported` value and token scope
enforcement MUST match that subset. Discovery, client metadata, consent,
authorization, token issuance, introspection, and resource verification MUST
reject unknown or duplicate scopes rather than expanding the configured set.
The access-token audience for the protected resource is exactly
`oauth_server.protected_resource.resource`; RFC 9728 `resource`, authorization
requests, codes, tokens, introspection, and resource verification MUST use the
same typed value without deriving it from a request host.

The RFC 8628 device code contains 32 random bytes encoded as 43-character
unpadded base64url, is stored only as a domain-separated keyed digest, and is
bound to client, tenant, requested scopes/resources, expiry and polling state.
On each `slow_down` response the current polling interval increases by exactly
five seconds. The increment is not configurable; only the bounded cumulative
interval cap is profile policy.

Dynamic registration is disabled by default. Enabling it requires the exact
typed registration profile, an authorized initial access token, and an
immutable tenant, organization or platform owner. Its allowed-scope policy is
the exact configured subset of `oauth_server.scopes`; requests with unknown,
duplicate, or out-of-policy scopes are rejected before client creation. The
closed dynamic-registration struct is the sole registration authority. In the
reference profile RFC 7592 management is unselected, so no registration-management
URI or registration access token is issued. A future RFC 7592 profile must
select authenticated owner-bound read/update/delete operations and routes as
one coherent change.

OAuth JWS, JWT, and JWK processing MUST follow RFC 7515, RFC 7517, RFC 7518, and
RFC 7519. `alg=none`, algorithm substitution, symmetric/asymmetric key
confusion, duplicate `kid`, private JWK members in public JWKS, and incoherent
`use`, `key_ops`, `kty`, `crv`, and `alg` combinations MUST be rejected.
Every security-relevant form field and every JSON object at the discovery,
JWKS, token, JWT header/payload, and UserInfo boundaries MUST reject duplicate
names. An authorization response contains exactly one of `code` or `error` and
the one bound `state`. A PKCE verifier is 43..128 unreserved ASCII characters;
S256 is the exact unpadded base64url SHA-256 digest and comparison is constant
time. JSON and response parsing consumes the exact `oauth.*` and
`http.provider_response_bytes` manifest bounds.

Discovery/JWKS fetches require the exact configured HTTPS issuer and shared
external-HTTP SSRF policy, revalidate every resolved address and redirect, and
obey the manifest timeout, size, cache, and JSON limits. Redirects are disabled
for these fetches. An unknown `kid` permits exactly one single-flight JWKS
refresh; validation never falls back to an arbitrary key. OP ID tokens use
ES256. RP validation accepts only configured ES256 or RS256 and requires the
protected header, provider metadata, and registered client algorithm to agree.

## OpenID Connect

The OIDC provider and relying-party profiles are pinned to:

- [OpenID Connect Core 1.0 incorporating errata set
  2](https://openid.net/specs/openid-connect-core-1_0.html);
- [OpenID Connect Discovery 1.0 incorporating errata set
  2](https://openid.net/specs/openid-connect-discovery-1_0.html);
- [OAuth 2.0 Multiple Response Type Encoding Practices
  1.0](https://openid.net/specs/oauth-v2-multiple-response-types-1_0.html)
  only for response modes actually advertised; and
- [OpenID Connect RP-Initiated Logout
  1.0](https://openid.net/specs/openid-connect-rpinitiated-1_0.html), Final,
  12 September 2022, when logout is advertised.

The reference profile supports authorization-code flow only. Implicit, hybrid,
JARM, request objects, encrypted ID tokens, front-channel logout, and
back-channel logout MUST be omitted from metadata and rejected with typed
unsupported-profile errors. ID token and UserInfo proof MUST cover issuer,
audience, `azp`, subject, nonce, time, `auth_time`, `acr`/`amr` where claimed,
and `at_hash`/`c_hash` where the selected flow requires them. UserInfo `sub`
MUST equal the ID-token subject for the same grant.

The reference authorization response mode is `query`; `form_post`, fragment,
and JWT-secured response modes are not selected. RP-Initiated Logout is enabled
for the reference OIDC provider and relying-party profiles. Its
`post_logout_redirect_uri` MUST be pre-registered and matched byte-for-byte,
and the provider MUST validate the `id_token_hint` issuer, audience, signature,
and session binding before accepting it as logout context. Front-channel and
back-channel logout are not selected and MUST be omitted from metadata.

The reference issuer is origin-only and has no path other than `/`.
Path-bearing issuers are rejected at configuration validation, so RFC 8414 and
OIDC well-known paths cannot disagree with the canonical API route table.
Pairwise subjects remain disabled unless the client has an exact sector
identifier and the deployment configures a versioned secret derivation key.
When enabled, HMAC-SHA-256 binds sector and internal subject under a
domain-separated key; rotation MUST preserve stable subjects through an
explicit overlap/migration and MUST NOT expose the raw internal subject.

## SAML 2.0

The SAML service-provider profile is pinned to the OASIS Standard publications
dated 15 March 2005:

- [Assertions and Protocols for SAML
  V2.0](https://docs.oasis-open.org/security/saml/v2.0/saml-core-2.0-os.pdf);
- [Bindings for SAML
  V2.0](https://docs.oasis-open.org/security/saml/v2.0/saml-bindings-2.0-os.pdf);
- [Profiles for SAML
  V2.0](https://docs.oasis-open.org/security/saml/v2.0/saml-profiles-2.0-os.pdf);
- [Metadata for SAML
  V2.0](https://docs.oasis-open.org/security/saml/v2.0/saml-metadata-2.0-os.pdf);
  and
- [Security and Privacy Considerations for SAML
  V2.0](https://docs.oasis-open.org/security/saml/v2.0/saml-sec-consider-2.0-os.pdf).

The selected errata baseline is [SAML V2.0 Approved Errata
05](https://docs.oasis-open.org/security/saml/v2.0/errata05/os/saml-v2.0-errata05-os.pdf),
1 May 2012. Later errata are not silently incorporated.

XML signature processing is pinned to XML Signature Syntax and Processing 1.1,
W3C Recommendation 11 April 2013, and Exclusive XML Canonicalization 1.0, W3C
Recommendation 18 July 2002. HTTP-Redirect is the sole selected binding for
outbound AuthnRequest and LogoutRequest messages; HTTP-POST is the sole selected
binding for inbound login Response and LogoutRequest/LogoutResponse messages.
HTTP-POST AuthnRequest and HTTP-Redirect Response are unsupported. Artifact,
SOAP, and ECP are
unsupported. SP-initiated logout and inbound Single Logout are selected: every
logout message MUST be signed, bind the exact entity ID, destination,
request/response ID, session index, and subject mapping, use the normal
clock-skew/replay authority, and revoke the local session even when the remote
outcome is unavailable or unknown. A logout response MUST correlate to one
outstanding request and MUST NOT establish that the IdP revoked any session
beyond its signed status.
An inbound LogoutRequest arrives only by HTTP-POST at the exact configured SP
SLO URL; its signed response is returned only with HTTP-POST to the verified
IdP response endpoint. An outbound LogoutRequest uses HTTP-Redirect, and its
correlated inbound LogoutResponse uses HTTP-POST at the SP SLO URL. SP metadata
MUST advertise only the HTTP-POST SP SLO binding registered by the API route.

Every outbound HTTP-Redirect AuthnRequest and LogoutRequest uses `SigAlg`
`http://www.w3.org/2007/05/xmldsig-more#sha256-rsa-MGF1`. The signature input is
the exact transmitted percent-encoded query sequence
`SAMLRequest=value&RelayState=value&SigAlg=value` when RelayState is present and
`SAMLRequest=value&SigAlg=value` when it is absent; `Signature` is appended only
after signing that sequence. Missing, duplicated, unsupported, or mismatched
`SigAlg` is rejected, and the verifier MUST NOT substitute the configured
default for an inbound value.

An inbound login MUST contain trusted signature coverage over the exact
Response or Assertion node from which identity is consumed. The reference
profile requires both the Response and every consumed Assertion to be signed.
A production profile MUST NOT accept both an unsigned Response and unsigned
Assertion. The parser MUST reject DTDs,
external or custom entity declarations/references other than the five
predefined XML entities, XInclude, duplicate XML IDs, ambiguous namespace/
element duplication, unselected transforms or canonicalization, external
signature references, and a signature whose referenced node is not the exact
node subsequently evaluated. Encrypted assertions, when selected later, MUST
be decrypted before the resulting assertion signature and conditions are
validated.

SP-initiated login Responses MUST arrive at the exact configured
`saml.sp_acs_url`; that URL is the metadata ACS Location and MUST equal both
Response Destination and SubjectConfirmation Recipient byte-for-byte. They
MUST carry the outstanding request ID in Response and subject-confirmation
`InResponseTo` and bind the one-time RelayState. IdP-initiated login, when
explicitly enabled, MUST arrive at the distinct configured
`saml.sp_idp_initiated_url`; metadata advertises it as a separate non-default
HTTP-POST ACS. Destination and Recipient MUST equal that URL, it MUST NOT
fabricate or require an outstanding request ID, and its one-time RelayState
MUST bind the enabled unsolicited-response and login-CSRF/confirmation policy.

Validation order is fixed: bounded transport/XML parse; unique ID and exact
same-document reference resolution; trusted provider entity/metadata version;
Response issuer/signature; Success status; exact selected-route Destination;
outstanding request and `InResponseTo` for SP-initiated responses, or the
explicit IdP-initiated profile with both fields absent; exactly one Assertion;
Assertion issuer/signature;
Conditions time window and audience equal to SP entity ID; SubjectConfirmation
Recipient, `InResponseTo`, and `NotOnOrAfter`; NameID/attribute mapping; then an
atomic replay reservation for both Response and Assertion IDs before authority
issuance. IdP-initiated login is disabled by default and becomes enabled only
when `saml.idp_initiated=true` and every unsolicited-response,
login-CSRF/confirmation, destination, domain-proof, timing, and replay
prerequisite validates. Embedded `KeyInfo` is only a key
hint and never a trust anchor; verification keys come only from the activated
provider certificate set. The only signature transform chain is the enveloped-
signature transform when the Signature is enveloped, followed by exclusive
canonicalization without comments; XPath, XSLT, external references, and every
other transform are rejected.

Raw transport, decoded XML, element/depth, attribute, namespace, text, ID/URI,
attribute-value, signature, assertion, certificate, base64, Redirect-DEFLATE,
and inflate-work limits are the exact `saml.*` manifest rows and are enforced
before the next allocation stage.

The reference XML-signature profile accepts only RSA-PSS with SHA-256
(`http://www.w3.org/2007/05/xmldsig-more#sha256-rsa-MGF1`) using an RSA key of
at least 2048 bits, or ECDSA with SHA-256
(`http://www.w3.org/2001/04/xmldsig-more#ecdsa-sha256`) using NIST P-256. The
digest algorithm is SHA-256 (`http://www.w3.org/2001/04/xmlenc#sha256`) and the
canonicalization algorithm is Exclusive XML Canonicalization 1.0 without
comments (`http://www.w3.org/2001/10/xml-exc-c14n#`). Every other signature,
digest, canonicalization, transform, key type, curve, or undersized key MUST be
rejected rather than downgraded.

Metadata fetched over the network is untrusted input. The reference profile
requires an operator-approved entity ID, endpoints, signing keys, and SHA-256
metadata digest before activation. A refresh MUST NOT silently change any of
those values. Key rollover requires an explicit overlap and removal decision.

## SCIM 2.0

SCIM is pinned to RFC 7643 and RFC 7644 with the following exact RFC Editor
Verified errata snapshot retrieved 2026-08-11. RFC 7643 Verified errata IDs: 5368, 5606, 5607, 5990, 5991, 6004, 6727, 7522, 8361, 8415, 8417, 8435, 8450, 8471, 8472, 8475.
RFC 7644 Verified errata IDs: 6893, 7898, 7916, 8096, 8365.
RFC 7642 is informative terminology only. ServiceProviderConfig, Schemas, ResourceTypes,
Users, Groups, ListResponse, Error, PATCH, filter, sort, pagination, ETags, and
Bulk MUST match those RFCs for every advertised capability.
HTTP 428 behavior is pinned to RFC 6585. Every SCIM error response MUST use
`application/scim+json`, include the string form of the actual HTTP status in
`status`, and include only a registered bounded `scimType` and enumeration-safe
bounded `detail`. HTML, provider text, and internal errors are forbidden.

The canonical User `password` attribute MUST remain `writeOnly` and
`returned=never`; it MUST NOT be returned, filtered, sorted, logged, or stored
in a SCIM projection. The reference profile rejects password writes as
unsupported. A future password-write profile must route them through
`identity/password` and requires a separate approved goal.

`externalId` uniqueness MUST be scoped by tenant, organization, provisioning
connection/provider, resource type, and the schema's exact `caseExact` rule.
The server-generated SCIM `id` remains stable and unique in its resource
namespace. Every reference-profile PUT, PATCH, and DELETE requires `If-Match`;
missing preconditions return HTTP 428 and mismatches return HTTP 412 without a
state change.

The exact selected numeric limits are the `scim.*` rows in
`REFERENCE_CONFIGURATION.md`: list count defaults to 100 and is capped at
1,000; filter depth is 16 with 256 parsed nodes; PATCH is capped at 100
operations; Bulk is capped at 1,000 operations and 1 MiB with
`failOnErrors` capped at 100. Implementations MUST enforce the relevant byte,
node, and operation limit before allocation or mutation and MUST NOT advertise
a larger runtime value than the effective manifest snapshot.

Pagination is 1-based. Omitted `startIndex` is 1, omitted `count` is
`scim.page_default`, values above `scim.page_max` are capped, and `count=0`
returns no Resources while still returning exact `totalResults`, `startIndex`,
and `itemsPerPage=0`. Sort uses the selected SCIM comparison semantics and a
stable server ID tie-break. Each request observes one transaction snapshot;
there is no cross-request snapshot promise. ServiceProviderConfig MUST
advertise the effective filter and Bulk values from the manifest, including the
decoded Bulk byte limit. Every advertised schema-valid filter/sort uses an
indexed plan or a bounded fallback; only invalid grammar/path is unsupported.

## WebAuthn, passkeys, and authenticator data

WebAuthn is pinned to [Web Authentication Level 3, W3C Candidate Recommendation
Snapshot 26 May
2026](https://www.w3.org/TR/2026/CR-webauthn-3-20260526/). The conformance
manifest MUST use this immutable dated snapshot, not the mutable latest-editor
document. Supporting formats use:

- CBOR, RFC 8949;
- COSE structures and algorithms, RFC 9052 and RFC 9053;
- Internet X.509 Public Key Infrastructure Certificate and CRL Profile,
  RFC 5280; and
- [FIDO Metadata Service 3.1.1 Proposed Standard, 5 January
  2026](https://fidoalliance.org/specs/mds/fido-metadata-service-v3.1.1-ps-20260105.html)
  and [Metadata Statement 3.1.1 Proposed Standard, 5 January
  2026](https://fidoalliance.org/specs/mds/fido-metadata-statement-v3.1.1-ps-20260105.html)
  for any named metadata trust profile.

The minimum server profile supports attestation conveyance `none`, accepts the
`none` attestation statement, and parses and structurally validates `packed`
and `fido-u2f` for compatibility testing. It MUST NOT accept `packed` or
`fido-u2f` as trusted attestation until a named metadata/trust profile is
selected. Other formats MUST fail as unsupported until selected with fixtures
and trust policy.
The minimum extension profile understands typed `credProps` and `credProtect`
outputs. Backup eligibility/state are authenticator-data flags;
`crossOrigin` and `topOrigin` are validated members of collected client data,
not extension outputs. Unknown extension inputs and outputs MUST be discarded
unless a later profile assigns them a typed schema, strict size bound,
authority classification, and explicit retention purpose; they MUST never
affect an authorization decision.
The reference profile requires `crossOrigin=false` and `topOrigin` absent; a
cross-origin ceremony is rejected. A future related-origin profile MUST pin its
exact top origins and validation rules before accepting `crossOrigin=true`.
`uvm` and `appid` are not selected; they MUST NOT be requested, retained, or
advertised by the reference profile.

The reference algorithm allowlist is COSE ES256 (`-7`) and EdDSA (`-8`) pinned
to Ed25519 (`kty=1`, `crv=6`). RS256 (`-257`) is a named compatibility profile.
For a production HTTPS origin, the RP ID MUST be a valid domain string equal to
the origin's effective domain or a registrable domain suffix of it under the
pinned Public Suffix List snapshot; it MUST NOT be a public suffix or IP
literal. The only non-HTTPS/effective-domain exception is the exact
`localhost` development profile. Suffix-lookalikes, IP/domain substitution,
opaque origins, and unexpected cross-origin ceremonies MUST be rejected.

Credential IDs are unique across the complete RP namespace; tenant scope MUST
NOT disambiguate them. User handles are opaque, 32 random bytes in the reference
profile, unique within the RP, and never exceed WebAuthn's 64-byte limit.
Challenges are 32 random bytes, expire after five minutes, and are atomically
single-use. Passwordless, usernameless, recent-authentication, and MFA use MUST
require user verification. User-presence-only results MUST NOT satisfy those
purposes.

Before parsing or allocation, collected client data is limited to 16 KiB,
attestation objects to 64 KiB, authenticator data to 4 KiB, credential IDs to
1 KiB, CBOR nesting to 16 levels, and CBOR items to 256. User handles are 32
random bytes in the reference profile and MUST NOT exceed 64 bytes. A package
MUST consume these exact `webauthn.*` manifest values and MUST NOT define a
different local parser profile.

Backup eligibility is immutable after registration. `BS=1` with `BE=0` is
invalid. Backup state is persisted on each assertion. A zero or non-monotonic
signature counter on a backup-eligible credential is risk evidence, not by
itself proof of cloning; a decreased positive counter on a non-backup-eligible
credential denies the assertion and requires recovery or a separately verified
factor.
For a non-backup-eligible credential, stored counter greater than zero and any
received counter equal to or less than the stored value is suspected clone or
reset and is denied. Challenges are stored only as scoped keyed digests and
bind purpose, RP/configuration version, tenant, user or pre-auth transaction,
allowed origin, and expiry before single-use consumption. The RS256
compatibility profile requires COSE RSA `kty=3`, exponent 65537, modulus at
least 2048 bits, and SHA-256; every other RSA profile is rejected.

TOTP is pinned to RFC 6238 and HOTP moving-factor mechanics to RFC 4226. The
provisioning URI follows the Google Authenticator Key URI Format snapshot
recorded in the conformance manifest; issuer label/query equality and strict
percent encoding are mandatory.

## Pinned conformance tools

The initial checked-in conformance manifest MUST include these exact upstream
revisions. A package MAY add a tool but MUST NOT replace or float one of these
inputs without an approved baseline change and invalidated evidence:

| Scope | Upstream revision |
| --- | --- |
| OAuth/OIDC | OpenID Foundation conformance suite commit `3f2bc78770e9ebdbb8165b6be86ae85b99bb2fc8` |
| WebAuthn | Web Platform Tests commit `9bc6e2404bff5349e48d7962b0a495582bc5ade8` |
| SCIM | UnboundID SCIM 2 SDK tag `scim2-6.0.0`, commit `badd1eb5e8ee7ace3712a92f9d83891884f93189` |
| SAML | SimpleSAMLphp tag `v2.5.3.1`, dereferenced commit `e049be1819327c76403fc0d6fa648d6dcfbc8516` |

## Mandatory conformance evidence

Every protocol unit MUST retain, as atomic evidence checkpoints:

1. the protocol-source manifest and digests described above;
2. a requirement-to-test matrix covering every advertised feature and every
   explicit unsupported feature;
3. official examples or fixtures with immutable source/version/checksum;
4. hostile negative fixtures for parser limits, duplicate fields, replay,
   substitution, signature/key confusion, redirects, cross-scope access, and
   downgrade;
5. an independent implementation transcript for every advertised protocol
   profile, including product/version/configuration and redacted requests,
   responses, timestamps, and result;
6. a pinned conformance-suite/tool revision and unmodified machine-readable
   result where a maintained suite exists;
7. a browser/authenticator matrix for WebAuthn/passkeys and a named IdP/client
   matrix for federation and OAuth/OIDC; and
8. a canonical, content-addressed evidence index committed under repository
   provenance, binding every artifact digest to the package input fingerprint,
   execution revision, source/tool revision, environment identity, and result.
   The index digest MUST be covered by the repository's release provenance;
   no undefined out-of-band signer is assumed.

Self-verification by the implementation under test is insufficient.
Unavailable, skipped, warning-only, manually asserted, or unattributable
interoperability evidence blocks the advertised profile from becoming
`verified`.
