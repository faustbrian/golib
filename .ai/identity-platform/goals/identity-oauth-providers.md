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

Every profile mapping MUST identify the stable provider subject field, email
field and verification rule, display name components, username or handle,
avatar URL and size variants, tenant or organization fields, locale and any
provider-specific raw field retained. It MUST declare required versus optional
fields, types, normalization, null/empty handling and whether each field is
authoritative, mutable, security-sensitive or display-only. Unknown claims
MUST NOT flow into identity metadata without a bounded declared extension
mapping.

Apple MUST select `form_post` under the pinned OAuth 2.0 Form Post Response Mode
and bind it into authorization state with the exact HTTPS callback. Every other
provider defaults to `query`; a provider may not inherit, advertise, or accept
`form_post` unless a later named profile adds an exact clause pin, configuration
catalog decision, and interoperability evidence.

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
documents. Secrets/tokens and raw profiles MUST be redacted.

## Evidence, maintenance, and blockers

Every profile MUST pin official documentation or metadata revision, reviewed
date, fixture checksums and expected deviations. It MUST have deterministic
contract fixtures and current documented sandbox/live interoperability for
authorization, identity mapping and refresh/revocation features claimed.
Unavailable provider features MUST be explicitly unsupported. One successful
generic provider does not verify the catalog.

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
