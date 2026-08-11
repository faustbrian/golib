# Goal: pkg/identity/oauth

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/oauth`
- Canonical module: `pkg/identity/oauth`
- Canonical goal after scaffolding: `pkg/identity/oauth/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/oauth:v1`; owned operation IDs: `contract:operation:identity.account.access-token:v1`, `contract:operation:identity.account.link-start:v1`, `contract:operation:identity.account.link-token:v1`, `contract:operation:identity.account.provider-info:v1`, `contract:operation:identity.account.refresh-token:v1`, `contract:operation:identity.oauth.callback:v1`, `contract:operation:identity.oauth.callback-form-post:v1`, `contract:operation:identity.oauth.popup-complete:v1`, `contract:operation:identity.oauth.signin-start:v1`, `contract:operation:identity.oauth.signin-token:v1`
- Requires: `identity`, `identity/session`, `identity/risk`, `primitive/capability-identity-contracts`
- Consumes existing primitives: `authentication/oidc`, `authentication/jwt`, `http-client`, `capability`, `capability/postgres`, `secret-envelope`, `audit`
- Unlocks after verification: `identity/oauth/postgres`, `identity/oauth/providers`, `identity/oauth/onetap`, `identity/oauth/proxy`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/oauth` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/oauth` module that owns generic OAuth/OIDC authorization-code and PKCE social-login orchestration, provider contracts, callback state, token exchange/refresh, provider-proof and account-link orchestration, and provider-token lifecycle. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns generic provider registration and OAuth/OIDC authorization-code and PKCE social-login orchestration, callback state, token exchange/refresh, provider-proof and account-link orchestration, and provider-token lifecycle. It MUST NOT own provider-link rows, provider-subject uniqueness, final-access policy, account-link metadata, or the `lifecycle.dimension.social_link` authority version. Every link, relink, and unlink MUST invoke the public `identity` command and enlist `identity/postgres`; that participant alone mutates the authoritative link row, decides uniqueness/final-access, and advances the social-link version in the same unit of work as enlisted token-vault changes. Built-in provider facts belong to `identity/oauth/providers`; Google One Tap belongs to `identity/oauth/onetap`; preview callback forwarding belongs to `identity/oauth/proxy`. It MUST NOT emit `identity.oauth.verify_one_tap` or `identity.oauth.use_proxy`; the child modules are their sole event owners. It does not own enterprise SSO routing, OAuth authorization-server behavior, provider UI, or provider-specific SDK wrappers. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define Provider, ProviderRegistry, AuthorizationRequest, StateProfile, PKCEPolicy, Callback, TokenSet, TokenVault, LinkPolicy, ClaimsMapper, SessionIssuer, and RefreshCoordinator contracts. The generic provider contract MUST expose all endpoint, client-authentication, issuer/audience, scope, claims, refresh and revocation decisions needed by the separate built-in catalog without importing it. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

The public API MUST expose distinct typed initiation, callback, direct-token,
link and unlink commands. Successful signin and linking MUST return one typed
identity result independent of transport; redirect and popup delivery MUST be
typed wrappers around that result, not unvalidated caller-controlled URLs or
stringly typed status maps. The command surface and authorization requirements
MUST remain aligned with [`API_OPERATIONS.md`](../API_OPERATIONS.md), and every
durable callback, link, unlink and token transition MUST implement
[`TRANSACTION_CONTRACT.md`](../TRANSACTION_CONTRACT.md).
Every signin initiation or direct-token request MUST accept
`identity/session`'s `RememberPolicy`; authorization state, native nonce,
popup/proxy handoff, MFA continuation and callback MUST bind and preserve it
unchanged until session issuance. A callback MUST NOT infer persistence from
ambient cookies or choose a new policy.

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
- The response mode MUST come from the immutable provider profile. Generic
  OAuth/OIDC providers use `query`; the sole selected exception is Apple's
  pinned `form_post` profile. The callback MUST reject a transport or mode that
  differs from the state-bound profile before code or ID-token processing.
- The generic option model MUST distinguish provider identity, client ID and
  secret source, authorization/token/UserInfo/JWKS/revocation endpoints,
  issuer and audience rules, client-authentication method, default and optional
  scopes, authorization and token parameters, PKCE and nonce policies,
  response mode, prompt, access type, signup and implicit-signup policy,
  ID-token and UserInfo precedence, claims mapping, refresh/revocation support,
  token retention and timeouts. Each option MUST declare whether it is fixed by
  the profile, safely caller-selectable, or forbidden; unknown fields and
  conflicting duplicate parameters MUST fail construction.
- Callback handling MUST distinguish provider denial, malformed callback,
  state/nonce/PKCE failure, code-exchange unknown outcome, identity-proof
  failure, disabled signup and account collision. A callback MUST be idempotent
  without making an authorization code reusable.
- An authorization response MUST contain exactly one of a code or an error and
  MUST reject duplicate or conflicting `code`, `state`, `iss`, `error`,
  `error_description` and `error_uri` members. When RFC 9207 issuer
  identification is selected, `iss` is REQUIRED and MUST exactly match the
  transaction's configured issuer before any token exchange; an absent,
  unexpected or mismatched issuer MUST fail closed.
- Callback state MUST be authenticated, confidential when it carries
  application data, single-use, bounded and expiring. It MUST bind the
  operation kind, provider and client, tenant, initiating subject for linking,
  exact callback redirect, PKCE verifier commitment, nonce, response mode,
  requested scopes, opener origin/channel where applicable and an opaque
  continuation reference. Consumption and code-exchange ownership MUST be
  atomic enough that concurrent or replayed callbacks cannot exchange twice;
  an unknown exchange outcome MUST enter reconciliation instead of recreating
  state or retrying the code.
- The PKCE verifier MUST follow `struct:ref.oauth.rp_transaction`: retain the
  recoverable value only as envelope-AEAD ciphertext with the exact AAD,
  lifetime, release and erasure rules, while retaining only a keyed commitment
  for binding/lookup/replay. Callback handling MUST reserve the state capability
  before the bound exchange worker decrypts it; an ambiguous exchange retains
  the reservation and ciphertext solely for authoritative recovery and never
  retries the code.
- Direct/native provider-token signin MUST accept only an explicitly configured
  token kind and provider profile. ID tokens MUST receive full issuer,
  signature, audience/authorized-party, time and nonce validation. Access
  tokens MUST be resolved through the configured provider introspection or
  UserInfo boundary and MUST NOT be treated as identity assertions by shape.
  The result MUST bind the intended application client, provider account,
  granted scopes and token expiry, apply the same signup/link/collision/session
  policy as callback signin, and MUST NOT silently retain a refresh token or
  broaden scopes. Provider credentials minted for an unapproved audience or
  unverifiable native client MUST fail closed.
  The reference selector is exactly `native-token-modes-v1`: Apple accepts only
  `id_token`, Facebook only `opaque_access_token`, Google and LINE either, and
  every other `provider-catalog-v1` ID neither. Unsupported pairs MUST fail
  before token parsing or provider I/O.
- Apple redirect signin is the sole selected `response_type=code id_token`,
  `response_mode=form_post` profile. Its success callback MUST receive code,
  ID token and state, validate the front-channel ID token issuer, audience,
  `azp`, nonce, `c_hash`, time and subject before exchanging the code, and
  require the same subject across proofs. It MUST NOT generalize hybrid or
  form-post behavior to another provider.
- The Apple cross-site form POST MUST correlate through the separate
  `__Secure-identity_frontchannel` Secure/HttpOnly/SameSite=None, exact-path,
  five-minute, one-use cookie. Normal session and flow cookies remain
  SameSite=Lax and MUST NOT be reused or weakened.
- Providers without email MUST require an explicit stable subject mapping and
  collision-safe placeholder or email-optional identity policy. The module
  MUST NOT synthesize a globally trusted verified email by default.
- Account linking MUST distinguish signed-in explicit link, verified implicit
  link and administrator policy; forced linking MUST be opt-in and audited.
  Unlink MUST preserve at least one allowed access path and classify provider
  revocation separately from local unlink.
- Link, relink, and unlink are orchestration commands only. They MUST enlist
  the identity command and `identity/postgres` authoritative mutation; OAuth
  proof or token-vault state MUST NOT independently create, replace, delete, or
  version a provider link.
- Implicit linking MUST require fresh provider proof and a currently verified,
  canonical identifier whose provider semantics authorize that use. It MUST
  re-evaluate the local account's current verified identifier and tenant scope,
  reject provider-account or identifier collisions, and require explicit
  reauthentication or administrator recovery when there is any ambiguity.
  An email match alone, an unverified email, mutable profile data, a stale
  session, or caller-supplied subject MUST NOT take over or merge an account.
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

OAuth/OIDC validation and client-authentication profiles MUST conform to
[`PROTOCOL_BASELINES.md`](../PROTOCOL_BASELINES.md). Grant unlink, subject
deletion, provider revocation and retained-token cleanup MUST follow
[`LIFECYCLE_CASCADES.md`](../LIFECYCLE_CASCADES.md). The supported reference
defaults and every intentionally unsupported option MUST be represented as
explicit configuration under
[`REFERENCE_CONFIGURATION.md`](../REFERENCE_CONFIGURATION.md).
This unit owns applicability for `ref.frontchannel_post.cookie`,
`ref.oauth.rp.shared_redirect_issuer`, and
`ref.struct:ref.frontchannel_post_cookie`; workers MUST consume those exact
rows and MUST NOT create parallel callback-cookie or issuer policy.
Security-relevant starts, denials, callbacks, links, refreshes and revocations
MUST emit the bounded records defined by
[`SECURITY_EVENTS.md`](../SECURITY_EVENTS.md).
Password reset, password compromise, global compromise, identity suspension,
anonymization and deletion MUST invalidate every affected session-derived
grant and cached positive decision at the selected authority-version boundary;
unknown cascade acknowledgement MUST deny refresh and session issuance.

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
