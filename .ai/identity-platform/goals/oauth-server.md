# Goal: pkg/oauth-server

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals, as shown here.

## Execution metadata

- Unit: `oauth-server`
- Canonical module: `pkg/oauth-server`
- Canonical goal after scaffolding: `pkg/oauth-server/.ai/GOAL.md`
- Requires: `identity`, `identity/session`, `identity/risk`
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

The design MUST define Issuer, Client, RedirectPolicy, AuthorizationRequest, Consent, Code, Grant, TokenIssuer, RefreshFamily, Revoker, Introspector, KeySet, Store, and endpoint result contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST validate clients and exact redirects; require PKCE; issue single-use codes; bind issuer/client/redirect/subject/scope/nonce where applicable; rotate refresh tokens with family replay revocation; narrow scopes; revoke and introspect consistently; publish metadata; prevent mix-up and authorization-code injection. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Client management MUST include get public/private view, list, create, update,
  rotate secret with overlap policy, delete, trusted-client policy and dynamic
  registration with registration access token, expiration and allowed scopes.
  Public clients MUST never receive or require a client secret.
- Redirect URIs MUST use exact matching except specification-defined loopback
  port handling. Client metadata, authentication method, grant types, response
  types, audiences, organization policy and PKCE requirements MUST be validated
  atomically at registration and authorization.
- Authorization MUST support login-required, signup-required, account
  selection, post-login continuation and consent-required outcomes as typed UI
  contracts; cached trusted-client consent MUST be narrow, expiring and
  revocable.
- Grants MUST include authorization code, refresh token and client credentials
  where policy permits. Unsupported implicit and resource-owner-password grants
  MUST be rejected and omitted from metadata.
- Consent operations MUST get, list, create/update narrow scopes and delete;
  changed client scopes/claims/audiences MUST invalidate stale consent.
- Token operations MUST include exchange, refresh rotation/reuse detection,
  revocation and introspection for JWT and opaque profiles. Access-token
  audience/resource binding and API-server verification guidance MUST be
  executable, not documentation-only.
- End-session behavior MUST validate client/post-logout redirect and session
  ownership and state without becoming open redirect or cross-client logout.
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

## Acceptance evidence

Before this unit becomes `verified`, the owner MUST satisfy every common gate,
the package-specific behavior above, the module's exact coverage and mutation
gates, race/fuzz/interoperability gates that apply, clean-consumer proof,
manifests, public API baseline, security and supply-chain checks, documentation,
changelog, and changed reverse-dependant gates. The final evidence record MUST
name any non-applicable gate with a reviewed reason; absence of infrastructure
or provider access is a blocker, not a pass.

## Release blockers

The unit MUST remain `implemented-unverified` or `blocked` if any prerequisite
is not `verified`, any ownership boundary is unresolved, a protocol claim
lacks pinned specification and interoperability evidence, a durable transition
has unhandled ambiguity, a secret can escape redaction, or any required gate is
stale, skipped, warning-only, or failing.
