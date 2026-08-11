# Identity Platform End-State Acceptance

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Product definition

The finished program MUST compose into a complete backend identity product,
not a collection of primitives. A reference server MUST expose every in-scope
journey through standard-library `net/http`, persist durable state in
PostgreSQL, use Valkey for selected ephemeral/distributed state, publish an
OpenAPI 3.1.1 document, and require no undocumented application glue beyond
deployment configuration, provider credentials, templates, and policy choices.

The reference server is verification and adoption material, not an admin UI.
Its handlers MUST call public package contracts exactly as a consumer would.

## Required composed journeys

1. **Identity lifecycle:** create, retrieve, update, suspend/ban with expiry
   and reason, restore, anonymize and delete users; manage verified identifiers
   and linked accounts; preserve tenant isolation and auditable outcomes.
2. **Email, password and username:** signup with email/password/username,
   verify email, signin by email or username, logout, forgot/reset/change
   password, automatic hash upgrade, HIBP policy, username availability and
   rename, enumeration resistance, and session revocation after sensitive
   changes.
3. **Sessions:** secure cookie issuance, validation, freshness, rotation,
   absolute/idle expiry, device labels, multiple simultaneous accounts in one
   client, active-account switching, configured maximum, list/revoke one/all,
   logout idempotency, and last-login-method track/get/check/clear.
4. **Passwordless:** magic-link, email OTP, verified phone and phone OTP signin;
   anonymous-session creation and collision-safe upgrade to a permanent user.
5. **MFA and WebAuthn:** enroll/challenge/remove TOTP, OTP, recovery codes,
   trusted devices and security keys; passkey-first signup, discoverable and
   usernameless signin, credential listing/renaming/removal, step-up and safe
   recovery without factor bypass.
6. **Social OAuth:** generic authorization-code/PKCE provider registration,
   every built-in provider profile, callback and token refresh, safe account
   linking/unlinking, Google One Tap prompt/button modes, and preview OAuth
   proxy without production identity writes.
7. **API keys:** create a scoped user or organization key, reveal once,
   authenticate, list metadata, rename, rotate, expire and revoke it.
8. **Administration:** explicitly authorized user search/management, bans,
   role/permission decisions, session control, credential reset and bounded
   impersonation with actor chain and immutable audit.
9. **Organizations:** create/update/delete, invite/accept/reject/cancel, member
   lifecycle, roles and permissions, teams, ownership transfer, active
   organization and configured limits under concurrent changes.
10. **Enterprise federation:** domain verification and provider selection;
    OIDC, OAuth 2 and SAML login; JIT provisioning; organization assignment;
    attribute/role mapping; linking; provider lifecycle; replay and downgrade
    denial.
11. **SCIM 2.0:** authenticated discovery, Users and Groups CRUD, filter,
    pagination, PATCH, bulk limits, ETags, organization mapping, soft/hard
    deprovisioning and reconciliation.
12. **OAuth/OIDC provider:** register clients, authorize with consent and PKCE,
    exchange and refresh with rotation, revoke/introspect, discover metadata
    and JWKS, validate ID token/UserInfo at an independent relying party, and
    complete device authorization including denial and slow polling.
13. **Abuse controls:** action-specific rate and velocity decisions, CAPTCHA
    challenges through reCAPTCHA, Turnstile, hCaptcha and CaptchaFox, replay
    denial, lockout/step-up, provider outage policy and recovery.
14. **Localization:** negotiate supported locale by explicit choice, session,
    cookie and `Accept-Language`; return stable localized public errors while
    retaining machine codes and original diagnostic identity.
15. **Developer use:** start an isolated reference stack, apply migrations,
    create deterministic test identities, capture delivery/OTP without real
    sends, authenticate requests, reset state, run parallel tests, consume the
    OpenAPI document, and prove test helpers cannot register production routes.

## HTTP and browser-facing contract

`identity/http` MUST own route composition, handler lifecycle, request limits,
content types, stable error envelopes, security headers, trusted proxy/origin
policy, CORS, CSRF, redirect allowlists, cookie attributes and prefixes, cache
policy, middleware/hooks, route collision detection, and idempotency surfaces.
Cookies MUST have explicit `Secure`, `HttpOnly`, `SameSite`, domain, path,
partition/cross-site and rotation semantics. State-changing cookie-authenticated
requests MUST have CSRF protection. Bearer endpoints MUST not inherit unsafe
cookie assumptions.

Every public operation MUST appear exactly once in OpenAPI 3.1.1 with bounded
schemas, security, errors and examples. Handler registration and schema
composition MUST fail on duplicate routes, operation IDs or incompatible
components. Extension packages MUST integrate through typed composition, not
global registration or reflection.

## Persistence and failure recovery

- PostgreSQL migrations MUST be forward-only, resumable, compatible with
  concurrent old/new binaries where promised, and enforce race-sensitive
  uniqueness and referential invariants in the database.
- Valkey usage MUST define namespace, TTL, eviction, atomic scripts/commands,
  cluster behavior, failover and the consequence of lost ephemeral state.
- Every multi-store or provider transition MUST distinguish not committed,
  committed and unknown outcomes and define retry, idempotency and
  reconciliation. No unknown outcome may be reported as success.
- Outbox/audit emission, revocation, token consumption and identity state MUST
  have explicit atomic boundaries. Cleanup MUST be bounded, cancellable and
  safe under concurrent workers.
- Backup/restore and migration rehearsal MUST prove that identities, sessions,
  organization links, grants and key material retain or intentionally change
  validity according to documented policy.

## Security and privacy properties

All parsers and cryptographic boundaries MUST be bounded and fuzzed. Secrets,
credentials, tokens, provider responses, PII and recovery material MUST remain
redacted from logs, traces, metrics, errors, fixtures and evidence. Recoverable
secrets MUST be envelope-encrypted with context and rotation; lookup tokens
MUST be digested where possible. Tenant, organization, audience, purpose,
redirect, origin and actor scope MUST fail closed.

The integrated threat suite MUST cover enumeration, fixation, replay, token
substitution, confused deputy, open redirect, CSRF, CORS/origin bypass, SSRF,
algorithm/key confusion, downgrade, account-link takeover, privilege
escalation, cross-tenant access, race-driven duplication, resource exhaustion,
and sensitive-data disclosure. Privacy proof MUST cover consent/notice for
last-login and trusted-device data, retention, export, deletion/anonymization,
audit/legal-hold boundaries and external-provider deletion limitations.

## Operability and provider proof

Every external operation MUST have bounded context, timeout, retry and
concurrency policy. Metrics and traces MUST identify safe operation/result
dimensions without unbounded or sensitive labels. Runbooks MUST cover key and
client-secret rotation, provider outage, webhook/callback anomalies, replay
store degradation, PostgreSQL/Valkey failover, migration rollback boundary,
compromise-driven global revocation and reconciliation of unknown outcomes.

Specification and provider claims require pinned official fixtures plus an
independent implementation or documented sandbox/live proof where available.
Fakes prove deterministic behavior only. Each supported social provider,
CAPTCHA adapter, enterprise protocol, SCIM, OAuth/OIDC and WebAuthn profile
MUST retain attributable current evidence or remain explicitly unverified.

## Final acceptance gates

The coordinator MUST prove all journeys above against the final integrated
branch, run every input-invalidated package and reverse-dependant gate, then run
the repository release gates required for all affected modules. Exact coverage
and mutation requirements, race/fuzz/leak/benchmark gates, clean-consumer
proof, API compatibility, documentation compilation, dependency/license,
vulnerability/secret, SBOM and provenance checks MUST pass where required.

The program MUST NOT be called complete while any inventory unit, parity row,
journey, provider profile, security property, migration/recovery profile,
review finding or required gate is partial, skipped, stale, warning-only,
failing, or dependent on unrecorded application behavior.
