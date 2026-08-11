# Identity Platform Program

## Objective

Deliver the missing identity product as independently releasable Go modules
without expanding `pkg/authentication` beyond credential-to-principal
validation. Each execution unit has its own goal, explicit prerequisites, and
a readiness state in `INVENTORY.md`.

The goals remain in this planning tree until their modules exist; this avoids
fake `pkg/...` directories with no `go.mod`. The first authorized
implementation change for a unit MUST move its goal unchanged to
`pkg/<module>/.ai/GOAL.md` and update the inventory goal path in the same
coherent batch.

## Product boundary decisions

- There is no `identityadmin` or `identity/management` module and no UI in
  this program. Account suspension belongs to `identity`, session
  administration belongs to `identity/session`, and privileged impersonation
  belongs to `identity/impersonation`. Applications may build admin UIs over
  those APIs.
- There is no generic `federation` module. `sso` is the concrete enterprise
  product boundary; protocol modules are `sso/oidc`, `sso/oauth2`, and
  `sso/saml`.
- `webauthn` and `passkey` are separate. WebAuthn owns protocol ceremonies,
  cryptographic verification, RP/origin/challenge policy, authenticators, and
  security-key profiles. Passkey owns identity-facing discoverable-credential
  lifecycle and passkey-first signup/signin. Passkeys use WebAuthn, while
  WebAuthn also supports non-passkey second-factor and security-key use.
- There is no generic one-time-token module. Identity workflows reuse
  `capability` for canonical signed payloads, issuer/audience/resource/action
  binding, expiry and not-before checks, key IDs and rotation, revocation,
  `MaxUses=1`, atomic consumption, replay stores, and stable failures. Each
  workflow still owns subject lookup, semantic action, delivery, callback,
  transaction boundaries, and post-consumption state transition.
- CAPTCHA support includes a provider-neutral contract plus Google reCAPTCHA
  and Cloudflare Turnstile adapters. Provider response semantics remain
  isolated in adapters; hCaptcha is not in current scope.
- HIBP Pwned Passwords support is in scope as a k-anonymity adapter under
  `identity/risk`. It supplies a breach signal; it does not own password
  policy or send complete password hashes to the provider.
- Billing, SIWE, MCP authentication, agent authentication, and unspecified
  speculative provider integrations are outside this program.

## Existing primitives

`authentication`, `authentication/jwt`, `authorization`, `password`,
`capability`, `tenancy`, `audit`, `rate-limit`, `secret-envelope`,
`identifier`, `postgres`, `migrations`, `outbox`, `workflow`,
`webhook`, `http-client`, `openapi`, and `telemetry` remain lower-level
primitives. JWT/JWK and OIDC validation remain validation-only. Token issuance,
clients, grants, consent, discovery, and JWKS publication belong to
`oauth-server` and `oauth-server/oidc`.

## Program completion contract

The program is complete only when every in-scope unit is `verified`, the DAG
is acyclic, every module is independently releasable, and the composed
reference journeys pass: password signup/signin/logout/reset and
breached-password assessment; email and phone verification; magic-link and
OTP signin; MFA; passkey signup/signin; social
OAuth linking; managed API keys; organization invitations and membership;
enterprise SSO; SCIM provisioning; and OAuth/OIDC authorization-server flows.

## Delivery policy

An agent claims exactly one inventory unit. The agent MUST read
`COMMON_REQUIREMENTS.md`, `DEPENDENCIES.md`, `INVENTORY.md`, and its goal
before work. It MUST NOT begin while the unit is `proposed` or `blocked`.
A coordinator may mark a unit `ready` only after every `Requires` unit is
`verified`. Implemented or partially verified prerequisites do not satisfy
the start gate.
