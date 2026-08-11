# Goal: pkg/identity/oauth

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals, as shown here.

## Execution metadata

- Unit: `identity/oauth`
- Canonical module: `pkg/identity/oauth`
- Canonical goal after scaffolding: `pkg/identity/oauth/.ai/GOAL.md`
- Requires: `identity`, `identity/session`, `identity/risk`
- Consumes existing primitives: `authentication/oidc`, `authentication/jwt`, `http-client`, `capability`, `secret-envelope`, `audit`
- Unlocks after verification: `identity/oauth/postgres`, `identity/oauth/providers`, `identity/oauth/onetap`, `identity/oauth/proxy`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/oauth` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/oauth` module that owns generic OAuth/OIDC authorization-code and PKCE social-login orchestration, provider contracts, callback state, token exchange/refresh, account linking, and provider-token lifecycle. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns generic provider registration and OAuth/OIDC authorization-code and PKCE social-login orchestration, callback state, token exchange/refresh, account linking, and provider-token lifecycle. Built-in provider facts belong to `identity/oauth/providers`; Google One Tap belongs to `identity/oauth/onetap`; preview callback forwarding belongs to `identity/oauth/proxy`. It does not own enterprise SSO routing, OAuth authorization-server behavior, provider UI, or provider-specific SDK wrappers. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define Provider, ProviderRegistry, AuthorizationRequest, StateProfile, PKCEPolicy, Callback, TokenSet, TokenVault, LinkPolicy, ClaimsMapper, SessionIssuer, and RefreshCoordinator contracts. The generic provider contract MUST expose all endpoint, client-authentication, issuer/audience, scope, claims, refresh and revocation decisions needed by the separate built-in catalog without importing it. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST register explicit generic provider configurations; bind state, nonce, redirect, provider, client and PKCE; validate ID tokens through existing validators; exchange once; encrypt refresh tokens; serialize refresh; link only with verified evidence; detect provider-account collisions; unlink without orphaning access; and classify revoked grants, unsupported provider capabilities, partial callbacks and unknown exchange/refresh outcomes. Provider-specific defaults MUST NOT erase a declared incompatibility or turn an unknown result into success. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Provider registration MUST validate unique provider IDs, HTTPS endpoints,
  exact callback paths, client authentication method, issuer policy, requested
  scopes, PKCE requirements and whether signup, implicit signup, ID-token
  signin, UserInfo override, refresh and revocation are supported.
- Authorization initiation MUST support signin and explicit account linking,
  additional scopes, provider prompt/response-mode options and bounded
  application data carried through authenticated state. Caller parameters MUST
  not override security-critical provider configuration.
- Callback handling MUST distinguish provider denial, malformed callback,
  state/nonce/PKCE failure, code-exchange unknown outcome, identity-proof
  failure, disabled signup and account collision. A callback MUST be idempotent
  without making an authorization code reusable.
- Providers without email MUST require an explicit stable subject mapping and
  collision-safe placeholder or email-optional identity policy. The module
  MUST NOT synthesize a globally trusted verified email by default.
- Account linking MUST distinguish signed-in explicit link, verified implicit
  link and administrator policy; forced linking MUST be opt-in and audited.
  Unlink MUST preserve at least one allowed access path and classify provider
  revocation separately from local unlink.
- Access-token retrieval, provider account information and incremental-scope
  requests MUST enforce subject/account ownership and return raw token data only
  through a secret-bearing contract with redaction and lifetime rules.
- Refresh MUST be single-flight per grant, rotate stored secrets when returned,
  preserve old-token validity only according to provider semantics, and expose
  revoked, retryable and reconciliation-required outcomes.
- Popup mode MUST bind expected opener origin and one-time result channel,
  produce the same identity/link/session result as redirect mode, and define
  cancellation, blocked popup, user closure and error delivery. Browser
  messaging belongs to `identity/http`; wildcard origins are forbidden.

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
