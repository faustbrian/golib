# Goal: pkg/identity/oauth/providers

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/oauth/providers`
- Canonical module: `pkg/identity/oauth/providers`
- Canonical goal after scaffolding: `pkg/identity/oauth/providers/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/oauth/providers:v1`; owned operation IDs: `contract:operation:identity.oauth.provider-apple-client-secret-sign:v1`, `contract:operation:identity.oauth.provider-list:v1`
- Requires: `identity/oauth`
- Consumes existing primitives: `authentication/oidc`, `http-client`, `secret-envelope`, `telemetry`
- Unlocks after verification: `identity/oauth/onetap`, `identity/http`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and MUST start only after
the coordinator marks this unit `in-progress` with `identity/oauth` verified.
Build one maintained built-in catalog of exact OAuth/OIDC provider profiles.
The generic orchestration remains in `identity/oauth`; this package supplies
provider facts, mappings and explicitly reviewed incompatibilities.

## Required provider baseline

Profiles are REQUIRED for Apple, Atlassian, Amazon Cognito, Discord, Dropbox,
Facebook, Figma, GitHub, GitLab, Google, Hugging Face, Kakao, Kick, LINE,
Linear, LinkedIn, Microsoft, Naver, Notion, Paybin, PayPal, Polar, Railway,
Reddit, Roblox, Salesforce, Slack, Spotify, TikTok, Twitch, Twitter/X, Vercel,
VK, WeChat and Zoom. Generic-OAuth helper profiles are additionally REQUIRED
for Auth0, Gumroad, HubSpot, Keycloak, Microsoft Entra ID, Okta, Patreon and
Yandex; LINE and Slack MUST serve both catalog uses without conflicting IDs.
Names and aliases MUST be stable and collision-free.

## Ownership and public contract

Each immutable profile MUST define authorization, token, user-info,
revocation and discovery endpoints as applicable; issuer and audience rules;
default and optional scopes; PKCE support; client authentication method;
authorization/token parameters; ID-token and user-info claims mapping;
stable provider-account ID; email and email-verification semantics; refresh,
expiry and revocation behavior; avatar/name mapping; and documented quirks.
Runtime client secrets, tenant-specific endpoints and requested scopes MUST be
provided explicitly without mutating global profile state.

The catalog MUST publish a machine-checkable provider matrix with one row per
stable provider ID and columns for aliases; OAuth/OIDC mode; fixed and
tenant-templated endpoints; discovery policy; issuer aliases; audience and
authorized-party rules; default, optional and forbidden scopes; PKCE and nonce;
client-authentication method; response modes; provider-required authorization
and token parameters; ID-token, UserInfo and introspection availability and
precedence; direct/native ID-token and access-token signin; signup and implicit
link suitability; refresh-token issuance and rotation; expiry; revocation and
unlink; and known incompatibilities. Every cell MUST be an explicit supported,
unsupported, conditional or unknown decision; absence MUST NOT inherit a
generic success default.

Its native-token capability projection MUST equal `native-token-modes-v1`
exactly: Apple=`id_token`; Facebook=`opaque_access_token`;
Google=`id_token,opaque_access_token`; LINE=`id_token,opaque_access_token`; and
every other provider ID has no native-token capability. The matrix MUST also
pin the corresponding offline ID-token or bounded opaque-token proof boundary;
an absent mode is unsupported, not conditional or inferred.

The checked-in matrix is
`CONFIGURATION_CATALOGS.json#provider_matrix` with version
`provider-matrix-v1`. Its row order and row IDs MUST equal the ordered
`provider-catalog-v1` ID list, and `catalog_sha256` MUST equal the provider
catalog checksum. Every row MUST retain the same closed key order and explicit
values for unsupported, conditional and unknown behavior; consumers MUST NOT
interpret an absent key as support. Configuration fields name credential
handles only and MUST NOT contain a credential, private key, token or other
secret value. The `official_docs` states `pin-required` and
`tenant-metadata-pin-required`, and the `interoperability` state `not-run`, are
deliberate blocking evidence states, not successful conformance claims.

The closed row schema is exactly `id`, `aliases`, `configuration`, `protocol`,
`endpoints`, `validation`, `scopes`, `response`, `token_auth`, `parameters`,
`pkce`, `nonce`, `state`, `identity_sources`, `claims`, `account_policy`,
`token_lifecycle`, `native_token_signin`, `apple_client_secret_signing`,
`incompatibilities` and `evidence`. `endpoints` includes revocation;
`validation` includes issuer aliases, audience and authorized party;
`scopes` includes forbidden scopes; `parameters` separates authorization and
token requirements; `identity_sources` closes ID-token, UserInfo,
introspection and precedence; and `token_lifecycle` closes refresh issuance,
rotation, expiry, revocation and unlink. `claims` MUST contain every mapping
named below with the exact classification fields enforced by the catalog
validator. Endpoint values are HTTPS/template values or the closed sentinels
`unsupported`, `unknown`, `discovery` and `tenant-metadata-pin-required`;
`discovery` requires the endpoint from validated pinned issuer metadata.

Every profile mapping MUST identify the stable provider subject field, email
field and verification rule, display name components, username or handle,
avatar URL and size variants, tenant or organization fields, locale and any
provider-specific raw field retained. It MUST declare required versus optional
fields, types, normalization, null/empty handling and whether each field is
authoritative, mutable, security-sensitive or display-only. Unknown claims
MUST NOT flow into identity metadata without a bounded declared extension
mapping.

Apple MUST select `response_type=code id_token` and `form_post` under the pinned
OAuth 2.0 Form Post Response Mode and bind both into authorization state with
the exact HTTPS callback. Success requires code, ID token and state plus
front-channel issuer, audience, `azp`, nonce, time and `c_hash` validation
before code exchange and cross-proof subject validation. Every other
provider defaults to `query`; a provider may not inherit, advertise, or accept
`form_post` unless a later named profile adds an exact clause pin, configuration
catalog decision, and interoperability evidence.

The package MUST expose the owner-visible direct operation
`identity.oauth.provider-apple-client-secret-sign`. Its typed request contains
exactly a canonical 10-character Apple team ID used as issuer, a bounded
canonical client ID used as subject, a canonical 10-character Apple key ID, an
opaque signing-key handle and a lifetime from one second through Apple's pinned
15,777,000-second maximum. It
MUST NOT accept a raw private key, algorithm, audience, issued-at or arbitrary
claim/header map. The signer reads issued-at exactly once from its injected
UTC clock, rejects a zero or non-second-aligned time, and computes expiry with
checked arithmetic from that value and the validated lifetime.

The key handle MUST resolve only through a cancellation-aware key-handle signer
whose private P-256 key is protected by the `secret-envelope` boundary. Raw key
bytes MUST NOT enter the request, result, error, logs, traces, metrics or test
diagnostics. The operation permits ES256 only. Its compact JWT protected header
MUST contain exactly `alg`=`ES256` then `kid`=<request key ID>; its claims MUST
contain exactly `iss`=<team ID>, `iat`=<integer UTC NumericDate>,
`exp`=<integer UTC NumericDate>, `aud`=`https://appleid.apple.com`, then
`sub`=<client ID>. Both JSON objects use that declared member order, minimal
JSON string escaping and unpadded base64url segments; no `typ`, extra header,
extra claim, fractional NumericDate or alternate audience is permitted. The
key-handle signer returns the 64-byte JOSE P-256 `R || S` signature for the
exact protected-header/payload signing input and MUST reject every other curve
or algorithm.

`NewAppleSigningKeyHandle` MUST make the successful request reachable from a
configured identifier of 1..256 ASCII bytes matching `[A-Za-z0-9._:/-]+` for a
preprovisioned key without accepting an envelope or key bytes. It MUST accept
the identifier byte-for-byte with no trimming, case folding, Unicode conversion
or other normalization. The opaque handle MUST redact formatting and
serialization. `AppleClientSecret.Use` MUST make the successful result usable
only through a synchronous token-request encoder callback: the callback MUST
NOT retain, copy, format or disclose the supplied mutable bytes. A defer MUST
overwrite that copy on success, error or panic. Nil callbacks, callback errors
and recovered callback panics return only stable `AppleClientSecretUseError`;
the callback error or panic value MUST be discarded rather than wrapped,
formatted or retained because it may contain the JWT.

The typed result contains a redacting `AppleClientSecret` plus the exact
issued-at and expiry metadata. Formatting, serialization and errors MUST never
reveal the compact JWT. Invalid IDs, handles, clock values, arithmetic or
lifetime bounds return the stable `invalid_bounds` error; canceled or expired
contexts return `canceled`; key lookup, envelope, curve and signing failures
return `signer_failure`. Every failure is redacted and returns a zero result.
The operation is internal/direct only: it has no HTTP route, handler or OpenAPI
operation and creates no durable state or event.

The package does not own browser redirects, state storage, account linking,
token vaults, sessions, enterprise SSO, provider SDK wrappers, or silent
provider guessing. A generic provider remains configurable through
`identity/oauth` even if it is absent from this catalog.

## Required behavior and security

Profile construction MUST validate HTTPS endpoints, controlled tenant
templates, scope and parameter collisions, issuer aliases and discovery
policy. Claims mapping MUST reject absent/unstable subject identifiers and MUST
not mark email verified unless the provider's pinned semantics prove it. A
provider-specific incompatibility MUST remain visible through a typed profile
decision; it MUST NOT be erased by a generic default. Unknown registration,
refresh, unlink or revocation outcomes MUST remain unknown and reconcilable.

Tests MUST protect against SSRF, tenant-template injection, issuer confusion,
algorithm downgrade, claims substitution, unverified-email linking, mutable
global state, scope escalation, duplicate parameters and oversized provider
documents. Apple signing evidence MUST cover the exact deterministic header and
claim bytes, fixed audience, injected issued-at, checked expiry at one second
and 15,777,000 seconds, rejection above 15,777,000 seconds, zero/noncanonical IDs and clock values,
non-P-256 keys, signer/envelope failure, cancellation before and during signing,
zero result on every failure, JOSE signature verification with the matching
public key, absence of any HTTP/OpenAPI registration, and redaction of the JWT,
key handle, envelope and all signer errors. Secrets/tokens and raw profiles MUST
be redacted. Evidence MUST also prove handle construction bounds and redaction,
successful callback use, nil-callback rejection, copy zeroization after success
and failure, and no alternate reveal or serialization path.

## Evidence, maintenance, and blockers

Every profile MUST pin official documentation or metadata revision, reviewed
date, fixture checksums and expected deviations. It MUST have deterministic
contract fixtures and current documented sandbox/live interoperability for
authorization, identity mapping and refresh/revocation features claimed.
Unavailable provider features MUST be explicitly unsupported. One successful
generic provider does not verify the catalog.

The package worker MUST replace each matrix row's `pin-required` evidence with
manifest-attributable official provider documentation or metadata and MUST
replace `not-run` only with an attributable provider-specific fixture,
sandbox, or live result. The pinned Better Auth 1.6.27 source locator records
the upstream configuration fact being dispositioned; it is not a substitute
for official protocol evidence or interoperability.

Profile decisions and fixtures MUST track
[`PROTOCOL_BASELINES.md`](../PROTOCOL_BASELINES.md), while deployer-supplied
tenant endpoints, credentials, scopes and feature choices MUST use the exact
schema and safe defaults in
[`REFERENCE_CONFIGURATION.md`](../REFERENCE_CONFIGURATION.md).
Safe provider enumeration MUST implement the public operation and response
boundary in [`API_OPERATIONS.md`](../API_OPERATIONS.md).

Exact coverage/mutation, race, discovery/claims fuzz, benchmark,
clean-consumer, API baseline, docs with a provider support matrix and update
procedure, changelog and supply-chain gates MUST pass. The unit MUST remain
unverified while any required profile is missing, stale, silently approximate,
unproved, or maps an unverified identifier as trusted.
