# Goal: pkg/oauth-server

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `oauth-server`
- Canonical module: `pkg/oauth-server`
- Canonical goal after scaffolding: `pkg/oauth-server/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:oauth-server:v1`; owned operation IDs: `contract:operation:identity.oauth-server.authorize:v1`, `contract:operation:identity.oauth-server.client-create:v1`, `contract:operation:identity.oauth-server.client-delete:v1`, `contract:operation:identity.oauth-server.client-get:v1`, `contract:operation:identity.oauth-server.client-get-public:v1`, `contract:operation:identity.oauth-server.client-get-public-prelogin:v1`, `contract:operation:identity.oauth-server.client-list:v1`, `contract:operation:identity.oauth-server.client-rotate-secret:v1`, `contract:operation:identity.oauth-server.client-update:v1`, `contract:operation:identity.oauth-server.consent-delete:v1`, `contract:operation:identity.oauth-server.consent-get:v1`, `contract:operation:identity.oauth-server.consent-list:v1`, `contract:operation:identity.oauth-server.consent-update:v1`, `contract:operation:identity.oauth-server.continue:v1`, `contract:operation:identity.oauth-server.discovery-oauth:v1`, `contract:operation:identity.oauth-server.dynamic-register:v1`, `contract:operation:identity.oauth-server.introspect:v1`, `contract:operation:identity.oauth-server.protected-resource-metadata:v1`, `contract:operation:identity.oauth-server.resource-verify:v1`, `contract:operation:identity.oauth-server.revoke:v1`, `contract:operation:identity.oauth-server.token:v1`
- Requires: `identity`, `identity/session`, `identity/risk`, `primitive/capability-identity-contracts`
- Consumes existing primitives: `authorization`, `authentication`, `capability`, `secret-envelope`, `audit`, `rate-limit`, `http-client`
- Unlocks after verification: `oauth-server/oidc`, `oauth-server/device`, `oauth-server/postgres`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `oauth-server` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/oauth-server` module that owns OAuth 2.1 authorization server core: clients, authorization endpoint model, code and PKCE grants, consent, token issuance, refresh rotation, revocation, introspection, metadata, and key lifecycle. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns OAuth 2.1 authorization server core: clients, authorization endpoint model, code and PKCE grants, consent, token issuance, refresh rotation, revocation, introspection, metadata, and key lifecycle. It does not own OIDC identity claims, device flow, resource-server authorization policy, UI, and persistence adapters. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define Issuer, Client, RedirectPolicy, AuthorizationRequest, Consent, Code, Grant, TokenIssuer, RefreshFamily, Revoker, Introspector, KeySet, Store, an OAuth-only `ScopeCatalog`, an OIDC extension seam, and endpoint result contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

Every endpoint contract MUST use typed request, success, interaction,
continuation and protocol-error results. Redirectable errors MUST carry only a
previously validated exact redirect target and bound state; errors discovered
before redirect validation MUST be rendered locally and MUST NOT redirect.
Public management operations and their actor/resource authorization MUST match
[`API_OPERATIONS.md`](../API_OPERATIONS.md), and all grant and client mutations
MUST implement [`TRANSACTION_CONTRACT.md`](../TRANSACTION_CONTRACT.md).

Core's built-in `ScopeCatalog` entries MUST contain OAuth resource scopes only.
The composed effective `oauth_server.scopes` catalog MAY additionally contain
OIDC extension entries, but core MUST NOT define, interpret, or issue the OIDC
`openid`, `profile`, `email`, `address`, `phone`, or `offline_access` scope
semantics or their claims. The consumer-owned OIDC
extension seam MUST accept one immutable, validated `OIDCExtension` supplied by
`oauth-server/oidc` at composition. It may contribute OIDC scopes, claims,
authorization validation and discovery projection without importing
`oauth-server/oidc` into core. Core MUST reject OIDC scopes when that extension
is absent, and OAuth discovery MUST advertise only the composed effective scope
union without interpreting OIDC claims.
`OIDCExtension` MUST expose exactly
`Scopes() ScopeCatalog`,
`ValidateAuthorization(context.Context, OIDCAuthorizationInput) (OIDCAuthorizationProjection, error)`,
`Claims(context.Context, OIDCClaimsInput) (OIDCClaimsProjection, error)`, and
`OIDCDiscovery(context.Context) (OIDCDiscoveryProjection, error)`. The core
`Dependencies` contract accepts at most one optional extension and the core
`Config.Scopes` field is the explicit OAuth-only `ScopeCatalog`.

## Required behavior

The implementation and tests MUST validate clients and exact redirects; require PKCE; issue single-use codes; bind issuer/client/redirect/subject/scope/nonce where applicable; rotate refresh tokens with family replay revocation; narrow scopes; revoke and introspect consistently; publish metadata; prevent mix-up and authorization-code injection. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Client management MUST include get public/private view, list, create, update,
  rotate secret with overlap policy, delete, trusted-client policy and dynamic
  registration with an RFC 7591 initial access token, expiration and allowed
  scopes. Enabling this reference profile selects RFC 7591 only. RFC 7592 is an
  unselected future management profile, and its registration access tokens
  MUST NOT be issued until exact owner-bound read/update/delete operations and
  routes are separately selected.
  Public clients MUST never receive or require a client secret.
- Dynamic registration MUST be disabled unless the typed
  `oauth_server.dynamic_registration` profile is enabled. Its initial access
  token MUST be expiring, audience/owner bound, single-use and digest-stored.
  Requested scopes MUST be unique and contained in the exact
  `oauth_server.dynamic_registration.allowed_scopes` subset of the canonical
  `oauth_server.scopes` catalog; an unknown or out-of-policy scope rejects the
  complete registration without creating a client.
  Reference enablement is startup-only through
  `oauth_server.dynamic_registration.enabled=true`, one exact immutable owner,
  and `secrets.oauth_server_dynamic_registration_initial_access_token`.
  Readiness and `/oauth2/register` registration require the matching
  owner/audience/version-scoped expiring digest in `oauth-server/postgres`; the
  token is consumed atomically with one client creation. Disabling the profile
  removes the route without deleting existing clients.
  Registration creates exactly one immutable tenant,
  organization or platform owner. The reference profile MUST omit every RFC
  7592 management endpoint, metadata value, URI, and token regardless of RFC
  7591 enablement.
- Administrative creation and RFC 7591 registration MUST expose an explicit
  `Public` Boolean. `Public=true` selects a no-secret public client;
  `Public=false` is a valid confidential-client selection and MUST NOT be
  rejected as a zero value. Authentication method and reveal-once secret
  behavior MUST agree with that selection.
- RFC 7591 registration MUST use a stable server-owned `protocol-command`
  identity derived from the authenticated request and immutable preconditions.
  A client `Idempotency-Key` MAY map to it but MUST NOT be required for
  standards interoperability.
- Every client MUST have an immutable owning tenant/organization or explicit
  platform owner. Client read, list, update, secret rotation, deletion,
  registration-token use, consent administration and trusted-client changes
  MUST re-evaluate current owner authority and prevent cross-owner identifiers,
  redirects, JWKS, sectors, resources or registration tokens from being
  attached or observed.
- Redirect URIs MUST use exact matching except specification-defined loopback
  port handling. Client metadata, authentication method, grant types, response
  types, audiences, organization policy and PKCE requirements MUST be validated
  atomically at registration and authorization.
- Authorization MUST support login-required, signup-required, account
  selection, post-login continuation and consent-required outcomes as typed UI
  contracts; cached trusted-client consent MUST be narrow, expiring and
  revocable.
- Post-login, account-selection and consent continuations MUST be opaque,
  authenticated, bounded, expiring and one-time. They MUST bind issuer, client,
  validated redirect URI, response type/mode, state, PKCE challenge/method,
  nonce, resource/audience, requested scopes/claims, tenant, browser session
  and completed interaction steps. Resumption MUST revalidate client status,
  redirect registration, policy and subject authority and MUST reject altered,
  stale, cross-client or replayed continuations.
- Grants MUST include authorization code, refresh token and client credentials
  where policy permits. Unsupported implicit and resource-owner-password grants
  MUST be rejected and omitted from metadata.
- Consent operations MUST get, list, create/update narrow scopes and delete;
  changed client scopes/claims/audiences MUST invalidate stale consent.
- Token operations MUST include exchange, refresh rotation/reuse detection,
  revocation and introspection for JWT and opaque profiles. Access-token
  audience/resource binding and API-server verification guidance MUST be
  executable, not documentation-only.
- RFC 7662 inactive or unknown tokens MUST return a successful typed result
  with `Active=false`; client, subject, scopes, audience, expiry and every other
  token metadata member MUST be absent. For `Active=true`, those members remain
  optional, present only when the token contains them and caller authorization
  permits disclosure. `oauthserver.Client.Public=false` MUST be a valid
  confidential-client projection consistent with create and RFC 7591 inputs.
- Authorization responses MUST include and validate the authorization-server
  issuer identifier required by the selected security baseline. Authorization
  codes and continuations MUST bind that issuer and the exact token endpoint
  client so a response or code from another issuer/client cannot be accepted.
  Tests MUST cover mix-up, malicious endpoint metadata, code substitution and
  multi-issuer deployments.
- Token, introspection and revocation endpoints MUST implement an explicit
  client-authentication matrix covering public-client `none` with PKCE and each
  advertised confidential method. Secret, assertion, key, audience, issuer,
  subject, time, JWT-ID replay and certificate bindings MUST be validated as
  applicable; credentials in multiple locations, method downgrade, methods not
  registered to the client and methods not advertised in metadata MUST fail.
- Resource indicators MUST be validated against client policy and carried into
  consent, code, access-token audience and introspection. When protected
  resource metadata is exposed, the contract MUST publish the exact resource
  identifier, authorization servers, supported bearer locations and scope
  semantics from registered configuration; it MUST NOT infer metadata from an
  untrusted request host or claim ownership of resource-server authorization.
  RFC 9728 metadata and access-token audience/resource validation MUST use the
  same typed profile. The reference profile rejects path-bearing resource
  identifiers and path-bearing issuers so the well-known route is unambiguous.
  The metadata `resource`, authorization request resource/audience, code and
  token audience, introspection result, and resource verifier MUST all use the
  exact `oauth_server.protected_resource.resource` origin and MUST NOT derive it
  from an inbound host.
  Its `scopes_supported` value MUST equal
  `oauth_server.protected_resource.supported_scopes`, which MUST remain a
  sorted unique subset of `oauth_server.scopes` and match resource-verification
  enforcement.
- Core MUST expose typed, authorization-checked client, grant, consent, token
  and signing seams to `oauth-server/oidc` and `oauth-server/device`, but MUST
  NOT own their endpoints, protocol state, configuration or child-specific
  events. OIDC owns end-session and session-token-exchange protocol validation;
  device owns device/user codes, polling, approval and denial. Their validated
  commands may invoke core token and revocation transitions without moving
  semantic ownership into core.
- Core signing-key lifecycle MUST own private-key generation/import, storage,
  algorithm policy, rotation, compromise and retirement. It MUST expose only a
  signing capability and public-key projection to `oauth-server/oidc`; OIDC
  owns JWKS/discovery representation. Protocol logout validation MUST return a
  typed session-termination command, while `identity/session` remains the sole
  owner of session revocation. No module MAY bypass those ownership boundaries.
- Rate limits MUST be endpoint/client/subject aware and use the identity-risk
  contract. Login, consent, client CRUD and device verification remain
  explicitly authorized operations.
- Discovery MUST advertise only implemented grants, authentication methods,
  scopes, claims, PKCE, registration and endpoints. Configuration that diverges
  from advertised metadata MUST fail construction.
- Custom token response fields, claims and scopes MUST use typed declarations,
  minimize data by consent and prevent hooks from adding unapproved privilege
  or secrets.

## Security and abuse requirements

- Inputs MUST be bounded before parsing, allocation, storage, hashing, or
  cryptographic work.
- Subject, tenant, organization, purpose, audience, action, and redirect scope
  MUST be bound wherever applicable and MUST fail closed on mismatch.
- Enumeration, replay, fixation, confused-deputy, downgrade, race, and
  cross-scope attacks MUST have deterministic regression cases.
- Logs, traces, metrics, examples, fixtures, and errors MUST preserve the
  redaction requirements in `.ai/identity-platform/COMMON_REQUIREMENTS.md`.

## Persistence, lifecycle, and compatibility

The core MUST remain adapter-neutral unless this goal is itself an adapter.
State ownership, consistency, retention, deletion, migration, key rotation,
clock skew, concurrent callers, shutdown, and recovery MUST be documented and
tested where applicable. Unsupported protocol or deployment profiles MUST be
stated rather than silently approximated.

OAuth behavior, client authentication, redirects, issuer identification,
resource indicators and protected-resource metadata MUST conform to
[`PROTOCOL_BASELINES.md`](../PROTOCOL_BASELINES.md). Grant/client/key deletion
and revocation MUST conform to
[`LIFECYCLE_CASCADES.md`](../LIFECYCLE_CASCADES.md), and advertised feature and
lifetime defaults MUST be explicit in
[`REFERENCE_CONFIGURATION.md`](../REFERENCE_CONFIGURATION.md).
Client, consent, authorization, code, refresh and revocation transitions MUST
emit the bounded records defined by
[`SECURITY_EVENTS.md`](../SECURITY_EVENTS.md).
This unit owns applicability for `ref.oauth_server.client_class` and
`ref.struct:ref.oauth_server.client_class`; administrative and RFC 7591 client
creation MUST consume those exact public/confidential semantics.

## Acceptance evidence

Before this unit becomes `verified`, the owner MUST satisfy every common gate,
the package-specific behavior above, the module's exact coverage and mutation
gates, race/fuzz/interoperability gates that apply, clean-consumer proof,
manifests, public API baseline, security and supply-chain checks, documentation,
changelog, and changed reverse-dependant gates. The final evidence record MUST
name any non-applicable gate with a reviewed reason; absence of infrastructure
or provider access is a blocker, not a pass.

Verification applicability is exact for this unit: `race=required`,
`fuzz=required`, `hostile=required`, `leak=required`, `benchmark=required`,
`infrastructure=required`, and `provider_interoperability=required`; a gate
MAY be satisfied by the required composed reference evidence but MUST NOT be
silently skipped.

## Release blockers

The unit MUST remain `implemented-unverified` or `blocked` if any prerequisite
is not `verified`, any ownership boundary is unresolved, a protocol claim
lacks pinned specification and interoperability evidence, a durable transition
has unhandled ambiguity, a secret can escape redaction, or any required gate is
stale, skipped, warning-only, or failing.
