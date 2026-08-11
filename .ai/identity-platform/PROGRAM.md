# Identity Platform Program

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Deliver a storage-neutral, independently releasable Go identity platform whose
composed backend capability is competitive with the pinned Better Auth
baseline in `BETTER_AUTH_PARITY.md`. `pkg/authentication` remains limited to
credential-to-principal validation. This program owns product workflows,
issuance, sessions, federation, provisioning, administration, HTTP composition,
and their adapters through explicit packages.

The goals remain in this planning tree until modules exist. On integration,
the coordinator MUST move each goal unchanged to
the exact canonical goal path declared in that goal, update the inventory, and
register the module.

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
- The public transport is standard-library `net/http`; packages below it
  remain transport-neutral. `identity/http` depends on feature/protocol
  contracts, while `identity/reference` is the only mandatory-all-adapters
  composition. Concrete PostgreSQL, Valkey and provider adapters MUST NOT
  become mandatory dependencies of the reusable HTTP module.

## Exact out-of-scope and divergence categories

Billing and payment plugins, SIWE, MCP authentication, agent authentication,
JavaScript framework clients, and database engines beyond the selected
PostgreSQL and Valkey profiles are product exclusions. Lead tracking, CLI
scaffolding, and community catalogs are non-capabilities; personal SCIM is an
unselected deployment profile; database-less OAuth state/provider-token
cookies are a security divergence. None authorizes adding those integrations.
Product exclusion is not permission to leave an in-scope capability partial. Upstream changes after
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

Completion requires all 61 inventory units to be `verified`, every in-scope
row in `BETTER_AUTH_PARITY.md` to have executable proof, and every composed
journey and cross-cutting property in `END_STATE.md` to pass against final
inputs. No row may remain partial, depend on undocumented application glue, or
be represented only by a primitive that lacks the required workflow.

The coordinator artifacts close the implementation choices that package goals
consume. `API_OPERATIONS.md` owns the complete transport operation catalog;
`UPSTREAM_DISPOSITIONS.md` owns the disposition of every pinned upstream
surface; `UPSTREAM_SURFACE.json` pins the machine-verifiable source objects and
every exact source-item -> disposition-row -> capability -> operation-ID edge,
with operation owners resolving to registered goals; independent inventory
digests or owner-has-some-operation checks are insufficient;
`PROTOCOL_BASELINES.md` pins supported protocol revisions and profiles;
`PROTOCOL_CONFORMANCE_MANIFEST.json` binds every selected source and
conformance tool to its immutable revision, retrieved digest, license and
consumers; `SECURITY_EVENTS.md` owns the interoperable audit taxonomy;
`TRANSACTION_CONTRACT.md` owns cross-module atomicity, idempotency,
compensation, and ambiguous-outcome rules; `LIFECYCLE_CASCADES.md` owns
destructive and privilege-changing cascades; `LIFECYCLE_CONSUMERS.md` owns the
versioned exact consumer set for every cascade; `REFERENCE_CONFIGURATION.md`
owns exact deployable defaults; `CONFIGURATION_CATALOGS.json` owns the
versioned provider/CAPTCHA instance IDs and checksums used for template
expansion; and `PREFLIGHT_EVIDENCE.md` records the
coordinator's versioned, attributable preflight result. Every artifact MUST be
complete and structurally valid before the first worker assignment. Package
goals MUST consume these decisions and MUST NOT reopen them independently.

`validate.rb` proves only the current files' structural invariants. A static
file validator cannot prove Git ancestry, commit reachability, prior ledger
versions, exact generation increments, or whether a same-status row update
changed only permitted fields. Before every coordinator state commit, the
transition-check procedure in `ORCHESTRATOR_GOAL.md` MUST compare the proposed
inventory and ledger to their exact Git parent and prove those historical
properties. Passing `validate.rb` MUST NOT be reported as ancestry or
transition-history proof.

The configurable package contracts MUST compose under the topology in
`REFERENCE_PROFILE.md` and the exact defaults and bounds in
`REFERENCE_CONFIGURATION.md`. A package worker MUST NOT choose an incompatible
local default merely because its isolated tests pass.

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
