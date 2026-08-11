# Goal: pkg/sso/oidc

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `sso/oidc`
- Canonical module: `pkg/sso/oidc`
- Canonical goal after scaffolding: `pkg/sso/oidc/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:sso/oidc:v1`; owned operation IDs: `contract:operation:identity.sso.oidc-callback:v1`, `contract:operation:identity.sso.oidc-logout:v1`, `contract:operation:identity.sso.oidc-logout-complete:v1`
- Requires: `sso`
- Consumes existing primitives: `authentication/oidc`, `authentication/jwt`, `http-client`, `secret-envelope`, `audit`
- Unlocks after verification: `sso/postgres`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `sso/oidc` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/sso/oidc` module that owns enterprise OpenID Connect relying-party protocol for SSO providers. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns enterprise OpenID Connect relying-party protocol for SSO providers. It does not own SSO routing, JIT policy, social-provider catalog, OAuth server, and persistence. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define discovery, static metadata, authorization request, state/nonce/PKCE, callback, ID-token validation adapter, UserInfo policy, logout capability declaration, and provider error contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST pin issuer and redirect; validate discovery and ID tokens through existing primitives; bind state, nonce, and PKCE; handle key rotation and cache bounds; prevent mix-up and substitution; map verified claims only; test documented OIDC provider fixtures and an independent implementation. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Registration MUST accept either pinned static metadata or HTTPS discovery
  with exact issuer matching. Discovery MUST enforce SSRF-safe resolution,
  redirect/IP policy, body/JSON limits, TLS and cache/refresh bounds.
- Every authorization/callback/token request MUST bind the activated provider
  credential generation selected by `identity.sso.provider.credentials-rotate`;
  overlap, disable and unknown activation behavior belong to `sso`, and this
  adapter MUST NOT select or persist an independent credential lifecycle.
- Authorization requests MUST bind state, nonce, PKCE, redirect and requested
  organization/provider and support configured prompt, login hint and scopes
  without caller scope escalation.
- The enterprise OIDC relying-party profile MUST use `query` response mode and
  reject fragment, `form_post`, JWT-secured, and caller-selected response-mode
  substitution as unsupported. Fragment parameters MUST never be treated as a
  callback input.
- Callback MUST validate issuer, audience/authorized party, signature/algorithm,
  times, nonce, state, PKCE and subject before optional bounded UserInfo merge.
  UserInfo MUST NOT override verified claims contrary to explicit policy.
- The raw PKCE verifier MUST use the recoverable encrypted storage contract in
  `struct:ref.oauth.rp_transaction`: exact AAD/lifetime, keyed commitment only
  for lookup/replay, decrypt only after callback capability reservation, erase
  on every declared terminal outcome, and retain without resubmission while an
  exchange outcome is ambiguous and under authoritative recovery.
- Access and refresh tokens returned by exchange, refresh or UserInfo-capable
  profiles MUST be handed directly to the SSO `EnterpriseTokenVault`; this
  adapter MUST NOT retain recoverable tokens or expose them to mapping, hooks,
  errors or audit. Refresh serialization, rotation/reuse, revocation and
  unknown outcomes MUST use the vault contract.
- Every successful login, including an existing linked subject, MUST return
  verified claims plus provider/profile version to the SSO repeat-login sync
  policy. The adapter MUST distinguish absent, null, unverified and authoritative
  claims and MUST NOT itself preserve roles or membership from an earlier login.
- Trusted origins and shared callback URLs MUST be exact allowlists. Mix-up,
  malicious discovery, issuer aliases, JWK rotation races and token-substitution
  cases MUST fail closed.
- Logout, refresh and revocation capabilities MUST be declared per provider;
  unsupported features MUST NOT be inferred from discovery omissions.
- RP-initiated logout MUST have distinct start and completion operations. Start
  persists one-time state bound to provider, issuer, local session/version and
  allowlisted post-logout target before redirect. Completion MUST consume that
  state exactly once, validate the provider outcome, and reconcile success,
  error, timeout, replay, and unknown provider outcome with local revocation,
  which remains authoritative on every path.
- Start and completion MUST return exactly one closed outcome variant:
  redirect, local-only, provider-complete, provider-error, timeout, or
  unknown-reconciliation. Required Boolean sentinel fields are forbidden;
  variant-specific fields are valid only for the selected outcome.
- Those operations MUST be exactly `identity.sso.oidc-logout` and
  `identity.sso.oidc-logout-complete`, including the fixed protocol paths,
  exposure, access, CSRF/origin, provider rate class, idempotency and
  `identity.sso.logout_oidc` event semantics in `API_OPERATIONS.md`.
- Official fixtures plus at least one independent enterprise IdP profile MUST
  prove discovery, login, JWK rotation and mapping; provider-specific deviations
  MUST remain attributable.

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

This unit owns applicability for `ref.struct:ref.oidc.logout_outcome`; start
and completion results MUST consume that exact closed outcome policy and MUST
NOT define parallel Boolean sentinel semantics.

The unit MUST remain `implemented-unverified` or `blocked` if any prerequisite
is not `verified`, any ownership boundary is unresolved, a protocol claim
lacks pinned specification and interoperability evidence, a durable transition
has unhandled ambiguity, a secret can escape redaction, or any required gate is
stale, skipped, warning-only, or failing.
