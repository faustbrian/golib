# Goal: pkg/sso/oauth2

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `sso/oauth2`
- Canonical module: `pkg/sso/oauth2`
- Canonical goal after scaffolding: `pkg/sso/oauth2/.ai/GOAL.md`
- Requires: `sso`
- Consumes existing primitives: `http-client`, `capability`, `secret-envelope`, `audit`
- Unlocks after verification: `sso/postgres`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `sso/oauth2` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/sso/oauth2` module that owns enterprise OAuth 2.0 authorization-code provider integration when no OIDC ID token exists. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns enterprise OAuth 2.0 authorization-code provider integration when no OIDC ID token exists. It does not own generic social login catalog, SSO routing/JIT policy, OAuth server, and vendor SDKs. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define metadata, authorization request, state/PKCE, callback, token exchange, token vault, identity endpoint, identity-proof policy, refresh, revocation, and provider error contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST bind provider, state, redirect, and PKCE; refuse access tokens as identity without an explicit trusted identity endpoint and verified stable subject; encrypt refresh tokens; serialize refresh; handle revoked grants; prove provider profiles separately without claiming generic OAuth interoperability. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Static provider configuration MUST define authorization/token/identity/
  refresh/revocation endpoints, stable subject extraction, client
  authentication, scopes, PKCE, redirect and explicit email-verification policy.
- Authorization/callback MUST bind provider, organization, tenant, state,
  redirect and PKCE and MUST reject mix-up, code substitution and callback
  replay under shared redirect URIs.
- An access token alone MUST NOT be identity proof. The adapter MUST call the
  configured authenticated identity endpoint and validate a stable provider
  subject and organization/domain evidence required by SSO policy.
- Provider tokens MUST pass to the SSO token vault with redaction, encryption,
  tenant/organization/provider/subject/purpose binding, version and retention
  policy; the adapter MUST NOT retain an independent recoverable copy.
  Serialized refresh, rotation/reuse, revocation and unknown outcomes MUST
  remain attributable through the `EnterpriseTokenVault` contract.
- Every successful identity-endpoint response, including repeat login for an
  existing linked subject, MUST identify absent, null, verified and provider-
  authoritative attributes and the provider-profile version for SSO sync. The
  adapter MUST NOT preserve prior role or membership authority on its own.
- Provider errors, HTTP redirects, bodies, JSON depth/size and custom field
  mappings MUST be bounded before use and MUST NOT create roles from unknown
  claims.
- Each declared provider profile requires pinned documentation/fixtures and
  separate interoperability evidence; one generic OAuth server does not prove
  all providers.

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
