# Better Auth Capability Baseline

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Pinned comparison

The comparison baseline is Better Auth commit
[`b8077b74ef9a80a7757220b72834349bd8de05c0`](https://github.com/better-auth/better-auth/commit/b8077b74ef9a80a7757220b72834349bd8de05c0).
Inputs are its pinned
[plugin index](https://github.com/better-auth/better-auth/tree/b8077b74ef9a80a7757220b72834349bd8de05c0/docs/content/docs/plugins),
[core authentication docs](https://github.com/better-auth/better-auth/tree/b8077b74ef9a80a7757220b72834349bd8de05c0/docs/content/docs/authentication),
[core concepts](https://github.com/better-auth/better-auth/tree/b8077b74ef9a80a7757220b72834349bd8de05c0/docs/content/docs/concepts),
[exported plugin source](https://github.com/better-auth/better-auth/blob/b8077b74ef9a80a7757220b72834349bd8de05c0/packages/better-auth/src/plugins/index.ts),
and provider catalog at that revision. Source-exported `access`,
`custom-session`, and `oauth-popup` capabilities are included even where the
documentation index does not give them a dedicated page. Internal
`additional-fields` behavior is covered through typed identity/session fields.

`UPSTREAM_DISPOSITIONS.md` closes the exact official documentation, export,
package, and provider surfaces. `API_OPERATIONS.md` is the canonical operation
baseline. A broad capability row below MUST NOT be used to waive or infer an
operation absent from that catalog.

`PARITY_DISPOSITIONS.json` is the machine-readable closure authority for
divergences, exclusions, provider credential modes and ownership
reclassifications. Every record MUST retain its stable ID, owner, proving
artifact and semantic digest; prose labels alone are not a closed disposition.

Parity means equivalent backend capability and security semantics, not API
shape, JavaScript framework breadth, UI, or database-adapter count. Every
in-scope row MUST be complete through the owning package's public contract and
the composed `identity/http` surface. A primitive alone is not parity when the
baseline supplies a usable workflow.

## Core identity and authentication

| Capability | Scope | Owner(s) | Required acceptance | Verification boundary |
| --- | --- | --- | --- | --- |
| User/account lifecycle | In | `identity`, `identity/postgres` | Create, retrieve, update, suspend, restore, anonymize, delete, link/unlink accounts, canonicalize identifiers, and enforce uniqueness atomically. | Contract tests plus real PostgreSQL races, migration, rollback, and recovery. |
| Self-service profile | In | `identity`, `identity/session`, `identity/http` | An authenticated subject can retrieve and update only name, image and typed input-enabled profile fields; email, status, role and sensitive fields follow their dedicated authorized workflows. | Self-service HTTP journey, field-policy matrix, cache/session invalidation and cross-tenant denial. |
| Verified account deletion | In | `identity`, `identity/email`, `identity/delivery`, `identity/session`, `identity/risk`, `capability`, `identity/http` | Delete by password or fresh proof, or request and consume an expiring single-use email confirmation; bind current subject, session/version and callback; revoke sessions and run ordered hooks without consumer orchestration. | Delivery capture, password/fresh/token paths, concurrent callback race, redirect denial, PostgreSQL capability consumption and cleanup recovery. |
| Email/password | In | `identity/email`, `identity/password`, `identity/password/postgres`, `identity/risk/hibp` | Signup, signin, signout, email verification, self/admin address list/get/remove lifecycle, forgot/reset/change password, reauthentication, hashing upgrade, enumeration resistance, and breached-password policy. | Full HTTP journeys, self/admin address lifecycle and last-path denial, delivery capture, replay/race cases, PostgreSQL state, HIBP protocol proof. |
| Reauthentication proof | In | `identity/password`, `identity/session`, `identity/risk` | Verify the current password under a purpose/audience-bound, rate-limited contract that returns a short-lived proof, never a new session, and can gate sensitive operations. | Success/denial/enumeration/timing tests, proof expiry/replay/cross-purpose denial and HTTP/direct operation proof. |
| Username | In | `identity/username` | Normalized unique username and separate display username; signup, signin, update, availability, custom validation, and bounded min/max length. | Unit/property tests, concurrent persistence collisions, HTTP journeys. |
| Magic link | In | `identity/magiclink` | Request and single-use signin/verification links with bound purpose, expiry, redirect, replay denial, and enumeration-safe response. | Delivery capture, concurrent consume, HTTP callback journey. |
| Email OTP | In | `identity/otp`, `identity/otp/postgres` | Bounded send, verify, signin, verification and reset purposes with attempt limits, expiry, replacement and replay denial. | Deterministic clock/random tests, delivery capture, PostgreSQL races, HTTP journeys. |
| Phone number | In | `identity/phone`, `identity/password` | Add/change/verify phone, distinct OTP and password signin, canonical E.164 handling, tenant credential lookup, remember/MFA/risk/enumeration behavior, uniqueness, rate limits and safe account recovery rules. | Pinned numbering fixtures, OTP/password negative-equivalence tests, delivery adapter proof, HTTP journeys. |
| Anonymous users | In | `identity/anonymous` | Create constrained anonymous identity/session, let the authenticated anonymous subject delete its own identity without permanent-account proof, link to a permanent account once, resolve collisions, and clean up abandoned records. | Self-delete, race and failure-recovery tests plus HTTP creation/upgrade journeys. |
| Multi-factor authentication | In | `identity/mfa`, `identity/mfa/postgres` | TOTP/OTP enrollment and challenge, recovery codes shown only at creation/full regeneration and never re-viewed, trusted devices, step-up, factor replacement/removal, lockout recovery, and independently approved administrator reset followed by user-only recovery delivery. | Pinned TOTP vectors, plaintext/admin-secret denial, PostgreSQL recovery/replay/race tests, complete HTTP enrollment/signin/admin-recovery journey. |
| Passkeys | In | `passkey`, `passkey/postgres`, `webauthn`, `webauthn/postgres` | Discoverable credential signup/signin, usernameless flow, credential management and authenticator policy. | Official WebAuthn fixtures, durable credential races, independent implementation interop, browser ceremony profile. |
| Two-factor security keys | In | `webauthn`, `webauthn/postgres`, `identity/mfa`, `identity/mfa/postgres` | Non-discoverable WebAuthn second factor with RP/origin/challenge, durable credential and counter policy. | Official fixtures, PostgreSQL ceremony/counter races, hostile parser/fuzz proof, independent interop. |
| Social OAuth / generic OAuth | In | `identity/oauth`, `identity/oauth/postgres`, `identity/oauth/providers` | Authorization code plus PKCE; state/nonce/provider/client/redirect binding; explicit signin-versus-signup and profile-sync policy; typed success/new-user/error/popup/URL-return results; encrypted durable token storage/refresh; verified linking/unlinking; and configurable generic providers. | Protocol fixtures, redirect/result and profile-sync tests, PostgreSQL races/key rotation, hostile callbacks, and real/sandbox provider evidence for every declared built-in profile. |
| Native/provider-token social signin | In | `identity/oauth`, `identity/oauth/postgres`, `identity/oauth/providers`, `identity/http` | Accept only the per-provider native-token modes selected in `CONFIGURATION_CATALOGS.json`; ID-token modes bind issuer, audience/authorized party, nonce, platform client and subject, while opaque-access-token modes require bounded provider introspection/UserInfo and token-substitution denial; persist optional provider tokens safely, apply signup/linking policy and issue the normal session result. Unsupported provider/mode pairs MUST fail before network or identity mutation. | Apple, Google, Facebook and LINE pinned fixtures plus current native/provider evidence, every configured mode, audience/nonce substitution tests, account collision tests and complete HTTP journey. |
| Generic OAuth configuration | In | `identity/oauth`, `identity/oauth/providers` | Explicitly support or reject discovery/static endpoints, RFC 9207 issuer strictness, response type/mode, prompt/access type, PKCE, expiry fallback, custom token exchange/UserInfo/profile mapping, endpoint headers/parameters, client authentication, signup and profile-sync policies. | Checked option matrix, construction failures, hostile discovery/callback tests and generic-provider interoperability. |
| Google One Tap | In | `identity/oauth/onetap` | Prompt and button modes, ID-token validation, authorized origins, redirect/callback handling, account linking and session issuance. | Google-pinned fixtures plus documented live/sandbox browser evidence. |
| Multiple sessions | In | `identity/session` | Multiple accounts in one client, bounded maximum sessions, list, select active, switch, revoke one/all, and consistent cookies. | Concurrent store tests and complete multi-account HTTP journey. |
| Last login method | In | `identity/session` | Track, get, check, and clear the last successful method with database/cookie/cross-domain and privacy policy. | Persistence and cookie tests, consent/deletion behavior, HTTP journey. |
| Stateful, cached, and stateless sessions | In | `identity/session`, `identity/session/postgres`, `identity/session/valkey` | Select opaque durable sessions, secondary-storage sessions, bounded cookie cache, or signed/encrypted stateless sessions; version and globally invalidate stateless state; preserve explicit refresh/freshness semantics. | Cross-profile contract suite, PostgreSQL/Valkey failure tests, cookie tamper/version/revocation journeys. |
| Persistent and browser-session issuance | In | `identity/session`, every signin owner, `identity/http` | Every session-issuing signup/signin flow accepts the selected remember policy; a non-remembered session uses a non-persistent browser cookie and preserves that choice through MFA/OTP continuations without gaining lifetime. | Password, username, phone, OTP, social and MFA continuation journeys; cookie persistence, refresh, rotation and browser-session expiry tests. |
| Custom session enrichment | In | `identity/session`, `identity/http` | Derive typed response fields from identity/session context with explicit cacheability, invalidation, redaction, failure and latency policy without mutating authenticated principal truth. | Enricher contract, cache invalidation, timeout/error, OpenAPI schema and HTTP response tests. |
| Typed additional fields | In | `identity`, `identity/postgres`, `identity/http` | Declare typed user/account/session fields with defaults, validation, input/output/write permissions, sensitivity, migration and API-schema behavior. | Schema/property tests, PostgreSQL migration/round-trip, authorization/redaction and OpenAPI tests. |

## Administration and organizations

| Capability | Scope | Owner(s) | Required acceptance | Verification boundary |
| --- | --- | --- | --- | --- |
| Admin | In | `identity`, `identity/session`, `identity/mfa`, `identity/impersonation`, `authorization`, `identity/reference`, `identity/http` | Persist versioned platform-administrator roles and subject assignments through the authoritative authorization adapter; compose one-time bootstrap and later authorized assignment changes with identity state and immutable audit; list/search users, status/ban, role/permission decisions, delete/anonymize, credentials, email-address lifecycle, sessions, independently approved MFA reset/recovery and impersonation. | Bootstrap/assignment and MFA-reset transaction/recovery races, authorization matrix, denial/cross-tenant tests, deletion cascades, HTTP administration journey and audit inspection. |
| Impersonation | In | `identity/impersonation`, `identity/impersonation/postgres` | Explicit authorization, reason, bounded duration, actor chain, visible session marker, stop/expiry, non-escalation and immutable audit. | Policy/race tests, PostgreSQL lifecycle and HTTP start/use/stop journey. |
| Organizations | In | `organization`, `organization/postgres` | Create/update/delete organizations; active organization; members, roles, permissions, invitations, teams, ownership transfer and limits. | Real PostgreSQL invariant/race tests and complete HTTP lifecycle. |
| Enterprise SSO | In | `sso`, `sso/oidc`, `sso/oauth2`, `sso/saml`, `sso/postgres` | Domain-discovered organization SSO, provider registration, OIDC/OAuth/SAML login, SAML SP- and IdP-initiated single logout, JIT provisioning, linking, mappings, replay protection, and versioned repeat-login synchronization of authoritative identity/membership/role claims without overwriting application-owned fields. | Official fixtures, repeat-login update/removal/conflict/replay races, independent IdP login/logout interoperability, PostgreSQL lifecycle and HTTP journey. |
| SCIM 2.0 | In | `scim`, `scim/organization`, `scim/postgres` | Users/groups, discovery, filter, pagination, PATCH, bulk limits, ETags, bearer auth and organization mapping; every connection is organization/provider owned and the personal-connection deployment profile is not selected. | RFC fixtures, personal-connection rejection/migration disposition, independent SCIM client interop, PostgreSQL reconciliation and HTTP conformance. |

## Tokens, APIs, and authorization server

| Capability | Scope | Owner(s) | Required acceptance | Verification boundary |
| --- | --- | --- | --- | --- |
| API keys | In | `identity/apikey`, `identity/apikey/postgres`, `identity/apikey/valkey`, `identity/session`, `identity/http` | User/organization-owned create, prefix lookup, list, rename, scopes, metadata, quota/refill, expiry, rotate and revoke; reveal secret once, digest at rest, support declared database/secondary-storage fallback profiles, and optionally authenticate a request as a session-compatible user principal with one verification/quota debit and no durable session. | Race/replay/redaction tests, PostgreSQL and Valkey quota/rotation/fallback proof, disabled/enabled header and revocation propagation, HTTP lifecycle. |
| Session bearer authentication | In | `identity/session`, `identity/http`, `authentication` | Accept a session bearer credential through `Authorization` without cookie fallback, preserve session expiry/revocation/freshness and return the same principal under explicit CSRF/cache policy. | Existing authentication validation gates plus cookie-versus-bearer, revocation and cross-origin HTTP tests. |
| Session bearer acquisition | In | `identity/session`, `identity/http` | A successful explicitly selected bearer profile returns the session bearer once through a typed response body or CORS-exposed response header, never a URL; rotation replaces rather than duplicates the credential. | Signin/refresh/rotation HTTP tests, header exposure and cache tests, URL/log redaction, cookie-versus-bearer isolation. |
| JWT issuance and JWKS | In | `oauth-server/oidc`, `authentication/jwt` | `oauth-server/oidc` owns issuance policy, claims, key selection and published JWKS. `authentication/jwt` owns validation only. Optional remote signing is a typed `oauth-server/oidc.Signer` adapter; optional hosted JWKS retrieval is a typed `authentication/jwt.KeySource` adapter. Neither profile changes ownership or permits hidden HTTP callbacks. | Independent verifier and relying-party tests, local and remote signing, hosted-JWKS cache/outage/rotation, session-exchange and OAuth token journeys. |
| One-time workflow tokens | In | `capability` plus each workflow | Signed, purpose-bound, expiring, revocable, atomically single-use mechanics with workflow-specific state transitions. | Capability gates plus concurrent consumption in every owning workflow. |
| One-time session transfer | In | `identity/session`, `identity/http`, `capability` | A fresh authenticated session can issue a digest-stored, three-minute, single-use transfer token; another trusted origin can consume it once and receive the same bounded session under explicit cookie/no-cookie policy, origin/audience binding and revocation checks. | Concurrent generate/consume/replay/revoke tests plus a two-origin HTTP journey. |
| OAuth proxy | In | `identity/oauth/proxy` | Preview/development callback proxy with encrypted profile return, shared-secret binding, no production user/session writes, and replay/state protection. | Two-origin integration profile, tamper/replay tests, no-write assertions. |
| OAuth popup | In | `identity/oauth`, `identity/http` | Complete social signin/linking in a popup with exact opener-origin binding, one-time result delivery, cancellation/closure/error semantics, and no wildcard `postMessage`; framework client code remains excluded. | Real-browser popup journey, origin-confusion/tamper tests and normal redirect equivalence. |
| OAuth 2.1 provider | In | `oauth-server`, `oauth-server/postgres` | Immutable user/organization/reference-owned client lifecycle; authorization code plus PKCE and client credentials; tamper-evident expiring login/account/consent continuation; refresh rotation/reuse detection; consent; revocation/introspection; and exact discovery metadata. | RFC/OAuth security BCP fixtures, continuation injection/replay/cross-client denial, owner-scoped CRUD races, independent client interop, PostgreSQL races and HTTP conformance. |
| OAuth protected resources | In | `oauth-server`, `oauth-server/oidc`, `identity/http` | Publish standards-generic protected-resource metadata and validate JWT/JWKS or introspected access tokens with exact issuer, audience, scope and resource binding. MCP-specific adapters remain excluded. | Independent resource-server proof, metadata/discovery linkage, JWT/introspection outage and confused-deputy tests. |
| OIDC provider | In | `oauth-server/oidc` | ID tokens, UserInfo, discovery, JWKS/key rotation, nonce, claims, subject policy and logout-related metadata explicitly supported. | OpenID conformance-compatible fixtures and independent relying party interop. |
| Device authorization | In | `oauth-server/device`, `oauth-server/postgres` | Device/user codes, verification UI contract, polling interval/slow_down, expiry, denial and token issuance. | RFC 8628 vectors, PostgreSQL races, concurrent polling and complete HTTP device journey. |

## Security, transport, and developer experience

| Capability | Scope | Owner(s) | Required acceptance | Verification boundary |
| --- | --- | --- | --- | --- |
| CAPTCHA | In | `identity/risk/captcha`, `identity/risk/captcha/recaptcha`, `identity/risk/captcha/turnstile`, `identity/risk/captcha/hcaptcha`, `identity/risk/captcha/captchafox` | Provider-neutral decision plus reCAPTCHA, Turnstile, hCaptcha and CaptchaFox verification with action/hostname/score policy and explicit provider-failure behavior. Each adapter owns exactly its named provider contract and evidence. | Official provider fixtures and documented sandbox/live interoperability per adapter. |
| Pwned Passwords | In | `identity/risk/hibp` | HIBP range k-anonymity, padding, bounded parsing/caching and no full password/hash disclosure. | Protocol fixtures and documented HIBP interoperability. |
| Auth abuse/risk | In | `identity/risk`, persistence adapters | Action-specific rate/velocity/replay/lockout/step-up decisions with explicit fail policy and atomic state. | Deterministic policy tests, PostgreSQL/Valkey failure and race profiles. |
| Core HTTP rate limiting | In | `identity/http`, `identity/reference` | Default and per-route rules, trusted-client IP derivation, IPv4-mapped IPv6 normalization, full canonical RFC 5952 IPv6 addresses without subnet aggregation, Retry-After, atomic distributed storage and fail policy apply to client HTTP calls without weakening domain risk controls; subnet aggregation belongs only to a future, separately selected profile. | Route contract tests plus PostgreSQL/Valkey concurrency, proxy-spoofing, expiry and outage journeys. |
| Access-control composition | In | `authorization`, `identity/http`, `organization` | Reusable role/permission statements and explicit checks for admin and organization operations; authentication never implies authorization. | Complete allow/deny matrix, custom-role tests and cross-tenant HTTP denial proof. |
| Hooks and request extension | In | `identity`, `identity/http` | Ordered before/after hooks can validate, enrich or observe bounded operations with explicit transaction, cancellation, response and failure semantics; no hidden global registration or fire-and-forget work. | Ordering/reentrancy/error tests, transaction-boundary proof and HTTP integration. |
| Typed extension modules | In | `identity/http`, `identity/reference` | Consumers can compose typed endpoints, schemas/migration contributors, route middleware, request/response hooks, per-route rate rules and contributed trusted origins through explicit immutable contracts with authorization, collision and lifecycle validation. | Clean external extension module, migration rehearsal, handler/OpenAPI completeness, origin/rate-policy and middleware-order tests. |
| Schema and migration operations | In | `identity/reference` and PostgreSQL adapters | Validate migration compatibility, enumerate an ordered plan, apply owned forward migrations safely and report current/required versions without copying adapter SQL or relying on hidden CLI state. | Empty/populated/mixed-version rehearsal, interruption/resume and clean deployment example. |
| Dynamic base URL and trusted origins | In | `identity/http`, `identity/reference` | Resolve one externally visible issuer/base URL from explicit configuration or a strictly trusted proxy/host profile; support browser-extension and preview profiles without arbitrary-host reflection. | Proxy/host/origin confusion tests, browser-extension and preview callback journeys. |
| Safe instrumentation | In | `identity/reference`, `audit`, `telemetry` | Emit bounded metrics/traces/audit for lifecycle and provider outcomes with opt-in exporters, controlled cardinality and no secrets/PII; expose permissioned/redacted audit get/search/list/export with immutable access records; instrumentation failure cannot alter auth decisions. | Redaction/cardinality tests, audit investigation/export authorization, exporter outage/shutdown and end-state trace correlation. |
| Delivery callbacks and templates | In | `identity/delivery`, `identity/reference` | Versioned email/SMS intents and templates compose with a documented bounded application-supplied Sender callback, idempotent queueing, retry/outcome classification and capture testing; no workflow requires consumer-written orchestration. | Sender contract/failure tests, template safety, capture journeys and clean consumer with a real bounded callback implementation. |
| Localization | In | `identity/i18n` | Localized stable errors while retaining original message/code; locale detection by explicit value, session, cookie, Accept-Language and fallback chain. | Pinned language fixtures, parser fuzzing, HTTP negotiation/privacy tests. |
| OpenAPI | In | `identity/http` | OpenAPI 3.1.1 describes every core and feature endpoint, security scheme, request/response/error schema and plugin-composed route. | Schema validation, operation-to-handler completeness and generated-client smoke test. |
| Test utilities | In | `identity/identitytest` | Test-only user/organization factories and DB lifecycle; login, auth-header, signed-cookie and cookie-header helpers; instance-isolated OTP capture/get/clear; a reusable store-conformance harness for CRUD, auth flow, identifier casing, joins, ID types and transactions; deterministic clock/randomness; and denial of production exposure. | Clean-consumer examples, production-cookie compatibility, OTP cross-instance denial, adapter-suite mutant proof, parallel isolation and build/route exclusion tests. |
| Operational tooling | In | `identity/reference` | Through direct Go APIs, enumerate/generate selected schemas, plan/apply migrations, generate cryptographic configuration secrets and emit a bounded fully redacted diagnostic summary. These contracts retain the backend outcomes exposed by relevant upstream tooling without selecting a CLI product. JavaScript command wiring, project initialization, source rewriting, prompts, dependency installation/upgrading, hosted-account login, AI and MCP commands are excluded. | Direct clean-consumer calls, empty/populated migration rehearsals, interruption/resume, entropy tests and golden redaction tests with secret-shaped inputs; no CLI invocation is acceptance evidence. |

## Explicit baseline dispositions outside the selected backend capability set

| Capability | Status | Rationale |
| --- | --- | --- |
| Billing and payment plugins | Excluded | Commercial integration scope is independent of identity correctness. |
| SIWE | Excluded | Wallet-signature identity is not a selected product profile. |
| MCP authentication | Excluded | No current product requirement beyond standard OAuth/OIDC capabilities. |
| Agent authentication | Excluded | No current autonomous-agent credential product requirement. |
| Lead tracking or analytics integrations | Non-capability integration | Analytics is not backend identity behavior; no Dub integration is added. |
| JavaScript framework clients | Client surface | This program delivers Go backend contracts and standard HTTP/OpenAPI surfaces. |
| Additional database engines | Deployment-profile divergence | Selected durable profiles are PostgreSQL and Valkey; capability parity does not require adapter-count parity. |
| JavaScript project-management CLI | Non-capability tooling | Command wiring, framework scaffolding, project initialization, source rewriting, prompts and package-manager installation/upgrades are not Go backend identity capabilities. Importable schema generation plus migration, secret and redacted diagnostic outcomes remain in scope only as direct `identity/reference` APIs, not as a CLI-derived product surface. |
| Community plugin catalog | Non-capability catalog | Third-party navigation/catalog breadth is not an official backend capability; the typed extension-module contract remains in scope. |
| Personal SCIM connections | Deployment-profile divergence | The selected provisioning deployment profile is organization/provider scoped; personal bearers MUST NOT provision identities outside that explicit boundary. |
| Database-less OAuth provider-token cookies | Security divergence | OAuth transaction state and recoverable provider tokens use the selected durable PostgreSQL adapters; stateless sessions do not imply cookie-owned provider-token storage. |
| Backup-code plaintext re-view | Superseded | Recovery codes are shown once and digest-stored; a fresh reauthenticated user regenerates the entire set instead of retrieving plaintext. |

The exact item-by-item rationale and standards capabilities retained despite
the MCP/agent exclusions are normative in `UPSTREAM_DISPOSITIONS.md`.

## Exported plugin disposition

The pinned `packages/better-auth/src/plugins/index.ts` export surface is closed
by this table. Validator changes MUST update this mapping when the pinned
source changes.

| Source export | Disposition |
| --- | --- |
| `access` | Access-control composition |
| `admin` | Admin and impersonation |
| `anonymous` | Anonymous users |
| `bearer` | Session bearer authentication |
| `captcha` | CAPTCHA |
| `custom-session` | Custom session enrichment |
| `device-authorization` | Device authorization |
| `email-otp` | Email OTP |
| `generic-oauth` | Social OAuth / generic OAuth |
| `haveibeenpwned` | Pwned Passwords |
| `jwt` | JWT issuance and JWKS, including required session exchange and optional bounded remote-signer/JWKS profiles |
| `last-login-method` | Last login method |
| `magic-link` | Magic link |
| `mcp` | Explicitly excluded MCP authentication |
| `multi-session` | Multiple sessions |
| `oauth-popup` | OAuth popup |
| `oauth-proxy` | OAuth proxy |
| `oidc-provider` | OIDC provider |
| `one-tap` | Google One Tap |
| `one-time-token` | One-time session transfer |
| `open-api` | OpenAPI |
| `organization` | Organizations |
| `phone-number` | Phone number |
| `siwe` | Explicitly excluded SIWE |
| `test-utils` | Test utilities |
| `two-factor` | Multi-factor authentication and two-factor security keys; plaintext backup-code re-view is deliberately superseded by regeneration |
| `username` | Username |
| `types/plugins` and `hide-metadata` | Type/utility exports, not standalone capabilities |

## Built-in social provider baseline

`identity/oauth/providers` MUST provide pinned, independently tested profiles
for: Apple, Atlassian, Amazon Cognito, Discord, Dropbox, Facebook, Figma,
GitHub, GitLab, Google, Hugging Face, Kakao, Kick, LINE, Linear, LinkedIn,
Microsoft (stable core ID `microsoft`), Naver, Notion, Paybin, PayPal, Polar, Railway, Reddit, Roblox,
Salesforce, Slack, Spotify, TikTok, Twitch, Twitter/X, Vercel, VK, WeChat, and
Zoom. The generic-OAuth helper baseline additionally requires Auth0, Gumroad,
HubSpot, Keycloak, Microsoft Entra ID (default helper ID `microsoft-entra-id`), Okta, Patreon and Yandex; LINE and Slack
are shared with the preceding list. Every profile MUST define endpoints,
discovery policy, scopes, PKCE,
client-auth method, claims mapping, issuer/audience rules, refresh/revocation
behavior, quirks, and pinned evidence. A profile with unavailable current
interoperability evidence remains unverified; it MUST NOT silently fall back to
generic assumptions.
