# Canonical Identity Platform Operation Catalog

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Purpose and authority

This is the closed operation baseline consumed by every package goal,
`identity/http`, `identity/reference`, OpenAPI, parity review, and final
end-state proof. A capability row is not complete unless every operation it
references here has a public owner, an implemented direct contract, its stated
HTTP disposition, and executable evidence for the stated policy.

Operation IDs are stable public identifiers. Implementations MAY choose
different Go method names, but HTTP method and path are fixed as follows:
`both` rows use `/v1/` followed by the operation ID with the leading
`identity.` removed and dots converted to slashes; `read` rows use `GET` and
all other `both` rows use `POST`. `middleware` rows attach to every route their
row names and have no independent path. `protocol` rows use the exact standard
endpoint path fixed by `PROTOCOL_BASELINES.md` and their owning goal. Any
exception MUST appear in a closed exception table in this file before
assignment; there are no implicit aliases or worker-chosen paths. The endpoint
manifest MUST map each in-scope ID exactly once. An
operation MUST NOT disappear through consolidation into a broad service method.

## Contract notation

- **Exposure:** `both` means a public Go call and an HTTP operation; `direct`
  means a public Go call with no independently callable route; `protocol`
  means the standards-defined HTTP surface plus a public protocol handler;
  `middleware` means behavior attached to named routes rather than a standalone
  endpoint.
- **Access:** `public` is unauthenticated; `session` is an authenticated current
  session; `fresh` is a session satisfying the configured recent-proof policy;
  `admin` and `org:<permission>` require an explicit authorization decision;
  `oauth-client` authenticates a registered OAuth client as the endpoint
  requires; `scim-token` is a scoped SCIM bearer credential.
- A cookie-authenticated unsafe HTTP method MUST use CSRF protection. `CSRF`
  below therefore means request `Origin` validation plus anti-CSRF token/session
  binding; `origin` means callback or redirect allowlist validation; `none` is
  allowed only for a bearer-
  authenticated protocol operation that does not consume ambient cookies.
- A `pre-auth transaction` is the five-minute, single-use unauthenticated
  session-issuance/linking transaction bound to the flow cookie, exact request
  origin, intended action, tenant, callback/redirect, server nonce, and resolved
  remember policy. Every later challenge, capability, provider state, callback,
  and session-issuer/linker call in that flow MUST carry and atomically consume
  the same binding. Public operations that cannot issue, replace, or link a
  session remain `public` without this transaction.
- Enabled IdP-initiated SAML is the sole session-issuing exception because no
  relying-party pre-auth transaction can precede an unsolicited response. Its
  separate allowlisted login-CSRF/explicit-confirmation policy MUST establish
  equivalent user intent before session issuance; metadata, discovery, health,
  readiness, availability, and delivery-only operations genuinely do not issue,
  replace, or link sessions and therefore remain plain `public`.
- Every owner cell names exactly one accountable public-contract owner first;
  any later comma-separated packages are collaborators or dependencies. The
  first owner MUST define the operation ID and typed public contract. A
  collaborator MUST NOT publish a competing definition. `same` and other
  implicit owner references are forbidden.
- Every HTTP operation MUST have a bounded request, response, and error body.
  `safe`, `auth`, `secret`, `admin`, `provider`, and `protocol` are stable risk
  classes; `internal` applies only to non-route direct APIs. The endpoint
  manifest MUST additionally map every HTTP operation ID to one exact named
  rate-policy record. That record MUST state subject/IP/client/device
  dimensions, normalization, window, burst, store authority, atomicity,
  failure policy, step-up interaction, and `Retry-After` behavior. A risk class
  without that named record is incomplete and MUST NOT unlock implementation.
- `repeat` means a retry returns the same semantic state without duplicating a
  transition; `keyed` requires an idempotency key; `once` consumes a single-use
  proof; `read` has no state transition; `rotate` returns one known successor
  or an explicit unknown/reconciliation result; `none` means duplicate effects
  are invalid and MUST be rejected.
- Every operation returns its named typed result and stable typed errors. Common
  errors include invalid input, unauthenticated, forbidden, stale/freshness
  required, conflict, rate limited, unavailable, cancelled, deadline exceeded,
  and unknown commit. Rows name additional security-relevant outcomes.
- Every operation that can issue a session consumes one immutable
  `RememberPolicy`. The initiating request supplies it; every intermediate
  capability, OAuth/OIDC/SAML state, MFA continuation, popup/proxy exchange,
  and callback binds and returns the same value. A callback never chooses a new
  policy. Omitted policy means persistent reference-session behavior; explicit
  browser-session behavior emits no persistent cookie and caps the server-side
  session to the browser-session policy. Tests MUST prove propagation through
  every session-issuing row.

## Normative route and OpenAPI mapping

This section is the complete operation-to-transport mapping algorithm. For a
`both` row, the default HTTP method is `GET` when its idempotency token is
`read`, otherwise `POST`; the default path is `/v1/` plus the operation ID
after removing `identity.` and replacing every dot with `/`; and the OpenAPI
`operationId` is the unchanged canonical operation ID. The following table is
the closed set of `both` exceptions. A `both` operation absent from this table
MUST use the default exactly; an entry for any other operation is invalid.

| Operation ID | HTTP method | Exact path | OpenAPI operationId |
| --- | --- | --- | --- |
| `identity.openapi.document` | `GET` | `/openapi.json` | `identity.openapi.document` |

Every `protocol` row MUST occur exactly once in the following table. Paths use
OpenAPI template notation; template values are bounded request inputs, never
worker-selected route prefixes. No other protocol route or alias exists.

| Operation ID | HTTP method | Exact path | OpenAPI ownership |
| --- | --- | --- | --- |
| `identity.oauth.proxy-forward` | `POST` | `/oauth2/callback/proxy` | `identity.oauth.proxy-forward` |
| `identity.sso.oidc-callback` | `GET` | `/sso/oidc/callback` | `identity.sso.oidc-callback` |
| `identity.sso.oauth-callback` | `GET` | `/sso/oauth2/callback` | `identity.sso.oauth-callback` |
| `identity.sso.saml-metadata` | `GET` | `/saml2/{provider_id}/metadata` | `identity.sso.saml-metadata` |
| `identity.sso.saml-start` | `GET` | `/saml2/{provider_id}/login` | `identity.sso.saml-start` |
| `identity.sso.saml-acs` | `POST` | `/saml2/{provider_id}/acs` | `identity.sso.saml-acs` |
| `identity.sso.saml-idp-init` | `POST` | `/saml2/{provider_id}/idp-init` | `identity.sso.saml-idp-init` |
| `identity.sso.saml-logout-start` | `POST` | `/saml2/{provider_id}/logout` | `identity.sso.saml-logout-start` |
| `identity.sso.saml-slo` | `POST` | `/saml2/{provider_id}/slo` | `identity.sso.saml-slo` |
| `identity.scim.service-provider-config` | `GET` | `/scim/v2/ServiceProviderConfig` | `identity.scim.service-provider-config` |
| `identity.scim.schemas-list` | `GET` | `/scim/v2/Schemas` | `identity.scim.schemas-list` |
| `identity.scim.schema-get` | `GET` | `/scim/v2/Schemas/{schema_uri}` | `identity.scim.schema-get` |
| `identity.scim.resource-types-list` | `GET` | `/scim/v2/ResourceTypes` | `identity.scim.resource-types-list` |
| `identity.scim.resource-type-get` | `GET` | `/scim/v2/ResourceTypes/{resource_type_id}` | `identity.scim.resource-type-get` |
| `identity.scim.user-list` | `GET` | `/scim/v2/Users` | `identity.scim.user-list` |
| `identity.scim.user-get` | `GET` | `/scim/v2/Users/{resource_id}` | `identity.scim.user-get` |
| `identity.scim.user-create` | `POST` | `/scim/v2/Users` | `identity.scim.user-create` |
| `identity.scim.user-replace` | `PUT` | `/scim/v2/Users/{resource_id}` | `identity.scim.user-replace` |
| `identity.scim.user-patch` | `PATCH` | `/scim/v2/Users/{resource_id}` | `identity.scim.user-patch` |
| `identity.scim.user-delete` | `DELETE` | `/scim/v2/Users/{resource_id}` | `identity.scim.user-delete` |
| `identity.scim.group-list` | `GET` | `/scim/v2/Groups` | `identity.scim.group-list` |
| `identity.scim.group-get` | `GET` | `/scim/v2/Groups/{resource_id}` | `identity.scim.group-get` |
| `identity.scim.group-create` | `POST` | `/scim/v2/Groups` | `identity.scim.group-create` |
| `identity.scim.group-replace` | `PUT` | `/scim/v2/Groups/{resource_id}` | `identity.scim.group-replace` |
| `identity.scim.group-patch` | `PATCH` | `/scim/v2/Groups/{resource_id}` | `identity.scim.group-patch` |
| `identity.scim.group-delete` | `DELETE` | `/scim/v2/Groups/{resource_id}` | `identity.scim.group-delete` |
| `identity.scim.bulk` | `POST` | `/scim/v2/Bulk` | `identity.scim.bulk` |
| `identity.oauth-server.dynamic-register` | `POST` | `/oauth2/register` | `identity.oauth-server.dynamic-register` |
| `identity.oauth-server.authorize` | `GET` | `/oauth2/authorize` | `identity.oauth-server.authorize` |
| `identity.oauth-server.continue` | `POST` | `/oauth2/continue` | `identity.oauth-server.continue` |
| `identity.oauth-server.token` | `POST` | `/oauth2/token` | `identity.oauth-server.token` |
| `identity.oauth-server.introspect` | `POST` | `/oauth2/introspect` | `identity.oauth-server.introspect` |
| `identity.oauth-server.revoke` | `POST` | `/oauth2/revoke` | `identity.oauth-server.revoke` |
| `identity.oauth-server.end-session` | `GET` | `/oauth2/end-session` | `identity.oauth-server.end-session` |
| `identity.oauth-server.userinfo` | `GET` | `/oauth2/userinfo` | `identity.oauth-server.userinfo` |
| `identity.oauth-server.discovery-oauth` | `GET` | `/.well-known/oauth-authorization-server` | `identity.oauth-server.discovery-oauth` |
| `identity.oauth-server.discovery-oidc` | `GET` | `/.well-known/openid-configuration` | `identity.oauth-server.discovery-oidc` |
| `identity.oauth-server.jwks` | `GET` | `/.well-known/jwks.json` | `identity.oauth-server.jwks` |
| `identity.oauth-server.protected-resource-metadata` | `GET` | `/.well-known/oauth-protected-resource` | `identity.oauth-server.protected-resource-metadata` |
| `identity.oauth-server.device-authorize` | `POST` | `/oauth2/device_authorization` | `identity.oauth-server.device-authorize` |
| `identity.oauth-server.device-token` | `POST` | `/oauth2/token` | `identity.oauth-server.token` request variant `grant_type=urn:ietf:params:oauth:grant-type:device_code` |

The device-token row is the sole shared-route exception. RFC 8628 defines it
as a grant handled by the token endpoint, so it MUST NOT create a second
OpenAPI operation on the same method/path. Its request and response schemas,
rate policy, handler dispatch, and tests remain attributable to
`identity.oauth-server.device-token` under the named token-operation variant.
No other operation may share a method/path or OpenAPI owner.

Every `middleware` row MUST occur exactly once below. It has no method, path,
or independent OpenAPI operation; it is represented by its exact attachment
set on the named target operations. The target list is closed, and middleware
MUST NOT attach by prefix, tag, risk class, or worker-selected configuration.

| Middleware operation ID | Exact target operation IDs |
| --- | --- |
| `identity.session.last-method-record` | `identity.password.signin`, `identity.username.signin`, `identity.magic-link.consume`, `identity.otp.signin`, `identity.phone.signin`, `identity.anonymous.signin`, `identity.passkey.signin-verify`, `identity.oauth.callback`, `identity.oauth.onetap-callback`, `identity.oauth.popup-complete`, `identity.sso.oidc-callback`, `identity.sso.oauth-callback`, `identity.sso.saml-acs`, `identity.sso.saml-idp-init`, `identity.mfa.totp-verify`, `identity.mfa.otp-verify`, `identity.mfa.recovery-use`, `identity.mfa.security-key-assert-verify` |
| `identity.risk.captcha-verify` | `identity.password.signup`, `identity.password.signin`, `identity.password.reset-request`, `identity.username.signup`, `identity.username.signin`, `identity.magic-link.request`, `identity.otp.send`, `identity.otp.signin`, `identity.phone.send-verification`, `identity.phone.signin`, `identity.phone.password-reset-request`, `identity.anonymous.signin`, `identity.passkey.signin-options`, `identity.passkey.signin-verify`, `identity.oauth.signin-start`, `identity.oauth.signin-token`, `identity.oauth.onetap-callback`, `identity.sso.discover`, `identity.sso.signin-start` |

Every `direct` row maps to method `none`, path `none`, and OpenAPI ownership
`none`. A direct row in any route table, or any registered handler or OpenAPI
operation naming a direct row, is invalid. Thus every catalog row has exactly
one deterministic disposition: default/exception `both`, enumerated
`protocol`, enumerated attachment-only `middleware`, or route-forbidden
`direct`.

## Normative HTTP rate-policy catalog

The rate token in each operation row maps exactly to `rate.<token>` unless the
operation appears in the override table below. This mapping is complete for
every `both`, `protocol`, and `middleware` row; direct-only `internal` rows do
not invoke the HTTP limiter. The endpoint manifest MUST contain the resulting
policy ID and the validator MUST reject a missing or different mapping.
Middleware attached to an already admitted route records that route's exact
policy ID and shares its admission decision; it MUST NOT debit the same request
a second time.

All policies use an atomic token bucket. A request consumes one token from
every stated bucket and is denied if any bucket is empty. `1m`, `10m`, `1h`,
and `1d` mean continuously refilled minute, ten-minute, hour, and 24-hour
periods, not wall-clock reset windows. Trusted IP derivation uses the reference
proxy policy and IPv4-mapped addresses normalize to IPv4. IPv6 uses the full
canonical RFC 5952 address without subnet aggregation. Subnet aggregation
belongs only to a future, separately selected profile. Identifier dimensions use the canonical
identifier's domain-separated HMAC digest; raw identifiers are never rate
keys. Subject, actor, tenant, client, provider, transaction, challenge, and
device dimensions use opaque stable IDs. Every applicable dimension is
required; absence of a required dimension is invalid input, not a skipped
bucket.

| Policy ID | Atomic buckets and capacities | Authoritative store and outage behavior |
| --- | --- | --- |
| `rate.safe` | trusted IP 100/1m; when authenticated, subject 300/1m | Shared rate store; on outage use a process-local emergency trusted-IP bucket of 20/1m and emit an attributable degraded event. |
| `rate.auth` | trusted IP 20/10m; identifier when present 10/10m; challenge when present 10/lifetime | Shared rate store; fail closed. |
| `rate.secret` | trusted IP 10/10m; subject or capability subject 10/10m; challenge when present 5/lifetime | Shared rate store; fail closed. |
| `rate.admin` | actor 30/1m; tenant 100/1m; trusted IP 60/1m | Shared rate store; fail closed and audit the denial without request secrets. |
| `rate.provider` | trusted IP plus provider 30/10m; transaction when present 10/lifetime | Shared rate store; fail closed. |
| `rate.protocol` | authenticated client 120/1m; trusted IP 240/1m; issuer/resource tenant when present 600/1m | Shared rate store; fail closed with the standards-defined temporarily-unavailable response where the protocol defines one. |
| `rate.signup` | trusted IP 5/1h; identifier 3/1d | Shared rate store; fail closed and preserve enumeration-safe signup results. |
| `rate.signin` | trusted IP 20/10m; identifier 10/10m | Shared rate store; fail closed and preserve indistinguishable credential denial. |
| `rate.delivery` | trusted IP 10/1h; destination identifier 5/1h; subject when present 10/1h | Shared rate store; fail closed while returning the workflow's enumeration-safe delivery-intent envelope. |
| `rate.verify` | trusted IP 20/10m; identifier or subject 10/10m; challenge 10/lifetime | Shared rate store; fail closed; the owning workflow's stricter attempt counter also applies. |
| `rate.provider-callback` | trusted IP plus provider 30/10m; transaction 5/lifetime | Shared rate store; fail closed before token exchange and preserve provider-safe error redirects. |
| `rate.oauth-token` | authenticated client 60/1m; trusted IP 120/1m; refresh family when present 20/10m | Shared rate store; fail closed with OAuth `temporarily_unavailable`. |
| `rate.device-poll` | authenticated client plus device code: one request per advertised interval, initially 5 seconds; trusted IP 120/1h | Shared rate store; an early poll returns `slow_down` and adds 5 seconds to that device code's interval; outage fails closed with OAuth `temporarily_unavailable`. |

Rate denial returns stable `rate_limited` (or the standards-defined equivalent)
and `Retry-After` equal to the ceiling of the longest exhausted bucket's next
token delay, clamped to 1..3600 seconds. Store outage returns stable
`rate_limiter_unavailable` except where a protocol response is mandated. A
step-up result never replenishes or bypasses a bucket. Domain risk, credential
attempt, provider, and protocol limits remain cumulative; the strictest denial
wins.

| Override policy ID | Exact operation IDs |
| --- | --- |
| `rate.signup` | `identity.password.signup`, `identity.username.signup` |
| `rate.signin` | `identity.password.signin`, `identity.username.signin`, `identity.otp.signin`, `identity.phone.signin`, `identity.anonymous.signin` |
| `rate.delivery` | `identity.password.reset-request`, `identity.email.verification-send`, `identity.magic-link.request`, `identity.otp.send`, `identity.phone.send-verification`, `identity.phone.password-reset-request`, `identity.mfa.otp-send` |
| `rate.verify` | `identity.password.verify`, `identity.password.reset-complete`, `identity.email.verification-confirm`, `identity.email.change-confirm`, `identity.magic-link.consume`, `identity.otp.check`, `identity.otp.email-verify`, `identity.otp.password-reset`, `identity.otp.email-change-confirm`, `identity.phone.verify`, `identity.phone.password-reset-complete`, `identity.mfa.totp-verify`, `identity.mfa.otp-verify`, `identity.mfa.recovery-use`, `identity.mfa.security-key-register-verify`, `identity.mfa.security-key-assert-verify`, `identity.passkey.register-verify`, `identity.passkey.signin-verify` |
| `rate.provider-callback` | `identity.oauth.callback`, `identity.oauth.popup-complete`, `identity.oauth.onetap-callback`, `identity.oauth.proxy-forward`, `identity.sso.oidc-callback`, `identity.sso.oauth-callback`, `identity.sso.saml-acs`, `identity.sso.saml-idp-init`, `identity.sso.saml-slo` |
| `rate.oauth-token` | `identity.oauth-server.token` |
| `rate.device-poll` | `identity.oauth-server.device-token` |

## Core identity, password, and account operations

| Operation ID | Owner / exposure | Access / CSRF | Rate / idempotency | Request, result, and additional error semantics |
| --- | --- | --- | --- | --- |
| `identity.health` | `identity/http` / both | public / none | safe / read | Empty request; bounded process-liveness result; MUST NOT reveal dependencies or configuration. |
| `identity.ready` | `identity/reference` / both | public / none | safe / read | Empty request; bounded readiness state and opaque failed-component codes; no secrets or network topology. |
| `identity.platform.bootstrap-administrator` | `identity/reference` / direct | offline operator plus one-time bootstrap proof / internal | internal / once | Out-of-band operator invocation with bootstrap capability, initial tenant, administrator identity and role set; returns committed disabled-after-use bootstrap state, authority versions and immutable audit ID. It MUST NOT be registered as an HTTP handler or OpenAPI operation. |
| `identity.platform.role.create` | `identity/reference`, `authorization` / both | fresh platform admin / CSRF | admin / keyed | Stable role ID, name and initial statement IDs; returns the versioned role and authority version. |
| `identity.platform.role.update` | `identity/reference`, `authorization` / both | fresh platform admin / CSRF | admin / keyed | Role ID, expected version, name and statement IDs; returns the updated role, assignment impact and authority version. |
| `identity.platform.role.delete` | `identity/reference`, `authorization` / both | fresh platform admin / CSRF | admin / repeat | Role ID, expected version and explicit assignment disposition; deletes only after bounded reassignment/removal and returns the authority version. |
| `identity.platform.permission-statement.create` | `identity/reference`, `authorization` / both | fresh platform admin / CSRF | admin / keyed | Stable statement ID and typed resource/action/condition contract; returns the versioned statement and authority version. |
| `identity.platform.permission-statement.update` | `identity/reference`, `authorization` / both | fresh platform admin / CSRF | admin / keyed | Statement ID, expected version and complete typed replacement; returns affected roles and authority version. |
| `identity.platform.permission-statement.delete` | `identity/reference`, `authorization` / both | fresh platform admin / CSRF | admin / repeat | Statement ID, expected version and explicit role disposition; deletes only after bounded detachment and returns the authority version. |
| `identity.audit-retention.policy.update` | `identity/reference`, `audit` / both | fresh privacy admin / CSRF | admin / keyed | Standard/protected durations, expected policy version, reason and effective boundary; mutates the durable `audit/postgres` runtime policy, atomically commits the next version without retroactive deletion, and emits exactly `identity.audit_retention.change_policy`; unset/reset values are rejected. |
| `identity.audit-retention.legal-hold.create` | `identity/reference`, `audit` / both | fresh privacy admin / CSRF | admin / keyed | Stable hold ID, exact tenant/record/query scope, legal authority, reason and optional expiry; returns hold version and emits exactly `identity.audit_retention.create_legal_hold`. |
| `identity.audit-retention.legal-hold.update` | `identity/reference`, `audit` / both | fresh privacy admin / CSRF | admin / keyed | Hold ID, expected version, replacement scope/authority/reason/expiry; returns the next hold version and emits exactly `identity.audit_retention.update_legal_hold`. |
| `identity.audit-retention.legal-hold.release` | `identity/reference`, `audit` / both | fresh privacy admin / CSRF | admin / repeat | Active hold ID, expected version, release authority and reason; returns the terminal released version and emits exactly `identity.audit_retention.release_legal_hold`. |
| `identity.audit-retention.records.delete` | `identity/reference`, `audit` / direct | offline retention operator plus immutable plan confirmation / internal | internal / keyed | Plan digest, cutoff, expected policy/hold checkpoint, bounded batch/cursor and confirmation; rechecks holds, deletes only eligible records, retains a protected receipt, and emits exactly `identity.audit_retention.delete_records`. It MUST NOT be registered as an HTTP handler or OpenAPI operation. |
| `identity.user.create` | `identity` / both | admin / CSRF | admin / keyed | Typed identity fields and actor scope; returns safe user; duplicate identifier, tenant conflict, or unknown commit are distinct. |
| `identity.user.get` | `identity` / both | admin / none | admin / read | Opaque user ID and tenant; safe user projection or enumeration-safe not-found. |
| `identity.user.list` | `identity` / both | admin / none | admin / read | Bounded filters, stable cursor and limit; returns redacted page and next cursor. |
| `identity.user.update-admin` | `identity` / both | admin / CSRF | admin / keyed | Versioned allowed fields; returns safe user; stale version and forbidden field are distinct. |
| `identity.profile.get` | `identity` / both | session / none | safe / read | Current subject only; returns self-service projection. |
| `identity.profile.update` | `identity` / both | session / CSRF | safe / keyed | Name, image and input-enabled additional fields only; email/status/role are forbidden; returns updated projection and invalidation version. |
| `identity.privacy-export.request` | `identity` / both | fresh / CSRF | secret / keyed | Export scope and format; snapshots authorized subject data and returns an opaque export job without provider secrets. |
| `identity.privacy-export.status` | `identity` / both | fresh / none | secret / read | Owned export job; returns bounded pending/ready/failed/expired state and no download bearer. |
| `identity.privacy-export.cancel` | `identity` / both | fresh / CSRF | secret / repeat | Owned export job/version; cancels pending work and crypto-erases any completed payload when policy permits. |
| `identity.privacy-export.download` | `identity` / both | fresh plus export capability / origin | secret / once | Single-use job/audience/session-bound capability; returns the bounded encrypted export stream or expired/replayed result and records download completion. |
| `identity.user.suspend` | `identity` / both | admin / CSRF | admin / keyed | User, reason and optional expiry; returns status transition and revocation outcome. |
| `identity.user.restore` | `identity` / both | admin / CSRF | admin / keyed | User and expected status version; returns restored state or conflict. |
| `identity.user.anonymize` | `identity` / both | admin / CSRF | admin / keyed | User, reason and retention/legal-hold decision; returns irreversible transition summary. |
| `identity.deletion.request` | `identity`, `identity/email`, `identity/delivery`, `identity/session`, `identity/risk`, `capability` / both | session / CSRF | secret / keyed | Closed deletion-proof choice: current password plus fresh session for a password user; fresh UV passkey for a passkey user; purpose/session/version-bound emailed capability for a provider-only user with a verified email; or fresh UV passkey or audited administrator recovery when there is no verified email. Returns deleted or enumeration-safe delivery-intent state. |
| `identity.deletion.confirm` | `identity` / both | session plus deletion capability / origin | secret / once | Purpose/user/session/version-bound token and callback; atomically consumes token, deletes owned identity state, revokes sessions, and returns deleted/redirect result. |
| `identity.account.list` | `identity` / both | session / none | safe / read | Current subject; returns safe linked-account metadata without provider secrets. |
| `identity.account.link-start` | `identity/oauth` / both | fresh / CSRF | provider / keyed | Provider, exact callback/error URLs, scopes and signup-disabled intent; returns redirect/native challenge result. |
| `identity.account.link-token` | `identity/oauth` / both | fresh / CSRF | provider / keyed | Provider credential, nonce and platform audience; returns linked account or verified collision. |
| `identity.account.unlink` | `identity`, `identity/oauth` / both | fresh / CSRF | secret / keyed | Account ID, expected version and provider-revoke choice; returns local/provider outcomes and refuses orphaning. |
| `identity.account.provider-info` | `identity/oauth` / both | session / none | provider / read | Owned account ID; returns bounded mapped provider profile, never raw secrets by default. |
| `identity.account.access-token` | `identity/oauth` / both | fresh / CSRF | provider / keyed | Owned account, scopes and refresh policy; returns secret-bearing token result with expiry and reconciliation outcome. |
| `identity.account.refresh-token` | `identity/oauth` / both | fresh / CSRF | provider / rotate | Owned account and expected token version; single-flight refresh returns successor metadata or revoked/unknown. |
| `identity.password.signup` | `identity/password` / both | public plus pre-auth transaction / origin | auth / keyed | Email, password, allowed fields, remember policy and callback; returns session/verification-required result without enumeration. |
| `identity.password.signin` | `identity/password` / both | public plus pre-auth transaction / origin | auth / none | Identifier, password, remember policy and callback; returns session, MFA continuation, verification-required, or indistinguishable denial. |
| `identity.password.verify` | `identity/password` / both | session / CSRF | secret / none | Current password plus purpose/audience; returns short-lived non-session reauthentication proof or generic denial. |
| `identity.password.set` | `identity/password` / both | fresh or reset/admin proof / CSRF | secret / keyed | New password and proof; returns credential version and session-revocation outcome. |
| `identity.password.change` | `identity/password` / both | fresh plus current-password proof / CSRF | secret / keyed | Current/new password and revoke policy; returns credential version and revocation result. |
| `identity.password.reset-request` | `identity/password` / both | public / origin | auth / keyed | Identifier and allowlisted callback; always returns enumeration-safe delivery-intent result. |
| `identity.password.reset-inspect` | `identity/password` / both | reset capability / origin | secret / read | Bounded token; returns valid/invalid UI state without revealing subject. |
| `identity.password.reset-complete` | `identity/password` / both | reset capability / origin | secret / once | Token and new password; returns committed credential/session-revocation result or invalid/replayed/expired. |
| `identity.email.verification-send` | `identity/email` / both | public or session / origin | auth / keyed | Address/subject context and callback; returns enumeration-safe delivery state. |
| `identity.email.verification-confirm` | `identity/email` / both | pre-auth-bound email capability when issuing a session, otherwise email capability / origin | secret / once | Token and callback; returns verified identity and optional new session only after commit. |
| `identity.email.change-request` | `identity/email` / both | fresh / CSRF | secret / keyed | New email, current-address proof policy and callback; returns current/new proof requirement. |
| `identity.email.change-confirm` | `identity/email` / both | email-change capability / origin | secret / once | Bound proof and callback; returns new primary address or collision/replay/expired. |
| `identity.username.signup` | `identity/username`, `identity/password` / both | public plus pre-auth transaction / origin | auth / keyed | Username/display username/password/email policy and remember policy; returns normal signup result. |
| `identity.username.signin` | `identity/username`, `identity/password` / both | public plus pre-auth transaction / origin | auth / none | Username/password/remember policy; returns normal signin result without username enumeration. |
| `identity.username.update` | `identity/username` / both | fresh / CSRF | secret / keyed | New username/display value and expected version; returns updated projection or collision/cooldown. |
| `identity.username.available` | `identity/username` / both | public / none | auth / read | Candidate username; returns advisory availability without reservation or account disclosure. |

## Sessions, passwordless, phone, and MFA

Phone signup initiation is `identity.phone.send-verification` with purpose
`signup`; there is no second implicit signup operation. Public signup/signin
initiation creates or uses the canonical pre-auth transaction and binds tenant,
purpose, canonical number, and resolved remember policy. A session-authenticated
number-change challenge uses the authenticated session and MUST NOT create or
substitute a public pre-auth transaction.

| Operation ID | Owner / exposure | Access / CSRF | Rate / idempotency | Request, result, and additional error semantics |
| --- | --- | --- | --- | --- |
| `identity.session.get` | `identity/session` / both | session or session bearer / none | safe / read | Current token and refresh policy; returns principal/session, `needsRefresh`, or unauthenticated. |
| `identity.session.list` | `identity/session` / both | session / none | safe / read | Stable cursor/limit; returns safe active-session metadata. |
| `identity.session.update` | `identity/session` / both | session / CSRF | safe / keyed | Input-enabled session fields/device label and version; core security fields are forbidden. |
| `identity.session.refresh` | `identity/session` / both | session / CSRF | safe / rotate | Current version; returns rotated/refreshed session or revoked/stale/unknown. |
| `identity.session.revoke-one` | `identity/session` / both | session / CSRF | secret / repeat | Owned session ID/version; returns revoked/already-revoked. |
| `identity.session.revoke-other` | `identity/session` / both | fresh / CSRF | secret / repeat | Current session; returns bounded count and propagation result. |
| `identity.session.revoke-all` | `identity/session` / both | fresh / CSRF | secret / repeat | Subject/global version; returns bounded count and current-session disposition. |
| `identity.session.signout` | `identity/session` / both | session or absent / CSRF | safe / repeat | Current transport; clears exact cookie/bearer state and returns success even if already absent. |
| `identity.session.select-active` | `identity/session` / both | session-set / CSRF | secret / keyed | Browser-container and owned session ID; returns active selection and cookies without cross-account substitution. |
| `identity.session.transfer-generate` | `identity/session`, `capability` / both | fresh, non-impersonated / CSRF | secret / keyed | Exact target origin/audience and cookie mode; returns three-minute reveal-once token. |
| `identity.session.transfer-consume` | `identity/session`, `capability` / both | pre-auth-bound transfer capability / origin | secret / once | Token and exact target origin; returns the same bounded valid session or replay/revoked/wrong-origin. |
| `identity.session.last-method-get` | `identity/session` / both | session or privacy cookie / none | safe / read | Current privacy scope; returns method or unavailable/disabled. |
| `identity.session.last-method-check` | `identity/session` / both | session or privacy cookie / none | safe / read | Candidate method; returns boolean without authentication meaning. |
| `identity.session.last-method-clear` | `identity/session` / both | session or privacy cookie / CSRF | safe / repeat | Current scope; deletes database/cookie records. |
| `identity.session.last-method-record` | `identity/session` / middleware | successful configured signin / none | signin / keyed | Records only the successful method after identity/session commit; shares the parent signin admission without a second debit; failed attempts and privacy-disabled profiles write nothing. |
| `identity.session.bearer-issue` | `identity/session`, `identity/http` / both | successful session issuance / none | secret / rotate | Emits the exact session bearer once in a typed body or exposed response header; never uses URL/query; rotation replaces prior bearer. |
| `identity.magic-link.request` | `identity/magiclink` / both | public plus pre-auth transaction / origin | auth / keyed | Email, signin/signup intent and callback; returns enumeration-safe delivery result. |
| `identity.magic-link.consume` | `identity/magiclink` / both | pre-auth-bound magic-link capability / origin | secret / once | Token and callback; returns session/linked verification or invalid/replay/expired. |
| `identity.otp.send` | `identity/otp` / both | public plus pre-auth transaction for signin, session, or MFA continuation by purpose / origin or CSRF | auth / keyed | Channel, subject and declared purpose; returns enumeration-safe delivery/challenge state. |
| `identity.otp.check` | `identity/otp` / both | purpose challenge / none | auth / none | Challenge and code; validates without consume only if enabled and separately limited. |
| `identity.otp.signin` | `identity/otp` / both | pre-auth-bound public challenge / origin | auth / once | Identifier, code and remember policy; returns session/MFA continuation or generic denial. |
| `identity.otp.email-verify` | `identity/otp`, `identity/email` / both | pre-auth-bound email challenge when issuing a session, otherwise email challenge / origin | auth / once | Address-bound code; returns verified address/session policy. |
| `identity.otp.password-reset` | `identity/otp`, `identity/password` / both | reset challenge / origin | secret / once | Code and new password; returns credential/revocation result. |
| `identity.otp.email-change-request` | `identity/otp`, `identity/email` / both | fresh / CSRF | secret / keyed | New address and optional current-address OTP; returns new-address challenge. |
| `identity.otp.email-change-confirm` | `identity/otp`, `identity/email` / both | change challenge / CSRF | secret / once | New address/code; returns committed change or collision/replay. |
| `identity.phone.send-verification` | `identity/phone` / both | public plus pre-auth transaction for signup/signin, or session-authenticated number-change / origin or CSRF | auth / keyed | E.164 number, purpose, and remember policy for signup/signin; creates or uses the canonical single-use pre-auth transaction bound to tenant, purpose, canonical number, and resolved remember policy. Session-authenticated number-change binds the current subject/session and does not create public pre-auth state. Returns an enumeration-safe challenge and emits exactly `identity.identifier.request_verification`. |
| `identity.phone.verify` | `identity/phone` / both | pre-auth-bound phone challenge for signup/session issuance, or session-bound number-change challenge / origin or CSRF | auth / once | Number/code and the same tenant, purpose, canonical number, and resolved remember policy for public flows; authenticated change instead binds subject/session/version. Signup/session purpose returns the bound session or continuation; number-change returns only verified identifier state. No session-suppression option exists and the operation emits exactly `identity.identifier.verify`. |
| `identity.phone.signin` | `identity/phone` / both | pre-auth-bound public challenge / origin | auth / once | Number/code/remember policy bound to the same tenant, signin purpose, canonical number, and resolved remember policy; returns session/MFA continuation. |
| `identity.phone.update` | `identity/phone` / both | fresh / CSRF | secret / keyed | New number and proof; on commit initiates exactly `lifecycle.cascade.identifier_change`, returns the pending/verified transition, and emits exactly `identity.identifier.change`. |
| `identity.phone.remove` | `identity/phone` / both | fresh / CSRF | secret / keyed | Number/version; on commit initiates exactly `lifecycle.cascade.identifier_remove`, returns removal or last-recovery-path denial, and emits exactly `identity.identifier.remove`. |
| `identity.phone.password-reset-request` | `identity/phone`, `identity/password` / both | explicitly enabled public recovery plus pre-auth transaction / origin | auth / keyed | Available only with `phone.recovery.enabled=true` and denied while disabled. Canonical number and recovery purpose create a purpose-bound OTP challenge and reset capability bound to the pre-auth transaction; no remember or session-suppression choice exists. The operation accepts only an opaque fresh initiation-only `RiskEvidence` reference issued by `identity/risk` for purpose `phone-password-reset-initiate`, never raw carrier facts. That evidence is reserved and atomically finalized with OTP challenge and reset capability issuance in one authoritative unit of work. It returns an enumeration-safe challenge result. |
| `identity.phone.password-reset-complete` | `identity/phone`, `identity/password` / both | explicitly enabled reset capability plus phone OTP plus independent factor / origin | secret / once | Available only with `phone.recovery.enabled=true` and denied while disabled. The canonical reset capability, purpose-bound phone OTP proof, eligible independent factor, and fresh one-use `RiskEvidence` issued by `identity/risk` for purpose `phone-password-reset-complete` must bind the same tenant, subject, recovery operation/purpose, canonical number, pre-auth transaction, attempt ID, and policy version. This separate completion-only RiskEvidence must not reuse the initiation artifact; the two purposes have distinct references, digests, reservations, and terminal records. Raw caller carrier facts and stale/mismatched/replayed evidence deny; phone/code/new-password alone is insufficient. No session is issued and no remember or session-suppression choice exists. Returns the credential/revocation result. |
| `identity.anonymous.signin` | `identity/anonymous` / both | public plus pre-auth transaction / origin | auth / keyed | Device/context and remember policy; returns constrained anonymous session. |
| `identity.anonymous.delete` | `identity/anonymous`, `identity` / both | anonymous session / CSRF | secret / keyed | Current anonymous subject/session/version and reason; revokes the session and deletes or anonymizes only anonymous-owned state under retention/legal-hold policy, without requiring permanent-account proof. |
| `identity.anonymous.upgrade` | `identity/anonymous` / both | anonymous session plus permanent proof / CSRF | secret / once | Target credential/account proof; returns merged identity/session or collision. |
| `identity.mfa.enable` | `identity/mfa` / both | fresh plus primary proof / CSRF | secret / keyed | Factor policy; returns pending enrollment and reveal-once recovery material where applicable. |
| `identity.mfa.disable` | `identity/mfa` / both | fresh plus factor proof / CSRF | secret / keyed | Factor/all-factor target; returns policy-compliant state or recovery-path denial. |
| `identity.mfa.list` | `identity/mfa` / both | session / none | safe / read | Returns safe factor/trusted-device/recovery-count metadata only. |
| `identity.mfa.totp-uri` | `identity/mfa` / both | pending enrollment / CSRF | secret / read | Enrollment ID; returns reveal-once provisioning URI. |
| `identity.mfa.totp-verify` | `identity/mfa` / both | pending/signin challenge / origin or CSRF | auth / once | Code and trust-device choice; returns activation/session/step-up result. |
| `identity.mfa.otp-send` | `identity/mfa`, `identity/otp` / both | MFA continuation / origin | auth / keyed | Factor/channel; returns challenge delivery state. |
| `identity.mfa.otp-verify` | `identity/mfa`, `identity/otp` / both | MFA continuation / origin | auth / once | Code and trust-device choice; returns session/step-up result. |
| `identity.mfa.recovery-regenerate` | `identity/mfa` / both | fresh plus factor proof / CSRF | secret / rotate | Replaces all codes and returns plaintext exactly once. |
| `identity.mfa.recovery-use` | `identity/mfa` / both | MFA continuation / origin | auth / once | Recovery code; returns session/recovery-required result and burns code. |
| `identity.mfa.trusted-device-revoke` | `identity/mfa` / both | fresh / CSRF | secret / repeat | Device ID or all; returns revocation propagation. |
| `identity.mfa.security-key-register-options` | `identity/mfa`, `webauthn` / both | fresh / CSRF | secret / keyed | Factor policy; returns bounded WebAuthn creation options. |
| `identity.mfa.security-key-register-verify` | `identity/mfa`, `webauthn` / both | enrollment challenge / CSRF | secret / once | Ceremony result; returns activated factor. |
| `identity.mfa.security-key-assert-options` | `identity/mfa`, `webauthn` / both | MFA continuation / origin | auth / keyed | Challenge context; returns bounded assertion options. |
| `identity.mfa.security-key-assert-verify` | `identity/mfa`, `webauthn` / both | MFA continuation / origin | auth / once | Assertion; returns session/step-up result. |
| `identity.mfa.factor-update` | `identity/mfa` / both | fresh plus factor proof / CSRF | secret / keyed | Rename/remove/replace and expected version; returns factor state. |

## Passkeys, social OAuth, API keys, and administration

| Operation ID | Owner / exposure | Access / CSRF | Rate / idempotency | Request, result, and additional error semantics |
| --- | --- | --- | --- | --- |
| `identity.passkey.register-options` | `passkey`, `webauthn` / both | session or pre-auth transaction / CSRF or origin | auth / keyed | RP/user/authenticator policy and extensions; creation options or policy denial. |
| `identity.passkey.register-verify` | `passkey`, `webauthn` / both | pre-auth-bound registration challenge for unauthenticated registration, otherwise session-bound registration challenge / CSRF or origin | auth / once | Attestation result/name; returns credential and optional new identity/session. |
| `identity.passkey.signin-options` | `passkey`, `webauthn` / both | public plus pre-auth transaction / origin | auth / keyed | Optional identifier, conditional/usernameless mode and extensions; assertion options. |
| `identity.passkey.signin-verify` | `passkey`, `webauthn` / both | pre-auth-bound assertion challenge / origin | auth / once | Assertion and remember policy; returns session/MFA policy or generic denial. |
| `identity.passkey.list` | `passkey` / both | session / none | safe / read | Returns safe credential metadata, AAGUID/backup state and labels. |
| `identity.passkey.update` | `passkey` / both | fresh / CSRF | secret / keyed | Credential ID/version/name; returns updated metadata. |
| `identity.passkey.delete` | `passkey` / both | fresh / CSRF | secret / repeat | Credential ID/version; returns deleted/already-deleted or recovery-path denial. |
| `identity.oauth.signin-start` | `identity/oauth` / both | public plus pre-auth transaction / origin | provider / keyed | Provider, signin/signup intent, login hint, scopes, success/new-user/error callbacks, popup/URL-return mode and bounded state; returns redirect transaction. |
| `identity.oauth.signin-token` | `identity/oauth`, `identity/oauth/providers` / both | public plus pre-auth transaction / origin | provider / keyed | Provider ID token or supported opaque access token, nonce, platform client/audience and optional provider token metadata; returns session/MFA/collision result. |
| `identity.oauth.callback` | `identity/oauth` / both | pre-auth-bound provider callback state / origin | provider / once | Code/error/issuer/state; returns session/link/new-user/error redirect result or replay/mix-up denial. |
| `identity.oauth.popup-complete` | `identity/oauth`, `identity/http` / both | popup transaction / origin | provider / once | One-time result channel; bounded CSP page and exact-origin acknowledged delivery. |
| `identity.oauth.onetap-start` | `identity/oauth/onetap` / both | public plus pre-auth transaction / origin | provider / keyed | Prompt/button options, nonce, hints and callback; returns GIS request context. |
| `identity.oauth.onetap-callback` | `identity/oauth/onetap` / both | pre-auth-bound One Tap state / origin | provider / once | Google credential and nonce; returns session/link/dismissal/error. |
| `identity.oauth.proxy-forward` | `identity/oauth/proxy` / protocol | preview transaction / origin | provider / once | Encrypted callback profile/state; returns encrypted origin-bound result and performs no production writes. |
| `identity.oauth.provider-list` | `identity/oauth/providers` / both | public / none | safe / read | Returns configured safe provider IDs, display names and supported modes only. |
| `identity.apikey.create` | `identity/apikey` / both | session or org:create-key / CSRF | secret / keyed | Owner/config/name/permissions/quota/expiry/metadata; reveal-once key and safe record. |
| `identity.apikey.verify` | `identity/apikey` / both | API-key bearer / none | auth / none | Raw key plus required permissions; valid principal/quota result or indistinguishable invalid. |
| `identity.apikey.get` | `identity/apikey` / both | owner/admin / none | safe / read | Key ID; safe metadata only. |
| `identity.apikey.list` | `identity/apikey` / both | owner/admin / none | safe / read | Owner/config filters and cursor; stable redacted page. |
| `identity.apikey.update` | `identity/apikey` / both | owner/admin / CSRF | secret / keyed | Name/metadata/permissions/quota and version; updated safe record. |
| `identity.apikey.rotate` | `identity/apikey` / both | fresh owner/admin / CSRF | secret / rotate | Key ID/version; reveal-once successor and predecessor disposition. |
| `identity.apikey.delete` | `identity/apikey` / both | owner/admin / CSRF | secret / repeat | Key ID/version; revoked/deleted outcome. |
| `identity.apikey.delete-expired` | `identity/apikey` / both | admin / CSRF | admin / keyed | Bounded batch/cursor; count and next cursor. |
| `identity.admin.user-role-set` | `authorization`, `identity` / both | admin / CSRF | admin / keyed | User, roles/permissions and expected version; decision and updated projection. |
| `identity.admin.permission-check` | `authorization` / both | admin principal / none | safe / read | Requested administrative resource/action/context; explicit allow/deny with no role-name inference. |
| `identity.admin.user-password-set` | `identity/password` / both | admin / CSRF | admin / keyed | User/new password/forced-change/revocation policy; credential transition only. |
| `identity.admin.user-delete` | `identity`, `identity/session`, `identity/oauth`, `identity/apikey`, `identity/impersonation`, `organization` / both | fresh admin / CSRF | admin / keyed | User, reason, retention/legal-hold disposition and expected version; irreversibly deletes or anonymizes the user under the selected policy and returns the complete cascade/revocation outcome or unknown commit. |
| `identity.admin.user-ban` | `identity` / both | admin / CSRF | admin / keyed | User, reason and expiry; status/revocation result. |
| `identity.admin.user-unban` | `identity` / both | admin / CSRF | admin / keyed | User/version; restored status. |
| `identity.admin.session-list` | `identity/session` / both | admin / none | admin / read | Target user and cursor; safe session page. |
| `identity.admin.session-revoke` | `identity/session` / both | admin / CSRF | admin / repeat | User/session; revocation result. |
| `identity.admin.session-revoke-all` | `identity/session` / both | admin / CSRF | admin / repeat | User; revocation count and propagation. |
| `identity.impersonation.start` | `identity/impersonation` / both | fresh admin / CSRF | admin / keyed | Target, reason, duration and scope; marked actor-chain session or denial. |
| `identity.impersonation.stop` | `identity/impersonation` / both | impersonated session / CSRF | admin / repeat | Current grant; restored actor session or expired/revoked. |

## Organizations, federation, and SCIM

| Operation ID | Owner / exposure | Access / CSRF | Rate / idempotency | Request, result, and additional error semantics |
| --- | --- | --- | --- | --- |
| `identity.organization.create` | `organization` / both | session / CSRF | admin / keyed | Name/slug/typed fields; organization and owner membership or limit/conflict. |
| `identity.organization.slug-available` | `organization` / both | session / none | safe / read | Candidate slug; advisory result only. |
| `identity.organization.list` | `organization` / both | session / none | safe / read | Stable cursor; organizations visible to current subject. |
| `identity.organization.get` | `organization` / both | org:read / none | safe / read | Organization ID; bounded full view. |
| `identity.organization.update` | `organization` / both | org:update / CSRF | admin / keyed | Versioned allowed fields; updated organization. |
| `identity.organization.delete` | `organization` / both | org:delete / CSRF | admin / keyed | Version/reason; archived/deleted transition or last-owner/policy denial. |
| `identity.organization.active-set` | `organization`, `identity/session` / both | session plus membership / CSRF | safe / keyed | Organization ID or clear; updated session selection. |
| `identity.organization.active-get` | `organization`, `identity/session` / both | session / none | safe / read | Returns the active organization projection or explicit none; stale/deleted selections are cleared safely. |
| `identity.organization.invitation-send` | `organization` / both | org:invite / CSRF | admin / keyed | Recipient/roles/team/expiry; invitation plus delivery state. |
| `identity.organization.invitation.get` | `organization` / both | recipient or org:invite / none | safe / read | Invitation ID/token-safe context; bounded projection. |
| `identity.organization.invitation.list` | `organization` / both | org:invite / none | safe / read | Status/cursor; stable page. |
| `identity.organization.invitation.list-mine` | `organization` / both | session / none | safe / read | Current verified identifiers; bounded page. |
| `identity.organization.invitation.accept` | `organization` / both | intended verified recipient / CSRF | secret / once | Invitation proof/version; membership result or expired/collision. |
| `identity.organization.invitation.reject` | `organization` / both | intended recipient / CSRF | safe / repeat | Invitation/version; rejected state. |
| `identity.organization.invitation.cancel` | `organization` / both | org:invite / CSRF | admin / repeat | Invitation/version; cancelled state. |
| `identity.organization.member.list` | `organization` / both | org:member-read / none | safe / read | Search/cursor; stable page. |
| `identity.organization.member.add` | `organization` / both | org:member-write / CSRF | admin / keyed | User/roles/teams; membership or duplicate/scope conflict. |
| `identity.organization.member.update` | `organization` / both | org:member-write / CSRF | admin / keyed | Member roles/version; updated membership or last-owner denial. |
| `identity.organization.member.remove` | `organization` / both | org:member-write / CSRF | admin / repeat | Member/version; removed or last-owner/recovery-admin denial. |
| `identity.organization.member.leave` | `organization` / both | member / CSRF | safe / repeat | Organization/version; left or last-owner denial. |
| `identity.organization.member.active-get` | `organization`, `identity/session` / both | session plus active organization / none | safe / read | Returns current membership and safe role references or explicit none. |
| `identity.organization.member.active-role-get` | `organization`, `identity/session` / both | session plus active organization / none | safe / read | Returns bounded effective role/permission summary, not an authorization bypass. |
| `identity.organization.owner-transfer` | `organization` / both | fresh owner / CSRF | admin / keyed | Target member and versions; atomic ownership transfer. |
| `identity.organization.role.create` | `organization` / both | org:role-write / CSRF | admin / keyed | Name/statements; role or limit/conflict. |
| `identity.organization.role.list` | `organization` / both | org:role-read / none | safe / read | Cursor; static/dynamic role page. |
| `identity.organization.role.get` | `organization` / both | org:role-read / none | safe / read | Role ID; safe role. |
| `identity.organization.role.update` | `organization` / both | org:role-write / CSRF | admin / keyed | Statements/version; role and binding-impact result. |
| `identity.organization.role.delete` | `organization` / both | org:role-write / CSRF | admin / repeat | Role/version/reassignment; deleted or in-use conflict. |
| `identity.organization.permission-check` | `organization`, `authorization` / both | member / none | safe / read | Resource/action/context; explicit allow/deny with no privilege inference. |
| `identity.organization.team.create` | `organization` / both | org:team-write / CSRF | admin / keyed | Team fields; team or limit/conflict. |
| `identity.organization.team.list` | `organization` / both | org:team-read / none | safe / read | Cursor; team page. |
| `identity.organization.team.update` | `organization` / both | org:team-write / CSRF | admin / keyed | Team/version; updated team. |
| `identity.organization.team.delete` | `organization` / both | org:team-write / CSRF | admin / repeat | Team/version; deletion/membership outcome. |
| `identity.organization.team.active-set` | `organization`, `identity/session` / both | team member / CSRF | safe / keyed | Team ID or clear; active-team state. |
| `identity.organization.team.list-mine` | `organization` / both | session / none | safe / read | Organization/cursor; teams containing the current subject. |
| `identity.organization.team.member-list` | `organization` / both | org:team-read / none | safe / read | Team/cursor; members page. |
| `identity.organization.team.member-add` | `organization` / both | org:team-write / CSRF | admin / keyed | Team/member; membership result. |
| `identity.organization.team.member-remove` | `organization` / both | org:team-write / CSRF | admin / repeat | Team/member; removed/already absent. |
| `identity.sso.provider.register-oidc` | `sso`, `sso/oidc` / both | org:sso-write / CSRF | admin / keyed | OIDC discovery/static metadata, domains and mappings; safe provider and verification requirements. |
| `identity.sso.provider.register-oauth` | `sso`, `sso/oauth2` / both | org:sso-write / CSRF | admin / keyed | OAuth endpoints, identity proof, domains and mappings; safe provider and verification requirements. |
| `identity.sso.provider.register-saml` | `sso`, `sso/saml` / both | org:sso-write / CSRF | admin / keyed | Bounded IdP metadata or explicit SAML config, domains and mappings; safe provider and SP metadata. |
| `identity.sso.provider.list` | `sso` / both | org:sso-read / none | safe / read | Organization/cursor; safe providers. |
| `identity.sso.provider.get` | `sso` / both | org:sso-read / none | safe / read | Provider ID; redacted provider details. |
| `identity.sso.provider.update` | `sso` / both | org:sso-write / CSRF | admin / keyed | Versioned config/mappings/key rotation; update result. |
| `identity.sso.provider.delete` | `sso` / both | org:sso-write / CSRF | admin / repeat | Provider/version; disable/delete and active-login race outcome. |
| `identity.sso.domain-challenge` | `sso`, `organization` / both | org:sso-write / CSRF | admin / rotate | Domain; reveal-once proof challenge. |
| `identity.sso.domain-verify` | `sso`, `organization` / both | org:sso-write / CSRF | provider / keyed | Domain/challenge evidence; verified ownership or conflict/unavailable. |
| `identity.sso.discover` | `sso` / both | public / none | auth / read | Email/domain; returns safe provider choice without organization enumeration. |
| `identity.sso.signin-start` | `sso` / both | public plus pre-auth transaction / origin | provider / keyed | Provider/domain, callbacks and login intent; protocol redirect. |
| `identity.sso.oidc-callback` | `sso/oidc` / protocol | pre-auth-bound OIDC state / origin | provider / once | Code/error/state/issuer; session/provision/link result or mix-up/replay denial. |
| `identity.sso.oauth-callback` | `sso/oauth2` / protocol | pre-auth-bound OAuth state / origin | provider / once | Code/error/state; session/provision/link result. |
| `identity.sso.saml-metadata` | `sso/saml` / protocol | public / none | safe / read | Provider/profile; bounded SP metadata advertising the exact SP-initiated ACS and, only when enabled, distinct IdP-initiated response URL. |
| `identity.sso.saml-start` | `sso/saml` / protocol | public plus pre-auth transaction / origin | provider / keyed | Provider/callback; HTTP-Redirect request signed with the configured `SigAlg` over exact `SAMLRequest=value&RelayState=value&SigAlg=value` order and bound relay state; the no-RelayState form is `SAMLRequest=value&SigAlg=value`. |
| `identity.sso.saml-acs` | `sso/saml` / protocol | pre-auth-bound SAML response / origin | provider / once | HTTP-POST Response at the exact configured SP ACS URL; Destination/Recipient equal that URL and InResponseTo/relay state bind the outstanding SP-initiated request. |
| `identity.sso.saml-idp-init` | `sso/saml` / protocol | permitted unsolicited response plus relay state / origin | provider / once | HTTP-POST Response at the distinct configured IdP-initiated URL; disabled by default and, when enabled, requires exact Destination/Recipient, unsolicited-response allowlist and login-CSRF/confirmation policy without fabricating InResponseTo. |
| `identity.sso.saml-logout-start` | `sso/saml`, `identity/session` / protocol | session plus provider context / CSRF | provider / keyed | Provider, bounded session and allowlisted post-logout target; signs a LogoutRequest, binds relay state and returns redirect/front-channel result or explicit local-only disposition. |
| `identity.sso.saml-slo` | `sso/saml`, `identity/session` / protocol | signed SAML logout message plus relay state / origin | provider / once | Bounded LogoutRequest or LogoutResponse, issuer and relay state; verifies signature/time/replay/session binding, performs the configured local/federated revocation and returns a signed response or allowlisted redirect. |
| `identity.sso.directory-sync-start` | `sso` / both | org:sso-write / CSRF | admin / keyed | Enterprise provider, mapping version, bounded source cursor and dry-run/apply mode; returns an immutable sync generation. |
| `identity.sso.directory-sync-apply` | `sso` / direct | internal / none | internal / keyed | Verified generation and bounded batch of user/group/role deltas; atomically applies owned mappings or records reconciliation-required outcomes without restoring locally removed authority. |
| `identity.sso.directory-sync-status` | `sso` / both | org:sso-read / none | safe / read | Generation/cursor; returns bounded counts, checkpoint, limitations and reconciliation state. |
| `identity.sso.directory-sync-cancel` | `sso` / both | org:sso-write / CSRF | admin / repeat | Generation/version; stops new batches while preserving applied checkpoints and reconciliation evidence. |
| `identity.scim.connection-create` | `scim`, `scim/organization` / both | org:scim-write / CSRF | admin / keyed | Provider/org/mappings; reveal-once bearer and safe connection. Personal connections are forbidden. |
| `identity.scim.connection-list` | `scim`, `scim/organization` / both | org:scim-read / none | safe / read | Organization/cursor; safe connection page. |
| `identity.scim.connection-get` | `scim`, `scim/organization` / both | org:scim-read / none | safe / read | Provider connection; redacted details. |
| `identity.scim.connection-rotate` | `scim`, `scim/organization` / both | org:scim-write / CSRF | secret / rotate | Connection/version; reveal-once successor and old-token disposition. |
| `identity.scim.connection-delete` | `scim`, `scim/organization` / both | org:scim-write / CSRF | admin / repeat | Connection/version; immediate bearer revocation. |
| `identity.scim.service-provider-config` | `scim` / protocol | scim-token / none | protocol / read | Exact feature support and effective `scim.page_max`, `scim.bulk.operations`, and decoded `scim.bulk.bytes`. |
| `identity.scim.schemas-list` | `scim` / protocol | scim-token / none | protocol / read | Schema list bounded by effective `scim.resource_depth`, `scim.resource_attributes`, and `scim.string_bytes`. |
| `identity.scim.schema-get` | `scim` / protocol | scim-token / none | protocol / read | Schema URN bounded by `scim.string_bytes`; exact schema bounded by `scim.resource_depth` and `scim.resource_attributes`, or SCIM not-found. |
| `identity.scim.resource-types-list` | `scim` / protocol | scim-token / none | protocol / read | Resource types bounded by effective `scim.resource_depth`, `scim.resource_attributes`, and `scim.string_bytes`. |
| `identity.scim.resource-type-get` | `scim` / protocol | scim-token / none | protocol / read | Resource type ID bounded by `scim.string_bytes`; exact metadata bounded by `scim.resource_depth` and `scim.resource_attributes`. |
| `identity.scim.user-list` | `scim` / protocol | scim-token / none | protocol / read | RFC filter/sort/page under `scim.page_default`, `scim.page_max`, `scim.filter_bytes`, `scim.filter_tokens`, `scim.filter.depth`, `scim.filter.nodes`, and `scim.path_bytes`; exact scoped snapshot response. |
| `identity.scim.user-get` | `scim` / protocol | scim-token / none | protocol / read | Resource ID; scoped resource and ETag. |
| `identity.scim.user-create` | `scim`, `scim/organization` / protocol | scim-token / none | protocol / keyed | User under `scim.resource_depth`, `scim.resource_attributes`, and `scim.string_bytes`; password rejected in the reference profile; resource or uniqueness/mapping conflict. |
| `identity.scim.user-replace` | `scim`, `scim/organization` / protocol | scim-token / none | protocol / keyed | If-Match and representation under `scim.resource_depth`, `scim.resource_attributes`, and `scim.string_bytes`; password rejected in the reference profile; updated resource/ETag or precondition failure. |
| `identity.scim.user-patch` | `scim`, `scim/organization` / protocol | scim-token / none | protocol / keyed | Atomic PATCH under `scim.patch.operations`, `scim.path_bytes`, `scim.resource_depth`, `scim.resource_attributes`, and `scim.string_bytes` plus If-Match; password rejected in the reference profile; updated resource/ETag or SCIM path/value error. |
| `identity.scim.user-delete` | `scim`, `scim/organization` / protocol | scim-token / none | protocol / keyed | Resource/If-Match, idempotency key and deprovision policy; tombstone-backed replay of the original outcome. |
| `identity.scim.group-list` | `scim`, `scim/organization` / protocol | scim-token / none | protocol / read | RFC filter/sort/page under `scim.page_default`, `scim.page_max`, `scim.filter_bytes`, `scim.filter_tokens`, `scim.filter.depth`, `scim.filter.nodes`, and `scim.path_bytes`; exact scoped snapshot response. |
| `identity.scim.group-get` | `scim`, `scim/organization` / protocol | scim-token / none | protocol / read | Group ID; resource and ETag. |
| `identity.scim.group-create` | `scim`, `scim/organization` / protocol | scim-token / none | protocol / keyed | Group under `scim.resource_depth`, `scim.resource_attributes`, `scim.string_bytes`, and `scim.group_members`; mapped team/group result. |
| `identity.scim.group-replace` | `scim`, `scim/organization` / protocol | scim-token / none | protocol / keyed | Group under `scim.resource_depth`, `scim.resource_attributes`, `scim.string_bytes`, and `scim.group_members` plus If-Match; resource/ETag. |
| `identity.scim.group-patch` | `scim`, `scim/organization` / protocol | scim-token / none | protocol / keyed | Atomic group PATCH under `scim.patch.operations`, `scim.path_bytes`, `scim.resource_depth`, `scim.resource_attributes`, `scim.string_bytes`, and `scim.group_members` plus If-Match; resource/ETag. |
| `identity.scim.group-delete` | `scim`, `scim/organization` / protocol | scim-token / none | protocol / keyed | Group/If-Match and idempotency key; tombstone-backed replay of the mapping deletion result. |
| `identity.scim.bulk` | `scim` / protocol | scim-token / none | protocol / keyed | Effective `scim.bulk.operations`, `scim.bulk.bytes`, `scim.bulk.fail_on_errors`, `scim.bulk.operation_bytes`, and `scim.bulk.response_bytes`; deterministic ordered replay from independently committed child checkpoints. |

## OAuth/OIDC authorization server and device operations

| Operation ID | Owner / exposure | Access / CSRF | Rate / idempotency | Request, result, and additional error semantics |
| --- | --- | --- | --- | --- |
| `identity.oauth-server.client-create` | `oauth-server` / both | authorized user/org/admin / CSRF | admin / keyed | Owner/reference and metadata; public client or reveal-once confidential secret. |
| `identity.oauth-server.client-get` | `oauth-server` / both | owner/admin / none | safe / read | Client ID; private owner view. |
| `identity.oauth-server.client-get-public` | `oauth-server` / both | session / none | safe / read | Client ID; safe public metadata for an authenticated user, with disabled and unknown clients returning the same bounded not-found result. |
| `identity.oauth-server.client-get-public-prelogin` | `oauth-server` / both | option-enabled public authorization transaction / none | auth / read | Client ID plus signed authorize context; returns the smaller pre-login display projection without owner/private metadata, and is unavailable unless the explicit pre-login profile is enabled. |
| `identity.oauth-server.client-list` | `oauth-server` / both | owner/admin / none | safe / read | Owner/cursor; scoped client page. |
| `identity.oauth-server.client-update` | `oauth-server` / both | owner/admin / CSRF | admin / keyed | Metadata/version; validated updated client. |
| `identity.oauth-server.client-rotate-secret` | `oauth-server` / both | fresh owner/admin / CSRF | secret / rotate | Client/version; reveal-once successor and overlap state. |
| `identity.oauth-server.client-delete` | `oauth-server` / both | owner/admin / CSRF | admin / repeat | Client/version; deletion/revocation result. |
| `identity.oauth-server.dynamic-register` | `oauth-server` / protocol | enabled-profile initial access token / none | protocol / keyed | RFC 7591 client metadata plus immutable tenant/organization/platform owner and unique scopes contained in `oauth_server.dynamic_registration.allowed_scopes`; unavailable unless the explicit dynamic-registration profile is enabled. Enabling this operation selects RFC 7591 only; RFC 7592 remains an unselected future profile, so no registration management URI or access token is returned. |
| `identity.oauth-server.authorize` | `oauth-server` / protocol | session or interaction-required / origin | protocol / keyed | OAuth authorization request; signed expiring continuation, redirect error, login/account/consent outcome, or code. |
| `identity.oauth-server.continue` | `oauth-server` / protocol | session plus continuation / CSRF | protocol / once | Tamper-evident continuation and selected account/decision; resumed authorization or stale/replay denial. |
| `identity.oauth-server.consent-get` | `oauth-server` / both | session / none | safe / read | Client/grant context; current bounded consent. |
| `identity.oauth-server.consent-list` | `oauth-server` / both | session / none | safe / read | Cursor; consent page. |
| `identity.oauth-server.consent-update` | `oauth-server` / both | fresh / CSRF | secret / keyed | Client/scopes/claims/audiences/version; updated narrow consent. |
| `identity.oauth-server.consent-delete` | `oauth-server` / both | fresh / CSRF | secret / repeat | Consent/version; revoked grants outcome. |
| `identity.oauth-server.token` | `oauth-server` / protocol | oauth-client as grant requires / none | protocol / rotate | Authorization-code, refresh-token or client-credentials grant; token response or standard OAuth error. |
| `identity.oauth-server.introspect` | `oauth-server` / protocol | authorized resource client / none | protocol / read | Token and hint; active metadata bounded to caller audience. |
| `identity.oauth-server.revoke` | `oauth-server` / protocol | owning oauth-client / none | protocol / repeat | Token and hint; non-enumerating success and family policy. |
| `identity.oauth-server.end-session` | `oauth-server/oidc`, `identity/session` / protocol | ID-token/session context / origin | protocol / repeat | Client/post-logout URI/state; exact session logout or denial, never cross-client global logout; emits exactly `identity.oauth_server.end_session`. |
| `identity.oauth-server.userinfo` | `oauth-server/oidc` / protocol | OAuth access token / none | protocol / read | Token; consented subject claims or invalid token/scope/audience. |
| `identity.oauth-server.discovery-oauth` | `oauth-server` / protocol | public / none | safe / read | Exact OAuth authorization-server metadata, including `scopes_supported` equal to the canonical `oauth_server.scopes` catalog. |
| `identity.oauth-server.discovery-oidc` | `oauth-server/oidc` / protocol | public / none | safe / read | Exact OpenID provider metadata. |
| `identity.oauth-server.jwks` | `oauth-server/oidc` / protocol | public / none | safe / read | Active public keys only with cache/rotation metadata. |
| `identity.oauth-server.session-token` | `oauth-server/oidc` / both | fresh session / CSRF | secret / keyed | Configured audience/scope/claims; bounded JWT no stronger/longer than source session; emits exactly `identity.oauth_server.exchange_session`. |
| `identity.oauth-server.protected-resource-metadata` | `oauth-server` / protocol | public / none | safe / read | RFC 9728 metadata with `resource` byte-for-byte equal to the canonical `oauth_server.protected_resource.resource` origin, exact authorization-server issuer set, supported bearer methods and `scopes_supported` equal to `oauth_server.protected_resource.supported_scopes`; values MUST match token audience/resource validation and the configured well-known URL. |
| `identity.oauth-server.resource-verify` | `oauth-server` / direct | OAuth access token / internal | internal / read | JWT/JWKS or introspection profile; requires audience/resource byte-for-byte equal to `oauth_server.protected_resource.resource` and returns a scope-bound principal or typed invalid/unavailable. |
| `identity.oauth-server.device-authorize` | `oauth-server/device` / protocol | oauth-client / none | protocol / keyed | Client/scopes and optional authorized pre-binding; device/user codes and verification URIs. |
| `identity.oauth-server.device-inspect` | `oauth-server/device` / both | fresh session plus user code / none | auth / read | User code; safe client/scope prompt without device enumeration. |
| `identity.oauth-server.device-approve` | `oauth-server/device` / both | fresh session / CSRF | auth / once | User code/consent; approved binding or stale/expired. |
| `identity.oauth-server.device-deny` | `oauth-server/device` / both | fresh session / CSRF | auth / once | User code; denied state; emits exactly `identity.oauth_server.deny_device`. |
| `identity.oauth-server.device-token` | `oauth-server/device`, `oauth-server` / protocol | oauth-client plus device code / none | protocol / none | Poll request; pending/slow_down/denied/expired or token response; emits exactly `identity.oauth_server.poll_device`. |

## Cross-cutting direct APIs and middleware

| Operation ID | Owner / exposure | Access / CSRF | Rate / idempotency | Request, result, and additional error semantics |
| --- | --- | --- | --- | --- |
| `identity.risk.evaluate` | `identity/risk` / direct | explicit actor/network facts plus trusted issuance phase and binding / internal | internal / keyed | Action, subject, authoritative server-resolved carrier facts, signals, attempt ID, and issuance phase. Issuance phase is exactly `none`, `phone-reset-initiation`, or `phone-reset-completion`: `none` is non-issuing, phase `phone-reset-initiation` maps only to purpose `phone-password-reset-initiate`, and phase `phone-reset-completion` maps only to purpose `phone-password-reset-complete`. Purpose is derived exclusively from the two issuing phases; unknown/unsupported phases, a purpose with `none`, and any caller-supplied purpose are rejected before provider evaluation or state access. Issuing phases bind tenant, subject, recovery operation, canonical number, pre-auth transaction, attempt ID, and risk-policy version. An allowed issuance returns an opaque RiskEvidence reference plus purpose, issued-at, expires-at, and one-use metadata, never raw signals, provider evidence, decision internals, embedded evidence payloads, digests, signatures, or journal identifiers. Denied returns no reference; Failed proves no issuance; Unknown returns no reference and requires same-command recovery; the same command and fingerprint replay the exact recorded result without issuing another artifact. Callers cannot fabricate authoritative facts, phase, purpose, decision, or evidence. Non-issuing evaluations return the deterministic allow/deny/throttle/step-up result. |
| `identity.risk.captcha-verify` | `identity/risk/captcha` plus adapter / middleware | route challenge / none | auth / once | Provider token and server-bound action/site/origin; attributable evidence or invalid/unavailable. |
| `identity.risk.hibp-check` | `identity/risk/hibp` / direct | password workflow / internal | internal / read | Password-derived prefix only leaves process; breach count/evidence or unavailable/ambiguous. |
| `identity.delivery.enqueue` | `identity/delivery` / direct | owning workflow / internal | internal / keyed | Versioned typed intent; queued/no-op/failure/unknown result, never false delivered. |
| `identity.i18n.resolve` | `identity/i18n` / direct | explicit/session/cookie/header context / internal | internal / read | Locale inputs and stable error; localized envelope retaining machine/original identity. |
| `identity.hooks.before` | `identity`, `identity/http` / direct | operation context / internal | internal / none | Ordered validation/enrichment with documented cancellation/transaction boundary. |
| `identity.hooks.after` | `identity`, `identity/http` / direct | attempted/committed result / internal | internal / keyed | Ordered observer result; no fire-and-forget and no rollback of committed state. |
| `identity.openapi.document` | `identity/http` / both | configured public/admin policy / none | safe / read | OpenAPI 3.1.1 document matching the exact registered operation manifest. |
| `identity.reference.config-validate` | `identity/reference` / direct | operator / internal | internal / read | Complete configuration; redacted deterministic validation report. |
| `identity.reference.migration-status` | `identity/reference` / direct | operator / internal | internal / read | Selected modules; current/required versions and compatibility state. |
| `identity.reference.migration-plan` | `identity/reference` / direct | operator / internal | internal / read | Selected modules/current versions; ordered forward plan and digest. |
| `identity.reference.migration-apply` | `identity/reference` / direct | operator / internal | internal / keyed | Plan digest/lock policy; checkpointed apply result or dirty/locked/unknown. |
| `identity.reference.schema-generate` | `identity/reference` / direct | operator / internal | internal / read | Selected modules and target version; deterministic ordered schema/migration artifacts with source/version digests. |
| `identity.reference.secret-generate` | `identity/reference` / direct | operator / internal | internal / none | Secret profile; reveal-once generated value and safe metadata. |
| `identity.reference.diagnostics` | `identity/reference` / direct | operator / internal | internal / read | Runtime/config context; bounded fully redacted summary. |
| `identity.identitytest.user-create` | `identity/identitytest` / direct | test build only / internal | internal / keyed | Deterministic factory input; test identity. MUST be impossible to register in production. |
| `identity.identitytest.user-save` | `identity/identitytest` / direct | test build only / internal | internal / keyed | Explicit test identity; persisted result using public repository contracts. |
| `identity.identitytest.user-delete` | `identity/identitytest` / direct | test build only / internal | internal / repeat | Test identity ID; bounded deletion in the isolated namespace. |
| `identity.identitytest.organization-create` | `identity/identitytest` / direct | test build only / internal | internal / keyed | Deterministic factory input; test organization. |
| `identity.identitytest.organization-save` | `identity/identitytest` / direct | test build only / internal | internal / keyed | Explicit organization; persisted result. |
| `identity.identitytest.organization-delete` | `identity/identitytest` / direct | test build only / internal | internal / repeat | Organization ID; isolated deletion result. |
| `identity.identitytest.member-add` | `identity/identitytest` / direct | test build only / internal | internal / keyed | Organization/user/role fixture; membership result. |
| `identity.identitytest.login` | `identity/identitytest` / direct | test build only / internal | internal / keyed | Test identity and session profile; headers/cookies/session. |
| `identity.identitytest.auth-headers` | `identity/identitytest` / direct | test build only / internal | internal / read | Session/key fixture; request headers without logging credentials. |
| `identity.identitytest.cookies` | `identity/identitytest` / direct | test build only / internal | internal / read | Session fixture; cookie jar matching the selected HTTP profile. |
| `identity.identitytest.cookie-create` | `identity/identitytest` / direct | test build only / internal | internal / read | Session token, selected cookie profile and optional bounded domain; correctly signed test cookie records with the production attributes, without exposing the signing secret. |
| `identity.identitytest.cookie-headers` | `identity/identitytest` / direct | test build only / internal | internal / read | Session token and selected cookie profile; request headers containing the correctly signed cookie and no unrelated ambient state. |
| `identity.identitytest.delivery-capture` | `identity/identitytest` / direct | test build only / internal | internal / read | Intent selector; captured message/OTP without production send. |
| `identity.identitytest.otp-get` | `identity/identitytest` / direct | test build only / internal | internal / read | Exact isolated identifier and optional purpose; captured OTP or explicit absent result without cross-instance lookup. |
| `identity.identitytest.otp-clear` | `identity/identitytest` / direct | test build only / internal | internal / repeat | Exact isolated test instance; clears only its captured OTP values and returns the bounded count. |
| `identity.identitytest.store-conformance` | `identity/identitytest` / direct | test build only / internal | internal / read | Consumer-supplied identity-store factory and declared capabilities; registers the reusable CRUD, auth-flow, case-insensitive identifier, join, numeric-ID, UUID and transaction conformance suites with isolated cleanup. |
| `identity.identitytest.reset` | `identity/identitytest` / direct | test build only / internal | internal / repeat | Test namespace; bounded state reset with parallel isolation. |

## Closure requirements

1. `identity/http` MUST materialize the `both`, `protocol`, and `middleware`
   dispositions in one machine-readable endpoint manifest. It MUST reject a
   missing or duplicate operation ID.
2. Every `direct` operation MUST have a public Go contract and clean-consumer
   proof. A direct operation MUST NOT be silently exposed over HTTP.
3. Every operation that can issue a session MUST accept the selected remember
   policy and preserve it through MFA or other continuation steps.
4. Redirect-bearing operations MUST distinguish success, new-user,
   cancellation, and error targets where applicable and MUST validate each
   target before storing state.
5. The social-provider support matrix MUST state which profiles implement
   `identity.oauth.signin-token`; code/PKCE support does not imply direct-token
   support.
6. Account deletion, password reset, verification, invitations, transfers, and
   every other capability-backed operation MUST prove concurrent single
   consumption through the selected PostgreSQL capability adapter.
7. Every successful enterprise SSO callback for an existing provider subject
   MUST apply the versioned repeat-login synchronization policy before session
   issuance; protocol adapters return claim provenance and MUST NOT bypass or
   independently persist that policy.
8. OpenAPI completeness, generated-client smoke tests, HTTP contract tests, and
   the final reference journeys MUST derive expected operation IDs from this
   catalog. A hand-maintained subset is insufficient.
