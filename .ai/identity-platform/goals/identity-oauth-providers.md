# Goal: pkg/identity/oauth/providers

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Execution metadata

- Unit: `identity/oauth/providers`
- Canonical module: `pkg/identity/oauth/providers`
- Canonical goal after scaffolding: `pkg/identity/oauth/providers/.ai/GOAL.md`
- Requires: `identity/oauth`
- Consumes existing primitives: `authentication/oidc`, `http-client`, `secret-envelope`, `telemetry`
- Unlocks after verification: `identity/oauth/onetap`, `identity/http`

## Start gate and objective

The worker MUST satisfy `../COMMON_REQUIREMENTS.md` and MUST start only after
the coordinator marks this unit `in-progress` with `identity/oauth` verified.
Build one maintained built-in catalog of exact OAuth/OIDC provider profiles.
The generic orchestration remains in `identity/oauth`; this package supplies
provider facts, mappings and explicitly reviewed incompatibilities.

## Required provider baseline

Profiles are REQUIRED for Apple, Atlassian, Amazon Cognito, Discord, Dropbox,
Facebook, Figma, GitHub, GitLab, Google, Hugging Face, Kakao, Kick, LINE,
Linear, LinkedIn, Microsoft, Naver, Notion, Paybin, PayPal, Polar, Railway,
Reddit, Roblox, Salesforce, Slack, Spotify, TikTok, Twitch, Twitter/X, Vercel,
VK, WeChat and Zoom. Names and aliases MUST be stable and collision-free.

## Ownership and public contract

Each immutable profile MUST define authorization, token, user-info,
revocation and discovery endpoints as applicable; issuer and audience rules;
default and optional scopes; PKCE support; client authentication method;
authorization/token parameters; ID-token and user-info claims mapping;
stable provider-account ID; email and email-verification semantics; refresh,
expiry and revocation behavior; avatar/name mapping; and documented quirks.
Runtime client secrets, tenant-specific endpoints and requested scopes MUST be
provided explicitly without mutating global profile state.

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

Exact coverage/mutation, race, discovery/claims fuzz, benchmark,
clean-consumer, API baseline, docs with a provider support matrix and update
procedure, changelog and supply-chain gates MUST pass. The unit MUST remain
unverified while any required profile is missing, stale, silently approximate,
unproved, or maps an unverified identifier as trusted.
