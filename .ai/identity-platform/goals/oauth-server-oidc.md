# Goal: pkg/oauth-server/oidc

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `oauth-server/oidc`
- Canonical module: `pkg/oauth-server/oidc`
- Canonical goal after scaffolding: `pkg/oauth-server/oidc/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:oauth-server/oidc:v1`; owned operation IDs: `contract:operation:identity.oauth-server.discovery-oidc:v1`, `contract:operation:identity.oauth-server.end-session:v1`, `contract:operation:identity.oauth-server.jwks:v1`, `contract:operation:identity.oauth-server.session-token:v1`, `contract:operation:identity.oauth-server.userinfo:v1`
- Requires: `oauth-server`, `primitive/capability-identity-contracts`
- Consumes existing primitives: `authentication/jwt`, `identifier`, `audit`
- Unlocks after verification: `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `oauth-server/oidc` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/oauth-server/oidc` module that owns OpenID Connect provider extensions: ID tokens, UserInfo, nonce, subject identifiers, claims, discovery, JWKS, auth-time and prompt/max-age semantics. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns OpenID Connect provider extensions: ID tokens, UserInfo, nonce, subject identifiers, claims, discovery, JWKS, auth-time and prompt/max-age semantics. It does not own OIDC relying-party validation, general OAuth grants, UI, federation, and persistence. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define SubjectPolicy, PairwiseSubject, ClaimSource, IDTokenProfile, Signer, UserInfo, NoncePolicy, AuthContext, Discovery, JWKS, key rotation, and error contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST issue correctly bound signed ID tokens; support public and pairwise subjects; enforce nonce and auth_time; honor max_age and prompt outcomes; minimize claims by consent/scope; rotate signing keys without breaking verification windows; test discovery, JWKS, tokens, and UserInfo with independent OIDC clients. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Discovery MUST include exact issuer/endpoints, subject types, response/grant
  types, signing algorithms, claims, scopes, authentication methods and PKCE
  capabilities and MUST match the OAuth-server metadata.
- ID tokens MUST bind issuer, audience/authorized party, subject, issued/expiry
  time, nonce when supplied, authentication time/context/method and access-token
  or code hashes where the selected flow requires them.
- UserInfo `sub` MUST equal the ID-token subject for the same grant and client;
  mismatch MUST deny without returning claims.
- Public and pairwise subjects MUST be stable for the declared sector/client
  policy, unlinkable across sectors, key-rotation safe and non-reversible to a
  raw internal user ID.
- Pairwise subjects MUST remain disabled without an exact registered sector
  identifier and a versioned secret derivation key. Enabled derivation MUST use
  the reference domain-separated HMAC-SHA-256 policy and key rotation MUST
  preserve stable subjects through an explicit overlap/migration.
- Prompt `none`, login, consent and select-account plus `max_age` MUST produce
  specification-correct success or interaction-required errors without
  silently creating a session or consent.
- UserInfo MUST authenticate the access token, enforce audience/scope/subject,
  return only consented claims and never expose write-only or sensitive custom
  identity fields.
- The module MUST implement an authenticated-session JWT exchange profile as a
  supported public operation; deployments MAY disable issuance by policy, but
  absence of the operation or executable proof blocks verification. It MUST
  issue only a short-lived token without a full authorization redirect, require
  a fresh valid session and explicit operation authorization, atomically bind
  and consume any anti-replay proof, bind configured audience/resource, scope
  and claims, prohibit arbitrary subject/audience requests, use the same
  issuer/signing/JWKS rotation as OAuth grants, and never outlive the source
  session or configured maximum. Session revocation, subject disablement and
  signing-key compromise MUST produce the lifecycle behavior declared for this
  exchange profile.
- Exchange MUST validate its anti-replay proof read-only, then reserve, apply
  and finalize the one-use capability with access-JWT issuance in one command.
  Unknown completion MUST recover before another exchange. Each JWT MUST
  snapshot global, tenant, user, authorization, every applicable organization
  and factor, OAuth client, grant, signing-key-compromise epoch and `kid`, plus
  the source session and session-family authority versions;
  verifiers and positive caches MUST deny on a newer version or unknown
  lifecycle acknowledgement.
- JWKS MUST publish only public verification material, unique `kid` values and
  supported algorithms. Rotation MUST preserve an overlap window, revoke
  compromised keys explicitly and never serve private members.
- `oauth-server` owns private signing-key lifecycle and supplies a bounded
  signing capability plus public projection. This module owns OIDC algorithm
  selection within that policy, ID-token signing, JWKS serialization/cache
  semantics and OIDC discovery. It MUST NOT persist or export private key
  members. Rotation publication order MUST prevent a newly signed token from
  referencing an unpublished key; retirement MUST preserve verification until
  every valid token expires unless compromise policy explicitly revokes it.
- OIDC logout MUST validate issuer, ID-token hint when required, client,
  initiating session, post-logout redirect and state, then return a typed
  termination result with exactly one closed outcome variant: redirect,
  local-only, provider-complete, provider-error, timeout, or
  unknown-reconciliation. Required Boolean sentinel fields are forbidden and
  variant-specific data is valid only for its selected outcome.
  `identity/session` alone owns session invalidation and
  cookie clearing. Front-channel, back-channel or RP-initiated logout metadata
  MUST be advertised only for implemented interoperable profiles; cross-client
  or subject-wide logout MUST require explicit authority.
- Independent relying-party evidence MUST verify discovery, JWKS, ID token,
  UserInfo, nonce, pairwise subject and rotation behavior; self-verification by
  the same signer/verifier is insufficient.

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

OIDC validation, session exchange, JWKS and logout MUST conform to
[`PROTOCOL_BASELINES.md`](../PROTOCOL_BASELINES.md); public operations MUST
match [`API_OPERATIONS.md`](../API_OPERATIONS.md); session/key/logout cascades
MUST match [`LIFECYCLE_CASCADES.md`](../LIFECYCLE_CASCADES.md); and algorithms,
lifetimes, exchange enablement and logout profiles MUST be explicit in
[`REFERENCE_CONFIGURATION.md`](../REFERENCE_CONFIGURATION.md).
This module emits exactly `identity.oauth_server.exchange_session` for session
exchange and `identity.oauth_server.end_session` for OIDC logout.
`oauth-server` alone owns add, retire and compromise signing-key events; OIDC
consumes the resulting signing authority and compromise versions without
becoming a parallel lifecycle or event owner. All emitted records use the
field, redaction and delivery contract in
[`SECURITY_EVENTS.md`](../SECURITY_EVENTS.md).

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
