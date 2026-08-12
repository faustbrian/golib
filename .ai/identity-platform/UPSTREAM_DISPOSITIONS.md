# Pinned Better Auth Surface Dispositions

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Baseline and closure rule

This disposition is pinned to Better Auth commit
`b8077b74ef9a80a7757220b72834349bd8de05c0`. It closes the official plugin
documentation tree, the `better-auth/plugins` export surface, official
top-level packages, built-in social providers, and generic OAuth helpers at
that revision.

`UPSTREAM_LEAVES.json` is the authoritative recursive Git-tree and package
export inventory for every declared source. Every physical blob in those trees
MUST have exactly one canonical leaf disposition containing its exact path,
kind, object ID, canonical source, stable exact-disposition ID, classification,
explicit classification-rule ID, semantic summary row, capability IDs and
exact operation IDs. Exact-disposition IDs MUST be unique per physical blob or
declared export; classification-rule IDs MAY group leaves only after each path
has passed the rule's explicit package/path classification. Every declared
package export and command entry point MUST likewise have one exact disposition
bound to the object ID of the pinned `package.json` that declares it. The
validator MUST
recompute every declared tree object from the checked-in recursive entries and
MUST require exact set equality between the proven physical blob set and the
canonical leaf dispositions, exact equality with the exports parsed from every
pinned package manifest, and a unique longest-source owner for overlapping
declared sources. Conceptual package rows below are derived summaries of those
exact blob/export dispositions; they MUST NOT replace exact classifications or
silently assign one blanket classification to a mixed package tree.

For official packages, the exact classifier MUST distinguish package/build
metadata, JavaScript client/framework surfaces, unselected database adapters,
excluded product integrations, backend feature implementations, and mixed
surfaces such as `cli`, `better-auth`, `core`, and `oauth-provider`. An in-scope
blob or export MUST retain non-empty exact operation closure. Unknown packages,
and unresolved exports MUST fail generation. Mixed-package classifiers MUST use
explicit ordered path/export rules, and the validator MUST pin the resulting
exact path-to-rule digest so a changed mixed tree cannot inherit a package
summary without an explicit reviewed digest update.

Every upstream item MUST have exactly one of these dispositions:

- **In:** equivalent backend capability is REQUIRED through the named local
  owners and `API_OPERATIONS.md`.
- **Product exclusion:** adopter-visible backend behavior deliberately outside
  this product boundary for the recorded product reason.
- **Superseded:** the local platform deliberately offers a safer or more
  general contract; the exact divergence and replacement MUST be tested.
- **Non-capability:** documentation, types, metadata, or implementation utility
  that creates no independent adopter-visible backend behavior.
- **Client surface:** framework, reactive-client, or device-client behavior
  that is not a backend identity capability.
- **Deployment-profile divergence:** backend behavior that belongs to a valid
  but unselected deployment profile rather than an omitted product capability.
- **Security divergence:** upstream behavior deliberately replaced by a
  stricter backend security invariant with executable proof.

Broad category exclusions MUST NOT conceal an unreviewed official item.

## Core documentation and route surface

| Pinned core input | Disposition | Operation or local contract closure |
| --- | --- | --- |
| concepts `api` | In | Direct and HTTP calls, typed request/response/errors, headers and response metadata are closed by `API_OPERATIONS.md` and `identity/http`. |
| concepts `cli` | In plus non-capability | The backend outcomes behind schema generation, migration, info and secret become direct `identity.reference.*` APIs. Upstream JavaScript ORM/dialect generator implementations, exports, fixtures and mixed-database tests are deployment-profile divergences and do not prove the local PostgreSQL contract. JavaScript command wiring, project initialization, source rewriting, prompts, dependency installation/upgrading, hosted-account login, AI and MCP commands are excluded tooling or excluded products; no CLI product surface is retained. |
| concepts `client` | Client surface | JavaScript reactive/fetch client behavior is not a backend capability; every backend action remains in the operation catalog and OpenAPI. |
| concepts `cookies` | In | `identity/session`, `identity/http`, reference cookie profiles and browser-session persistence. |
| concepts `database` | In by selected adapters | Core schemas, hooks, migrations and secondary storage are owned by PostgreSQL/Valkey adapters; alternate engines and experimental ORM joins are excluded. |
| concepts `email` | In | Verification, change, reset and delivery operations. |
| concepts `hooks` | In with safer lifecycle | Ordered bounded hooks/outbox replace fire-and-forget background callbacks. |
| concepts `oauth` | In | Redirect, direct-token signin, linking, provider tokens, additional scopes/state and no-email policy. |
| concepts `plugins` | In by typed extension contract | Server endpoints, schemas, middleware, hooks, rate rules and origins; JavaScript client-plugin atoms/fetch plugins are excluded. |
| concepts `rate-limit` | In | `identity/http`, `identity/risk`, PostgreSQL/Valkey selected storage. |
| concepts `session-management` | In | Stateful/cached/stateless sessions, refresh, freshness, custom response and all session operations. Database-less OAuth provider-token cookies are separately excluded. |
| concepts `typescript` | Superseded | Typed Go declarations, OpenAPI and generated-client proof replace TypeScript inference; typed additional fields remain in scope. |
| concepts `users-accounts` | In | Profile update, email/password, verified deletion and account/provider-token operations. |
| authentication `email-password` | In | Signup/signin/signout/verification/reset/change/remember policy. |
| authentication Apple provider page | In backend profile facts | Apple client-secret generation maps exactly to the owner-visible direct operation `identity.oauth.provider-apple-client-secret-sign`; its team issuer, client subject, fixed Apple audience, key ID, ES256 key-handle signing, injected issued-at, pinned 15,777,000-second maximum expiry and redaction requirements are closed by `identity/oauth/providers`. The remaining Apple profile behavior is closed by the provider matrix below; framework/client snippets are excluded. |
| authentication provider pages | In backend profile facts | Every pinned built-in profile is closed by the provider matrix below; framework/client snippets are excluded. |
| route `account.ts` | In | `identity.account.list`, link start/token, unlink, access-token, refresh-token and provider-info. |
| route `callback.ts` | In | `identity.oauth.callback`. |
| route `email-verification.ts` | In | `identity.email.verification-send` and `identity.email.verification-confirm`. |
| route `error.ts` | Superseded | Stable typed/localized error envelopes and allowlisted error redirects replace Better Auth's hosted error-page behavior; no UI is owned. |
| route `index/meta` | Non-capability | Route aggregation metadata only; every exported route is disposed by its exact implementation and test leaves. |
| route `ok.ts` | In | `identity.health`; readiness is an additional local operation. |
| route `password.ts` | In | Reset request/inspect/complete and password verification. |
| route `session.ts` | In | Get/list/revoke-one/revoke-other/revoke-all plus explicit refresh/rotation. |
| route `sign-in.ts` | In | Password, social redirect, provider-token and continuation-aware signin operations. |
| route `sign-out.ts` | In | Idempotent `identity.session.signout`. |
| route `sign-up.ts` | In | Password and username signup operations. |
| route `update-session.ts` | In | `identity.session.update`; only input-enabled additional fields are writable. |
| route `update-user.ts` | In | Self-service profile update, change/set password, verified deletion and email change. |

## Official plugin documentation tree

| Pinned page | Disposition | Local owner or exact rationale |
| --- | --- | --- |
| `2fa` | In | `identity/mfa`, `identity/mfa/postgres`, `webauthn`; recovery-code re-view divergence below. |
| `admin` | In | `identity`, `identity/password`, `identity/email`, `identity/session`, `identity/mfa`, `identity/impersonation`, `authorization`, `identity/http`; the documented irreversible remove-user route maps to `identity.admin.user-delete`, and credential administration includes independently approved MFA reset/recovery without administrator-visible secrets. |
| `agent-auth` | Excluded | No autonomous-agent capability-discovery, approval, or execution credential product is selected. Standard OAuth/OIDC remains in scope. |
| `anonymous` | In | `identity/anonymous`. |
| `api-key` | In | `identity/apikey`, PostgreSQL and Valkey adapters. |
| `api-key advanced` | In | `identity/apikey`, `identity/session`, `identity/http`, PostgreSQL and Valkey adapters; API-key-derived request authentication, exact header selection, one-debit user-only session-compatible principal, multiple configuration IDs, organization-owned keys, storage profiles, quota/refill/expiry and metadata behavior are required. |
| `api-key reference` | In | `identity/apikey`, PostgreSQL and Valkey adapters; documented options, permissions and schema are part of the typed backend contract. |
| `api-key metadata` | Non-capability | Navigation metadata only; it adds no API-key behavior. |
| `autumn` | Excluded | Billing/payment integration; it does not define identity correctness. |
| `bearer` | In | `identity/session`, `identity/http`; includes both bearer acceptance and explicit token delivery. |
| `captcha` | In | `identity/risk/captcha` and reCAPTCHA, Turnstile, hCaptcha and CaptchaFox adapters. |
| `chargebee` | Excluded | Billing/payment integration. The page exists even though it is omitted from the pinned plugin index. |
| `commet` | Excluded | Billing/subscription integration. |
| `community-plugins` | Non-capability catalog | Third-party navigation/catalog metadata creates no official backend identity capability; typed extension-module support remains in scope. |
| `creem` | Excluded | Billing/payment integration. |
| `device-authorization` | In | `oauth-server/device`, `oauth-server/postgres`. |
| `dodopayments` | Excluded | Billing/payment integration. |
| `dub` | Non-capability integration | Lead attribution/analytics is not backend identity behavior. Generic OAuth linking remains in scope independently; no Dub integration is added. |
| `email-otp` | In | `identity/otp`, `identity/otp/postgres`, `identity/email`, `identity/password`. |
| `generic-oauth` | In | `identity/oauth`, `identity/oauth/postgres`, `identity/oauth/providers`. |
| `have-i-been-pwned` | In | `identity/risk/hibp`. |
| `i18n` | In | `identity/i18n`, `identity/http`. |
| `jwt` | In | `oauth-server/oidc`, `oauth-server/postgres`, existing `authentication/jwt`; session exchange is REQUIRED. |
| `last-login-method` | In | `identity/session`. |
| `magic-link` | In | `identity/magiclink`, `identity/delivery`, `capability`. |
| `mcp` | Excluded | MCP-specific discovery/session/adapters are not selected. Standards-generic protected-resource metadata and OAuth resource verification remain in scope. |
| `multi-session` | In | `identity/session`. |
| `oauth-provider` | In | `oauth-server`, `oauth-server/oidc`, `oauth-server/postgres`. MCP-specific additions remain excluded. |
| `oauth-proxy` | In | `identity/oauth/proxy`. |
| `oidc-provider` | In | `oauth-server`, `oauth-server/oidc`; the newer OAuth-provider capability is also required. |
| `one-tap` | In | `identity/oauth/onetap`. |
| `one-time-token` | In | `identity/session` session-transfer workflow plus `capability`. |
| `open-api` | In | `identity/http`; interactive Scalar UI is excluded, the OpenAPI document is required. |
| `organization` | In | `organization`, `organization/postgres`, `authorization`. |
| `passkey` | In | `passkey`, `passkey/postgres`, `webauthn`, `webauthn/postgres`. |
| `phone-number` | In | `identity/phone`, `identity/otp`, `identity/password`; OTP signin and password+phone signin are distinct operations, while caller-selected session suppression is superseded by the platform-wide remember/session-issuance policy. |
| `polar` | Excluded | The plugin page is a billing/payment integration; Polar social OAuth remains a separate in-scope provider profile. |
| `scim` | In with deployment-profile divergence | `scim`, `scim/organization`, `scim/postgres`; personal connections are an unselected deployment profile described below. |
| `siwe` | Excluded | Wallet-signature identity is not a selected product profile. |
| `sso` | In | `sso`, OIDC/OAuth/SAML protocol packages and PostgreSQL adapter, including provider registration variants, domain verification and both SAML logout initiation and SLO handling. |
| `stripe` | Excluded | Billing/payment integration. |
| `test-utils` | In | `identity/identitytest`; every pinned factory, persistence, login, auth-header, cookie-construction and OTP get/clear helper is required and production exposure is forbidden. |
| `username` | In | `identity/username`. |
| plugin index/meta pages | Non-capability | Catalog/navigation metadata only. |

## Source-exported and internal plugin surface

| Source export or internal module | Disposition | Local owner or rationale |
| --- | --- | --- |
| `access` | In | Existing `authorization`, `organization`, `identity/http`. |
| `admin` | In | Administrative create/get/list/update/delete, role/permission, password, email-address, MFA reset/recovery, ban, session and impersonation operations in `API_OPERATIONS.md`; upstream `remove-user` maps to the explicit irreversible `identity.admin.user-delete` cascade. |
| `anonymous` | In | `identity/anonymous`. |
| `bearer` | In | `identity.session.bearer-issue` and bearer-authenticated session operations. |
| `captcha` | In | Provider-neutral risk middleware plus four adapters. |
| `custom-session` | In | `identity/session` enrichment and `identity/http` schemas. |
| `device-authorization` | In | OAuth device operations. |
| `email-otp` | In | OTP operations including verification, reset and email change. |
| `generic-oauth` | In | Generic provider and social orchestration. |
| `haveibeenpwned` | In | HIBP range protocol. |
| `jwt` | In | JWT session exchange, OAuth JWTs, JWKS and key rotation. |
| `last-login-method` | In | Track/get/check/clear and privacy policy. |
| `magic-link` | In | Passwordless request/consume workflow. |
| `mcp` | Excluded | MCP-specific behavior only. |
| `multi-session` | In | Multi-account browser session set. |
| `oauth-popup` | In | Popup initiation/completion with exact opener origin. |
| `oauth-proxy` | In | Preview callback proxy. |
| `oidc-provider` | In | OIDC provider extension. |
| `one-tap` | In | Google One Tap. |
| `one-time-token` | In | Session transfer, not a generic unowned token workflow. |
| `open-api` | In | OpenAPI 3.1.1; reference UI excluded. |
| `organization` | In | Organization lifecycle and access control. |
| `phone-number` | In | Verified phone, distinct OTP/password signin and recovery workflows. |
| `siwe` | Excluded | Wallet signature profile not selected. |
| `test-utils` | In | `identity/identitytest`, including exact user/organization factory and persistence helpers, login/auth-header/cookie helpers, production-profile cookie signing, and instance-isolated OTP get/clear operations. |
| `two-factor` | In | MFA/TOTP/OTP/recovery/trusted devices/security keys. |
| `username` | In | Username workflows. |
| `types/plugins`, `hide-metadata` | Non-capability | Type and metadata utilities. |
| internal `additional-fields` | In | Typed user/account/session/organization fields with input/output/write policy. |
| plugin source index | Non-capability | Source aggregation metadata only; each plugin implementation leaf maps to its own top-level feature row. |

## Official top-level packages

| Pinned package | Disposition | Local owner or exact rationale |
| --- | --- | --- |
| `api-key` | In | `identity/apikey` and selected adapters. |
| `better-auth` | In by decomposed ownership | All core/backend operations in `API_OPERATIONS.md`; JavaScript client behavior excluded. |
| `cli` | Derived mixed summary | Exact blob/export dispositions retain secret generation, configuration inspection and redacted diagnostics as direct `identity/reference` outcomes. The selected PostgreSQL schema and migration APIs remain required by the conceptual contract, but upstream JavaScript ORM/dialect generators, exports, fixtures and mixed-database tests are conservatively classified as deployment-profile divergences rather than evidence for them. JavaScript project initialization, source rewriting, prompts, command wiring, package-manager mutation/upgrades, hosted-account login, AI and MCP commands remain excluded. This does not select or require a CLI product. |
| `core` | In by contract | Shared backend types and behavior are decomposed across identity modules; TypeScript type inference itself is excluded. |
| `drizzle-adapter` | Deployment-profile divergence | Additional database adapter; PostgreSQL is selected. |
| `electron` | Client surface | JavaScript desktop client integration is not a backend capability. |
| `expo` | Client surface | JavaScript/mobile client integration is not a backend capability. Native provider-token backend signin remains in scope. |
| `i18n` | In | `identity/i18n`. |
| `kysely-adapter` | Deployment-profile divergence | Additional database adapter is not in the selected deployment profile. |
| `memory-adapter` | Deployment-profile divergence | Test doubles MAY implement public store contracts; an in-memory production profile is not selected. |
| `mongo-adapter` | Deployment-profile divergence | Additional database engine is not in the selected deployment profile. |
| `oauth-provider` | In | OAuth/OIDC authorization-server modules plus the exact `./resource-client` export for standards-generic protected-resource metadata and access-token verification; the ordinary `./client` export remains JavaScript client surface. |
| `passkey` | In | WebAuthn/passkey modules. |
| `prisma-adapter` | Deployment-profile divergence | Additional database adapter is not in the selected deployment profile. |
| `redis-storage` | In by selected equivalent | Valkey adapters implement selected secondary/distributed storage behavior. |
| `scim` | In with deployment-profile divergence | SCIM modules and organization-scoped connections; the personal-connection deployment profile is not selected. |
| `sso` | In | Enterprise federation modules, including SAML SP/IdP login and single logout. |
| `stripe` | Excluded | Billing/payment integration. |
| `telemetry` | Superseded | Local safe instrumentation is opt-in, bounded, PII/secret-free and cannot alter decisions; upstream product telemetry behavior is not copied. |
| `test-utils` | In | `identity/identitytest`; in addition to the plugin helpers above, the top-level package's reusable adapter suites map to the public `identity.identitytest.store-conformance` harness covering CRUD, auth flow, case-insensitive identifiers, joins, numeric/UUID IDs and transactions. |

## Provider catalog disposition

The 35 pinned core profiles are all **In**: Apple, Atlassian, Amazon Cognito,
Discord, Dropbox, Facebook, Figma, GitHub, GitLab, Google, Hugging Face, Kakao,
Kick, LINE, Linear, LinkedIn, Microsoft (stable core ID `microsoft`), Naver, Notion, Paybin,
PayPal, Polar, Railway, Reddit, Roblox, Salesforce, Slack, Spotify, TikTok,
Twitch, Twitter/X, Vercel, VK, WeChat, and Zoom.

The 10 pinned generic helpers are all **In**: Auth0, Gumroad, HubSpot,
Keycloak, LINE, Microsoft Entra ID, Okta, Slack, Patreon, and Yandex. LINE and
Slack overlap the core catalog but MUST retain compatible stable identities.
LINE MUST support multiple independently configured channel/provider IDs.

The provider catalog MUST maintain one checked-in machine-readable matrix with
one row per profile and these fields: stable ID and aliases; required and
optional configuration with defaults; endpoint/discovery variants; client
authentication; scopes and authorization parameters; PKCE, response mode and
direct-token support; issuer, audience and nonce rules; stable subject; missing
email and email-verification trust; profile mapping; refresh, revocation and
expiry; environment/tenant/channel variants; documented incompatibilities;
pinned official evidence; and current interoperability status. Broad
"provider quirks" prose does not close this requirement.

That matrix is `CONFIGURATION_CATALOGS.json#provider_matrix`, version
`provider-matrix-v1`. It contains exactly the 43 stable IDs in the ordered
`provider-catalog-v1` catalog and binds that identity with the unchanged
catalog SHA-256. Its `configuration` members are names of required or optional
inputs, never secret values. The `pin-required` and
`tenant-metadata-pin-required` official-document statuses and the `not-run`
interoperability status explicitly keep a row evidence-blocked; the pinned
Better Auth 1.6.27 source locator proves only which upstream provider fact was
dispositioned. No consumer may promote any of those statuses to supported or
verified behavior.

The row schema additionally closes revocation endpoints, issuer aliases,
audience and authorized-party validation, forbidden scopes, authorization and
token parameters, ID-token/UserInfo/introspection precedence, signup and
implicit-link policy, refresh issuance/rotation/expiry/revocation/unlink
behavior, classified claim mappings, and incompatibilities. Every such field
is explicit in every row; an omitted field is schema drift, not an inherited
default.

The following rows are the minimum pinned profile decisions. The implemented
machine-readable matrix MUST retain every exact option and endpoint from the
pinned source even when this summary says "base contract".

| Profile | Required pinned distinctions |
| --- | --- |
| provider source index | Non-capability source aggregation; every provider implementation and test leaf maps to its exact provider row. |
| Apple | Multiple client audiences, optional app bundle identifier, `form_post`, code plus ID-token response, nonce verification including hashed nonce, first-consent user payload, private-relay email semantics, and the exact direct `identity.oauth.provider-apple-client-secret-sign` operation for a secret-envelope-backed P-256 key handle, team issuer, client subject, fixed Apple audience, key ID, ES256-only signing, injected issued-at, bounded expiry and redacted result/errors. |
| Atlassian | Exact Atlassian endpoints/scopes and stable account identifier under the base contract. |
| Amazon Cognito | Region/user-pool or explicit domain/issuer variants, client-secret/public-client behavior, discovery/JWKS and exact issuer/audience binding. |
| Discord | Exact scopes/profile mapping and optional bot-permission authorization parameters. |
| Dropbox | Exact access-type/token behavior and Dropbox account ID/email trust. |
| Facebook | Config ID and requested fields, limited-login JWT versus opaque access-token modes, and debug-token app/client binding before profile acceptance. |
| Figma | Exact endpoints/scopes, stable subject and email trust under the base contract. |
| GitHub | No assumed refresh token, exact verified-email selection, scopes and stable numeric account ID. |
| GitLab | Hosted and explicitly configured self-hosted base/issuer profiles with SSRF-safe endpoints. |
| Google | Multiple platform client IDs for ID-token audiences, one primary code-flow client, PKCE, hosted-domain claim enforcement, prompt/display/access-type and offline refresh behavior. |
| Hugging Face | Exact profile endpoint/scopes, stable subject and email trust under the base contract. |
| Kakao | Exact Kakao profile nesting, consent-dependent email and email-verification semantics. |
| Kick | Exact PKCE/token/profile behavior and stable subject under the base contract. |
| LINE | Exact ID-token validation and official endpoint behavior; multiple independent channel/provider IDs MUST coexist without collision. |
| Linear | Exact Linear endpoints/scopes and stable subject under the base contract. |
| LinkedIn | OIDC/profile endpoint selection, issuer/audience and verified-email semantics. |
| Microsoft (core ID `microsoft`) | Tenant ID (`common`, organizations, consumers or exact tenant), configurable authority/CIAM, tenant/issuer binding, public-client PKCE, multiple audiences and bounded profile-photo retrieval. It MUST remain distinct from the generic helper ID. |
| Naver | Exact nested profile mapping and provider-specific email trust. |
| Notion | Public versus internal integration distinctions and requested scopes. |
| Paybin | Exact scopes and explicit user-profile mapping. |
| PayPal | Sandbox versus live endpoints, exact OpenID scopes/claims and optional shipping/profile behavior. |
| Polar social | Social OAuth profile only, distinct from the excluded Polar billing plugin. |
| Railway | Exact supported scopes and Railway subject/profile mapping. |
| Reddit | Exact scopes, required user agent/token behavior and stable Reddit subject. |
| Roblox | Exact OpenID issuer/audience/scopes and stable subject. |
| Salesforce | Production, sandbox and explicitly configured domain variants with issuer/domain validation. |
| Slack | OpenID Connect versus Slack API token/scopes behavior, workspace/team identity and incremental scopes. |
| Spotify | Exact scopes/profile mapping and provider refresh behavior. |
| TikTok | Client-key naming/authentication, PKCE and exact subject/profile mapping. |
| Twitch | OIDC claims/issuer/audience and exact profile/email behavior. |
| Twitter/X | OAuth 2 PKCE, exact scopes/profile fields and refresh behavior. |
| Vercel | Exact Vercel scopes/team-account behavior and stable subject. |
| VK | Exact endpoint/version parameters, stable subject and email availability rules. |
| WeChat | Declared web/native platform and language profiles, non-standard token exchange method/parameters and stable union ID/open ID policy. |
| Zoom | PKCE and exact account/profile/refresh behavior. |
| Auth0 helper | Required HTTPS domain, discovery/issuer validation and configurable provider ID. |
| Gumroad helper | Exact Gumroad authorization/token/profile behavior despite its omission from the helper prose list. |
| HubSpot helper | Default `oauth` scope and configurable additional scopes. |
| Keycloak helper | Required exact realm issuer and discovery validation. |
| LINE helper | Same LINE semantics with caller-selected collision-free channel/provider ID. |
| Microsoft Entra ID helper (default ID `microsoft-entra-id`) | Required tenant ID and exact authority/issuer validation. It MUST NOT alias or overwrite the core `microsoft` profile. |
| Okta helper | Required exact issuer and discovery validation. |
| Slack helper | Generic helper identity MUST remain compatible with the core Slack profile without silent behavior merging. |
| Patreon helper | Exact endpoints/scopes/profile mapping under the generic contract. |
| Yandex helper | Exact endpoints/scopes/profile mapping under the generic contract. |

## Explicit divergences and exclusions

### Recovery-code plaintext retrieval

Better Auth exposes a server-only operation to view stored backup codes. The
local platform **Supersedes** that behavior: recovery codes MUST be shown only
at creation or full regeneration, MUST be digest-stored, and MUST NOT be
retrievable as plaintext afterward. A user who loses the displayed values MUST
reauthenticate and regenerate the complete set, atomically invalidating the old
set. Safe status/count remains available. This narrower contract is a
deliberate security decision, not an accidental omission.

### SCIM personal connections

Better Auth can create user-owned personal SCIM connections. This is a
**deployment-profile divergence**, not a product exclusion. Every SCIM
connection in the selected profile MUST be owned by an
organization and external provider, and management MUST require explicit
organization SCIM permissions. This avoids an unselected personal directory-
provisioning product and prevents a personal bearer from provisioning outside
an organization boundary. Inbound organization/provider connection records
are in scope; vendor-specific outbound directory SDKs are excluded.

### Database-less OAuth state and provider-token cookies

Better Auth can store OAuth state and provider account tokens in encrypted,
chunked cookies for database-less deployments. This is a **security
divergence**, not a product exclusion.
The selected reference profile uses durable, single-use OAuth transaction
state and envelope-encrypted provider tokens in PostgreSQL. Stateless identity
sessions remain separately in scope; they do not imply stateless provider-token
storage. Cookie OAuth state MAY carry only a non-authoritative correlation
handle whose durable record remains the source of truth.

### Remote JWT signing and externally hosted JWKS

Remote/KMS signing and externally hosted JWKS are **In as extension profiles**.
The signing and key-set contracts MUST support a bounded remote signer and a
pinned HTTPS JWKS publisher without forcing either into the reference default.
Integrity, key ID/algorithm binding, timeouts, cancellation, stale-key policy,
rotation overlap and unavailable/ambiguous outcomes are REQUIRED. The
reference default uses locally controlled encrypted key material and its owned
JWKS endpoint.

### Payment, wallet, MCP, and agent products

Autumn, Chargebee, Commet, Creem, Dodo Payments, Polar billing, Stripe and any
other billing/payment plugin are excluded. SIWE is excluded. MCP-specific and
agent-specific authentication products are excluded. These exclusions MUST
NOT remove generic OAuth/OIDC clients, protected-resource metadata, bearer
resource verification, device authorization, client credentials, or other
standards behavior that also happens to be useful to MCP or agents.

## Maintenance

`BETTER_AUTH_PARITY.md`, this file, `UPSTREAM_SURFACE.json`,
`UPSTREAM_LEAVES.json`, `generate_upstream_leaves.rb`,
`API_OPERATIONS.md`, the package goals, and the validator form one closure set.
The manifest MUST freeze every exact source-item -> disposition-row ->
capability -> operation-ID edge and every operation owner MUST resolve to its
registered goal; independent set digests and owner lower bounds are not
closure proof. Changing the pinned Better Auth revision MUST regenerate and
review the exact plugin-doc page set, source export set, top-level package set,
provider set, helper set, and operation edges before any parity claim can be
restored.
