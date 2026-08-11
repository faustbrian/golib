# Identity Platform Program

## Objective

Deliver a storage-neutral, independently releasable Go identity platform whose
composed backend capability is competitive with the pinned Better Auth
baseline in `BETTER_AUTH_PARITY.md`. `pkg/authentication` remains limited to
credential-to-principal validation. This program owns product workflows,
issuance, sessions, federation, provisioning, administration, HTTP composition,
and their adapters through explicit packages.

The goals remain in this planning tree until modules exist. On integration,
the coordinator MUST move each goal unchanged to
`pkg/<module>/.ai/GOAL.md`, update the inventory, and register the module.

## Product boundary decisions

- There is no `identityadmin` or `identity/management` module and no admin UI.
  User status belongs to `identity`, session control to `identity/session`,
  organization administration to `organization`, authorization decisions to
  `authorization`, and privileged impersonation to `identity/impersonation`.
  `identity/http` exposes these operations as a coherent backend API.
- `sso` is the enterprise-federation boundary. `sso/oidc`, `sso/oauth2`, and
  `sso/saml` isolate protocol behavior without introducing a vague federation
  facade.
- `webauthn` owns protocol ceremonies and cryptographic verification;
  `passkey` owns identity-facing discoverable-credential lifecycle. WebAuthn
  also supports non-passkey security-key use, so these remain separate.
- There is no generic one-time-token package. `capability` supplies signed
  payloads, issuer/audience/resource/action binding, time checks, rotation,
  revocation, `MaxUses=1`, atomic consumption, replay storage, and stable
  failures. Each workflow owns lookup, delivery, callback, transaction, and
  state-transition semantics.
- CAPTCHA has a provider-neutral contract and adapters for Google reCAPTCHA,
  Cloudflare Turnstile, hCaptcha, and CaptchaFox.
- HIBP Pwned Passwords is in scope through its k-anonymity range protocol. It
  produces risk evidence and never owns password policy or sends complete
  passwords or hashes.
- Social OAuth has one built-in provider-profile catalog. Provider-specific
  SDK wrappers remain out of scope.
- The final public transport is standard-library `net/http`; packages below it
  remain transport-neutral.

## Explicit exclusions

Billing and payment plugins, SIWE, MCP authentication, agent authentication,
lead tracking or analytics, JavaScript framework clients, and database engines
beyond the selected PostgreSQL and Valkey profiles are excluded. Exclusion is
not permission to leave an in-scope capability partial. Upstream changes after
the pinned revision become a separately approved parity audit.

## Existing primitives

`authentication`, `authentication/jwt`, `authentication/oidc`,
`authorization`, `password`, `capability`, `tenancy`, `audit`, `rate-limit`,
`secret-envelope`, `identifier`, `postgres`, `migrations`, `outbox`,
`workflow`, `webhook`, `http-client`, `openapi`, and `telemetry` remain
lower-level primitives. JWT/JWK and OIDC remain validation-only. Authorization
server issuance, grants, discovery, consent, and JWKS publication belong to
`oauth-server` and `oauth-server/oidc`.

## Program completion contract

Completion requires all 48 inventory units to be `verified`, every in-scope
row in `BETTER_AUTH_PARITY.md` to have executable proof, and every composed
journey and cross-cutting property in `END_STATE.md` to pass against final
inputs. No row may remain partial, depend on undocumented application glue, or
be represented only by a primitive that lacks the required workflow.

The complete reference server MUST use standard `net/http`, PostgreSQL, and
Valkey and MUST exercise provider integrations where the goals require them.
Unavailable provider or infrastructure evidence is an explicit blocker for
the affected claim, not a pass.

## Delivery policy

`ORCHESTRATOR_GOAL.md` is the sole program-level execution goal. The
coordinator assigns exactly one inventory unit per worker using
`WORKER_PROMPT.md`. A worker MUST NOT start before every `Requires` unit is
integrated and verified. The coordinator alone owns shared state, manifests,
integration, reverse-dependant verification, parity closure, and end-state
proof.
