# Identity Platform End-State Acceptance

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Product definition

The finished program MUST compose into a complete backend identity product,
not a collection of primitives. `identity/reference` MUST assemble a server
that exposes every in-scope
journey through standard-library `net/http`, persist durable state in
PostgreSQL, use Valkey for selected ephemeral/distributed state, publish an
OpenAPI 3.1.1 document, and require no undocumented application glue. The only
application-supplied seams MAY be deployment configuration, provider
credentials, templates, explicit policy callbacks and bounded email/SMS
`Sender` implementations equivalent to Better Auth delivery callbacks.
Consumers MUST NOT supply domain stores, workflow orchestration, handlers,
migrations or security middleware.

The selected PostgreSQL end state MUST use the dedicated identity, password,
session, risk, OTP, MFA, WebAuthn, passkey, social-OAuth, API-key,
impersonation, organization, SSO, SCIM and OAuth-server adapters; an in-memory
store or consumer-written adapter is not acceptable proof. Capability-backed
one-time workflows MUST use the existing PostgreSQL capability consumption
adapter.

`identity/http` owns the reusable transport and MUST NOT import concrete
storage/provider adapters. `identity/reference` owns the all-adapters deployment
profile and cross-feature proof. The reference server is verification and
adoption material, not an admin UI.
Its handlers MUST call public package contracts exactly as a consumer would.

## Required composed journeys

1. **Identity lifecycle:** create, retrieve, update, suspend/ban with expiry
   and reason, restore, anonymize and delete users; manage verified identifiers
   and linked accounts; preserve tenant isolation and auditable outcomes;
   round-trip declared typed additional fields with per-field input, output,
   write, sensitivity and migration policy.
   Deletion MUST exercise the configured hard-delete/anonymize boundary and
   lifecycle cascades for sessions, credentials, identifiers, linked provider
   accounts, organization memberships/invitations/teams, SSO/SCIM mappings,
   API keys, grants, delivery/outbox/workflow state and retained audit/legal
   holds; no dangling authority or cross-tenant disclosure may remain.
   Self-service deletion MUST prove the closed credential-specific policy:
   current password plus fresh session for password users, a fresh UV passkey
   for passkey users, a purpose/session/version-bound emailed capability for
   provider-only users with a verified email, or fresh UV passkey or audited
   administrator recovery when no verified email exists, including expiry,
   replay, callback and unknown-outcome recovery.
2. **Email, password and username:** signup with email/password/username,
   verify email, signin by email or username, logout, forgot/reset/change
   password, automatic hash upgrade, HIBP policy, username availability and
   rename, enumeration resistance, and session revocation after sensitive
   changes.
   Sensitive changes MUST exercise explicit reauthentication with a fresh-
   session proof, expiry, cancellation, replay denial and account/session
   binding rather than treating an old authenticated session as sufficient.
   The standalone password-verification operation MUST also be exercised
   directly: it returns only a short-lived purpose/audience-bound proof, never
   creates or extends a session, and denies expiry, replay and cross-purpose
   use without account enumeration.
3. **Sessions:** secure cookie issuance, validation, freshness, rotation,
   absolute/idle expiry, device labels, multiple simultaneous accounts in one
   client, active-account switching, configured maximum, list/revoke one/all,
   logout idempotency, and last-login-method track/get/check/clear. The same
   contract suite MUST cover durable opaque sessions, Valkey/secondary storage,
   bounded cookie caching and signed/encrypted stateless sessions with explicit
   versioning/global invalidation. Custom session enrichment MUST define
   schema, timeout, failure, caching, invalidation and redaction.
   The same session MUST authenticate through an explicit bearer transport
   profile without cookie fallback or weakened revocation/freshness.
   A fresh session MUST generate a three-minute single-use transfer token for
   an exact trusted target origin; that origin MUST consume it once to obtain
   the same still-valid session under explicit cookie/no-cookie policy, while
   replay, wrong-origin and revoked-source attempts fail.
   Cookie issuance MUST exercise `rememberMe` enabled and disabled: the former
   uses the configured persistent lifetime bounded by session expiry, while the
   latter is a non-persistent browser-session cookie; rotation, logout,
   revocation and account switching MUST preserve the choice without weakening
   `Secure`, `HttpOnly`, `SameSite`, prefix or CSRF policy.
   Every session-issuing mechanism, including passwordless, social, passkey,
   anonymous and enterprise SSO, MUST accept the same session-owned remember
   policy and preserve it unchanged through risk, MFA, protocol and callback
   continuations.
4. **Passwordless:** magic-link, email OTP, verified phone and phone OTP signin;
   anonymous-session creation, authenticated anonymous self-deletion without a
   permanent-account proof, and collision-safe upgrade to a permanent user.
5. **MFA and WebAuthn:** enroll/challenge/remove TOTP, OTP, recovery codes,
   trusted devices and security keys; passkey-first signup, discoverable and
   usernameless signin, credential listing/renaming/removal, step-up and safe
   recovery without factor bypass.
   Recovery-code plaintext MUST be returned only at creation or full
   regeneration; later reads expose status/count only, and regeneration
   atomically invalidates the prior set.
6. **Social OAuth:** generic authorization-code/PKCE provider registration,
   every built-in provider profile, callback and token refresh, safe account
   linking/unlinking, Google One Tap prompt/button modes, and preview OAuth
   proxy without production identity writes, plus a real-browser popup flow
   with exact opener-origin binding and redirect-flow-equivalent results.
   The same account/linking/session contract MUST be proved for native
   provider-token signin without a browser redirect, including issuer,
   audience/authorized-party, nonce, expiry, signature/key rotation, replay,
   token-substitution, unverified-email and collision handling.
7. **API keys:** create a scoped user or organization key, reveal once,
   authenticate, list metadata, rename, rotate, expire and revoke it.
8. **Administration:** explicitly authorized user search/management, bans,
   role/permission decisions, session control, credential reset and bounded
   impersonation with actor chain and immutable audit.
   A clean tenant MUST bootstrap its first administrator exactly once through
   the out-of-band, short-lived bootstrap capability; concurrent/replayed
   attempts, non-empty authority state, wrong tenant and partial/unknown
   outcomes MUST fail closed and leave an immutable audit trail.
   The resulting platform-administrator role and subject assignment MUST be
   durable, versioned authorization state, not configuration or identity-row
   metadata; later assignment changes MUST use explicit authorized operations,
   invalidate stale authority and compose atomically with audit/outbox state.
   The composed acceptance proof MUST drive all six platform-authority
   operations through the reference `net/http` handlers and public service
   contracts without an admin UI. An allow and deny decision MUST be captured
   before each successful mutation; the prior-version decision MUST be denied
   after the authority-version increment, while a newly evaluated decision
   reflects the committed role and statement graph. Each rejected transition
   MUST leave role, statement and authority versions unchanged; its safe
   conflict/denial audit MUST NOT imply that a mutation outbox event committed.

   | Operation | Successful state transition | Required rejection transition | Successful authority transition |
   | --- | --- | --- | --- |
   | `identity.platform.role.create` | absent -> role version 1 with validated statement bindings | duplicate ID or unknown statement -> conflict; state unchanged | authority version A -> A+1; prior-version decisions rejected; cached decisions invalidated; audit/outbox commit atomically |
   | `identity.platform.role.update` | role version N -> N+1 with replacement statement bindings | stale expected role version -> conflict; state unchanged | authority version A -> A+1; prior-version decisions rejected; cached decisions invalidated; audit/outbox commit atomically |
   | `identity.platform.role.delete` | role version N -> absent after assignment disposition | stale expected role version or in-use without explicit bounded assignment removal/reassignment -> conflict; state unchanged | authority version A -> A+1; prior-version decisions rejected; cached decisions invalidated; audit/outbox commit atomically |
   | `identity.platform.permission-statement.create` | absent -> permission statement version 1 | duplicate ID or invalid typed statement -> conflict; state unchanged | authority version A -> A+1; prior-version decisions rejected; cached decisions invalidated; audit/outbox commit atomically |
   | `identity.platform.permission-statement.update` | permission statement version N -> N+1 with affected roles identified | stale expected statement version -> conflict; state unchanged | authority version A -> A+1; prior-version decisions rejected; cached decisions invalidated; audit/outbox commit atomically |
   | `identity.platform.permission-statement.delete` | permission statement version N -> absent after role disposition | stale expected statement version or in-use without explicit bounded role detachment/replacement -> conflict; state unchanged | authority version A -> A+1; prior-version decisions rejected; cached decisions invalidated; audit/outbox commit atomically |

   The delete cases MUST first prove the in-use conflict, then repeat with the
   explicit bounded disposition and prove atomic assignment/role detachment.
   Update and delete MUST each submit a stale expected version before the
   current version succeeds. The proof MUST assert returned resource and
   authority versions, persistent state, cache invalidation, immutable audit,
   outbox publication and authorization behavior after every step.

   Audit-retention administration MUST exercise the four authenticated
   reference `net/http` operations and the direct-only record-deletion public
   service contract without an admin UI. The direct deletion operation MUST
   remain absent from HTTP and OpenAPI. Every successful transition MUST emit
   exactly the named protected security event; every stale version, stale plan,
   ineligible record or hold conflict MUST leave policy, hold and record state
   unchanged apart from its safe denial/conflict audit.

   | Operation | Successful state transition | Required rejection transition | Exact protected event |
   | --- | --- | --- | --- |
   | `identity.audit-retention.policy.update` | retention policy version N -> N+1 at the declared effective boundary | stale expected policy version -> conflict; state unchanged | `identity.audit_retention.change_policy` |
   | `identity.audit-retention.legal-hold.create` | absent -> active legal hold version 1 | duplicate hold ID or invalid scope -> conflict; state unchanged | `identity.audit_retention.create_legal_hold` |
   | `identity.audit-retention.legal-hold.update` | active legal hold version N -> N+1 | stale expected hold version -> conflict; state unchanged | `identity.audit_retention.update_legal_hold` |
   | `identity.audit-retention.legal-hold.release` | active legal hold version N -> released version N+1 | stale expected hold version or already released by another command -> conflict; state unchanged | `identity.audit_retention.release_legal_hold` |
   | `identity.audit-retention.records.delete` | eligible planned records -> deleted with protected receipt and checkpoint | stale plan/policy/hold checkpoint, newly held record or ineligible record -> abort batch; records unchanged | `identity.audit_retention.delete_records` |

   The composed proof MUST show that an active hold prevents deletion, releasing
   it does not delete records automatically, and a newly confirmed plan is
   required before the direct bounded deletion succeeds. Policy changes MUST
   affect only newly evaluated eligibility; they MUST NOT silently delete or
   rewrite retained records. A clean deployment initializes durable policy
   version 1 exactly once from startup bootstrap defaults; concurrent starts
   converge on that row. Every restart loads that durable version without
   overwrite, and configuration drift does not reset runtime policy. The proof
   MUST restart after an online update and observe the same version and
   durations. Unset/reset requests MUST fail without changing policy state.
   The proof MUST also observe one protected bootstrap initialization event
   `identity.audit_retention.change_policy` committed atomically with version 1
   and no duplicate event after restart.
9. **Organizations:** create/update/delete, invite/accept/reject/cancel, member
   lifecycle, roles and permissions, teams, ownership transfer, active
   organization and configured limits under concurrent changes.
10. **Enterprise federation:** domain verification and provider selection;
    OIDC, OAuth 2 and SAML login; JIT provisioning; organization assignment;
    attribute/role mapping; linking; provider lifecycle; replay and downgrade
    denial.
    Every repeat login for an existing provider subject MUST apply the current
    versioned claim/mapping policy before session issuance, including explicit
    absent/null, downgrade/removal, local-ownership conflict, replay and
    outcome-unknown behavior.
    Directory/IdP synchronization MUST add, update, suspend and remove mapped
    users/groups/roles idempotently, preserve authoritative-source and conflict
    policy, reject stale/replayed versions, and cascade deprovisioning into
    sessions, grants and organization access without deleting retained audit
    evidence.
11. **SCIM 2.0:** authenticated discovery, Users and Groups CRUD, filter,
    pagination, PATCH, bulk limits, ETags, organization mapping, soft/hard
    deprovisioning and reconciliation.
    Personal/user-owned SCIM connections MUST be rejected; every connection,
    bearer and mapping is organization/provider owned, and imported legacy
    personal state requires an explicit migrate, disable or delete disposition.
12. **OAuth/OIDC provider:** register clients, authorize with consent and PKCE,
    exchange and refresh with rotation, revoke/introspect, discover metadata
    and JWKS, validate ID token/UserInfo at an independent relying party, and
    complete device authorization including denial and slow polling, and issue
    a bounded JWT from an explicitly enabled fresh-session exchange profile
    with the same JWKS/key-rotation guarantees.
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
    Package verification MUST avoid a reference/identitytest cycle: reference
    first proves itself with a public-contract pre-verification suite,
    identitytest then proves its helpers against that public API, and the
    coordinator finally reruns this complete end-state suite using those
    helpers as program-level evidence.
16. **Extension and access policy:** register ordered typed before/after hooks,
    custom identity/session fields and custom authorization statements without
    global state; prove cancellation, transaction boundaries, response/error
   behavior, custom roles and cross-tenant denial.
17. **Typed modules and operations:** compose an external typed endpoint,
    OpenAPI schema, middleware/hook, route-rate rule, reviewed trusted origin
    and owned migration contributor; reject every collision; generate a secret
    and a fully redacted diagnostic summary through public operational APIs.
18. **Privacy export:** an authenticated and freshly reauthenticated subject
    requests a bounded asynchronous export, observes authorized status, and
    downloads one short-lived single-use encrypted artifact containing the
    documented portable identity/account/session/device/organization/consent
    data and exclusions. Cross-tenant access, duplicate requests, cancellation,
    expiry, deletion races and delivery failure MUST preserve idempotency,
    auditability, minimization and redaction; provider-held data and legal-hold
    limitations MUST be explicit.

## HTTP and browser-facing contract

`identity/http` MUST own route composition, handler lifecycle, request limits,
content types, stable error envelopes, security headers, trusted proxy/origin
policy, CORS, CSRF, redirect allowlists, cookie attributes and prefixes, cache
policy, middleware/hooks, route collision detection, and idempotency surfaces.
Cookies MUST have explicit `Secure`, `HttpOnly`, `SameSite`, domain, path,
partition/cross-site and rotation semantics. State-changing cookie-authenticated
requests MUST have CSRF protection. Bearer endpoints MUST NOT inherit unsafe
cookie assumptions.

Every HTTP-exposed `both` or `protocol` operation, plus the security effects of
applicable middleware rows, MUST appear exactly once in OpenAPI 3.1.1 with
bounded schemas, security, errors and examples. Direct-only Go operations MUST
instead have clean-consumer compile and behavioral proof and MUST NOT be
silently exposed over HTTP. Handler registration and schema composition MUST
fail on duplicate routes, operation IDs or incompatible components. Extension
packages MUST integrate through typed composition, not global registration or
reflection.

`.ai/identity-platform/API_OPERATIONS.md` is the canonical operation inventory.
Every row requiring HTTP MUST have exactly one handler and OpenAPI operation
with the row's method/path, authentication, authorization, CSRF/origin,
parser/size, idempotency, rate and error contract. Coverage MUST be checked in
both directions; generated OpenAPI or route discovery MUST NOT replace the
canonical inventory.

The integrated HTTP proof MUST include pre-session login-CSRF binding for every
signin mechanism; exact origin and cookie serialization/deletion semantics;
strict bearer acquisition from one `Authorization` header with ambient cookies
ignored; bounded ambiguity-rejecting JSON/form/multipart/protocol parsers;
operation-scoped concurrent idempotency and unknown outcomes; exact
credential-mode CORS; trusted-hop-only proxy/base-URL resolution; configured
server/handler/provider/store timeouts; deterministic secret-free OpenAPI
export; and separate process-only liveness and dependency/migration/key-aware
readiness probes through startup and drain.

Every client HTTP route MUST have an explicit default, endpoint or extension
rate rule. The complete profile MUST prove trusted-proxy derivation,
IPv4-mapped IPv6 normalization, full canonical RFC 5952 IPv6 addresses without
subnet aggregation, Retry-After, distributed atomic counters and selected-store
outage behavior. IPv6 subnet aggregation belongs only to a future, separately
selected profile.

`identity/reference` MUST inject the complete selected feature and adapter set
through this public HTTP constructor. Missing configuration or dependencies
MUST fail before readiness; the HTTP module MUST remain usable with narrower
consumer-supplied feature sets without acquiring concrete adapter dependencies.

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
- The complete reference profile MUST wire the canonical PostgreSQL adapters
  for delivery, anonymous users, audit, authorization, rate limiting,
  idempotency, outbox and workflow, plus canonical `sso/domain-verification`,
  contract, and the selected Valkey authorization/rate/idempotency secondaries
  where configured. It MUST own tenant resolution, admin bootstrap, separate
  migration check/plan/apply operations, audit retention, bounded
  instrumentation, delivery/outbox/workflow workers, probes and rehearsed
  backup/restore/key rotation rather than delegating them to consumer glue.

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
