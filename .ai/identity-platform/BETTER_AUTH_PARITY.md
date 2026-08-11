# Better Auth Capability Baseline

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Pinned comparison

The comparison baseline is Better Auth commit
[`b8077b74ef9a80a7757220b72834349bd8de05c0`](https://github.com/better-auth/better-auth/commit/b8077b74ef9a80a7757220b72834349bd8de05c0).
Inputs are its pinned
[plugin index](https://github.com/better-auth/better-auth/tree/b8077b74ef9a80a7757220b72834349bd8de05c0/docs/content/docs/plugins),
[core authentication docs](https://github.com/better-auth/better-auth/tree/b8077b74ef9a80a7757220b72834349bd8de05c0/docs/content/docs/authentication),
and provider catalog at that revision.

Parity means equivalent backend capability and security semantics, not API
shape, JavaScript framework breadth, UI, or database-adapter count. Every
in-scope row MUST be complete through the owning package's public contract and
the composed `identity/http` surface. A primitive alone is not parity when the
baseline supplies a usable workflow.

## Core identity and authentication

| Capability | Scope | Owner(s) | Required acceptance | Verification boundary |
| --- | --- | --- | --- | --- |
| User/account lifecycle | In | `identity`, `identity/postgres` | Create, retrieve, update, suspend, restore, anonymize, delete, link/unlink accounts, canonicalize identifiers, and enforce uniqueness atomically. | Contract tests plus real PostgreSQL races, migration, rollback, and recovery. |
| Email/password | In | `identity/email`, `identity/password`, `identity/risk/hibp` | Signup, signin, signout, email verification, forgot/reset/change password, reauthentication, hashing upgrade, enumeration resistance, and breached-password policy. | Full HTTP journeys, delivery capture, replay/race cases, PostgreSQL state, HIBP protocol proof. |
| Username | In | `identity/username` | Normalized unique username and separate display username; signup, signin, update, availability, custom validation, and bounded min/max length. | Unit/property tests, concurrent persistence collisions, HTTP journeys. |
| Magic link | In | `identity/magiclink` | Request and single-use signin/verification links with bound purpose, expiry, redirect, replay denial, and enumeration-safe response. | Delivery capture, concurrent consume, HTTP callback journey. |
| Email OTP | In | `identity/otp` | Bounded send, verify, signin, verification and reset purposes with attempt limits, expiry, replacement and replay denial. | Deterministic clock/random tests, delivery capture, HTTP journeys. |
| Phone number | In | `identity/phone` | Add/change/verify phone, OTP signin, canonical E.164 handling, uniqueness, rate limits and safe account recovery rules. | Pinned numbering fixtures, delivery adapter proof, HTTP journeys. |
| Anonymous users | In | `identity/anonymous` | Create constrained anonymous identity/session, link to permanent account once, resolve collisions, and clean up abandoned records. | Race and failure-recovery tests plus HTTP upgrade journey. |
| Multi-factor authentication | In | `identity/mfa` | TOTP/OTP enrollment and challenge, recovery codes, trusted devices, step-up, factor replacement/removal and lockout recovery. | Pinned TOTP vectors, recovery/replay/race tests, complete HTTP enrollment and signin. |
| Passkeys | In | `passkey`, `webauthn` | Discoverable credential signup/signin, usernameless flow, credential management and authenticator policy. | Official WebAuthn fixtures, independent implementation interop, browser ceremony profile. |
| Two-factor security keys | In | `webauthn`, `identity/mfa` | Non-discoverable WebAuthn second factor with RP/origin/challenge and counter policy. | Official fixtures, hostile parser/fuzz proof, independent interop. |
| Social OAuth / generic OAuth | In | `identity/oauth`, `identity/oauth/providers` | Authorization code plus PKCE, state/nonce/redirect binding, token storage/refresh, verified linking/unlinking and configurable generic providers. | Protocol fixtures, hostile callbacks, and real/sandbox provider evidence for every declared built-in profile. |
| Google One Tap | In | `identity/oauth/onetap` | Prompt and button modes, ID-token validation, authorized origins, redirect/callback handling, account linking and session issuance. | Google-pinned fixtures plus documented live/sandbox browser evidence. |
| Multiple sessions | In | `identity/session` | Multiple accounts in one client, bounded maximum sessions, list, select active, switch, revoke one/all, and consistent cookies. | Concurrent store tests and complete multi-account HTTP journey. |
| Last login method | In | `identity/session` | Track, get, check, and clear the last successful method with database/cookie/cross-domain and privacy policy. | Persistence and cookie tests, consent/deletion behavior, HTTP journey. |

## Administration and organizations

| Capability | Scope | Owner(s) | Required acceptance | Verification boundary |
| --- | --- | --- | --- | --- |
| Admin | In | `identity`, `identity/session`, `identity/impersonation`, `authorization`, `identity/http` | List/search users, status/ban with reason and expiry, role/permission decisions, create/update/delete users, set credentials, revoke sessions, and audit every privileged action. | Authorization matrix, denial/cross-tenant tests, HTTP administration journey and audit inspection. |
| Impersonation | In | `identity/impersonation` | Explicit authorization, reason, bounded duration, actor chain, visible session marker, stop/expiry, non-escalation and immutable audit. | Policy/race tests and HTTP start/use/stop journey. |
| Organizations | In | `organization`, `organization/postgres` | Create/update/delete organizations; active organization; members, roles, permissions, invitations, teams, ownership transfer and limits. | Real PostgreSQL invariant/race tests and complete HTTP lifecycle. |
| Enterprise SSO | In | `sso`, `sso/oidc`, `sso/oauth2`, `sso/saml`, `sso/postgres` | Domain-discovered organization SSO, provider registration, OIDC/OAuth/SAML login, JIT provisioning, linking, mappings and replay protection. | Official fixtures, independent IdP interoperability, PostgreSQL lifecycle and HTTP journey. |
| SCIM 2.0 | In | `scim`, `scim/organization`, `scim/postgres` | Users/groups, service-provider configuration, schemas/resource types, filter, pagination, PATCH, bulk limits, ETags, bearer auth and organization mapping. | RFC fixtures, independent SCIM client interop, PostgreSQL reconciliation and HTTP conformance. |

## Tokens, APIs, and authorization server

| Capability | Scope | Owner(s) | Required acceptance | Verification boundary |
| --- | --- | --- | --- | --- |
| API keys | In | `identity/apikey` | User/organization-owned create, prefix lookup, list, rename, scopes, expiry, rotate and revoke; reveal secret once and digest at rest. | Race/replay/redaction tests, persistence proof, HTTP lifecycle. |
| Bearer/JWT validation | In | `authentication`, `authentication/jwt` | Strict bearer and JWT/JWK validation remains reusable and validation-only. | Existing primitive gates plus composed endpoint tests. |
| One-time tokens | In | `capability` plus each workflow | Signed, purpose-bound, expiring, revocable, atomically single-use mechanics with workflow-specific state transitions. | Capability gates plus concurrent consumption in every owning workflow. |
| OAuth proxy | In | `identity/oauth/proxy` | Preview/development callback proxy with encrypted profile return, shared-secret binding, no production user/session writes, and replay/state protection. | Two-origin integration profile, tamper/replay tests, no-write assertions. |
| OAuth 2.1 provider | In | `oauth-server`, `oauth-server/postgres` | Client lifecycle, authorization code plus PKCE, refresh rotation/reuse detection, consent, revocation/introspection and discovery metadata. | RFC/OAuth security BCP fixtures, independent client interop, PostgreSQL races, HTTP conformance. |
| OIDC provider | In | `oauth-server/oidc` | ID tokens, UserInfo, discovery, JWKS/key rotation, nonce, claims, subject policy and logout-related metadata explicitly supported. | OpenID conformance-compatible fixtures and independent relying party interop. |
| Device authorization | In | `oauth-server/device` | Device/user codes, verification UI contract, polling interval/slow_down, expiry, denial and token issuance. | RFC 8628 vectors, concurrent polling and complete HTTP device journey. |

## Security, transport, and developer experience

| Capability | Scope | Owner(s) | Required acceptance | Verification boundary |
| --- | --- | --- | --- | --- |
| CAPTCHA | In | `identity/risk/captcha` and four adapters | Provider-neutral decision plus reCAPTCHA, Turnstile, hCaptcha and CaptchaFox verification with action/hostname/score policy and explicit provider-failure behavior. | Official provider fixtures and documented sandbox/live interoperability per adapter. |
| Pwned Passwords | In | `identity/risk/hibp` | HIBP range k-anonymity, padding, bounded parsing/caching and no full password/hash disclosure. | Protocol fixtures and documented HIBP interoperability. |
| Auth abuse/risk | In | `identity/risk`, persistence adapters | Action-specific rate/velocity/replay/lockout/step-up decisions with explicit fail policy and atomic state. | Deterministic policy tests, PostgreSQL/Valkey failure and race profiles. |
| Localization | In | `identity/i18n` | Localized stable errors while retaining original message/code; locale detection by explicit value, session, cookie, Accept-Language and fallback chain. | Pinned language fixtures, parser fuzzing, HTTP negotiation/privacy tests. |
| OpenAPI | In | `identity/http` | OpenAPI 3.1.1 describes every core and feature endpoint, security scheme, request/response/error schema and plugin-composed route. | Schema validation, operation-to-handler completeness and generated-client smoke test. |
| Test utilities | In | `identity/identitytest` | Test-only factories, DB lifecycle, auth/session helpers, OTP capture, deterministic clock/randomness and denial of production exposure. | Clean-consumer examples, parallel isolation and build/route exclusion tests. |

## Explicitly excluded baseline capabilities

| Capability | Status | Rationale |
| --- | --- | --- |
| Billing and payment plugins | Excluded | Commercial integration scope is independent of identity correctness. |
| SIWE | Excluded | Wallet-signature identity is not a selected product profile. |
| MCP authentication | Excluded | No current product requirement beyond standard OAuth/OIDC capabilities. |
| Agent authentication | Excluded | No current autonomous-agent credential product requirement. |
| Lead tracking or analytics integrations | Excluded | Analytics is not an identity ownership boundary. |
| JavaScript framework clients | Excluded | This program delivers Go backend contracts and standard HTTP/OpenAPI surfaces. |
| Additional database engines | Excluded | Selected durable profiles are PostgreSQL and Valkey; capability parity does not require adapter-count parity. |

## Built-in social provider baseline

`identity/oauth/providers` MUST provide pinned, independently tested profiles
for: Apple, Atlassian, Amazon Cognito, Discord, Dropbox, Facebook, Figma,
GitHub, GitLab, Google, Hugging Face, Kakao, Kick, LINE, Linear, LinkedIn,
Microsoft, Naver, Notion, Paybin, PayPal, Polar, Railway, Reddit, Roblox,
Salesforce, Slack, Spotify, TikTok, Twitch, Twitter/X, Vercel, VK, WeChat, and
Zoom. Every profile MUST define endpoints, discovery policy, scopes, PKCE,
client-auth method, claims mapping, issuer/audience rules, refresh/revocation
behavior, quirks, and pinned evidence. A profile with unavailable current
interoperability evidence remains unverified; it MUST NOT silently fall back to
generic assumptions.
