# Goal: pkg/identity/reference

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Execution metadata

- Unit: `identity/reference`
- Canonical module: `pkg/identity/reference`
- Canonical goal after scaffolding: `pkg/identity/reference/.ai/GOAL.md`
- Requires: `identity/http` and every concrete adapter listed explicitly in `INVENTORY.md`
- Consumes existing primitives: `postgres`, `migrations`, `capability/postgres`, `capability/valkey`, `secret-envelope`, `outbox`, `workflow`, `telemetry`
- Unlocks after verification: `identity/identitytest`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and start only from the
coordinator's committed assignment after every listed transport and adapter
prerequisite is verified. Build the complete opinionated PostgreSQL/Valkey
identity-platform composition that proves all in-scope features work together
without consumer-written stores, handlers or wiring.

## Ownership boundary

This module owns dependency assembly, configuration schema/validation,
migration ordering, key/secret/provider wiring, lifecycle/startup/shutdown,
health/readiness, selected deployment profiles and the executable end-state
journey harness. It does not reimplement feature behavior, make concrete
adapters dependencies of `identity/http`, own an admin UI, hide credentials in
global state, or weaken a package contract to make composition easier.

## Required composition contract

The public API MUST define immutable `Config`, validated deployment profile,
dependency overrides for testing, migration plan, `Application` lifecycle,
`http.Handler`/server construction, readiness state, shutdown and reconciliation
entry points. Zero/default behavior MUST be secure and explicit. Configuration
MUST distinguish required, optional and mutually exclusive settings and return
all safe validation failures before starting listeners or migrations.

## Selected complete profile

- PostgreSQL MUST durably back identity, password, sessions, OTP, MFA,
  WebAuthn/passkeys, social OAuth tokens, API keys, impersonation grants,
  organizations, SSO, SCIM and OAuth/OIDC-server state through their dedicated
  adapters.
- Valkey MUST be selectable for session secondary/cache state and risk counters
  with explicit PostgreSQL-only and PostgreSQL-plus-Valkey profiles. A Valkey
  outage MUST follow each adapter's declared degradation policy.
- Capability one-time consumption MUST use the existing PostgreSQL adapter by
  default and MAY use its Valkey adapter only under an explicitly documented
  durability/failover profile.
- All four CAPTCHA adapters and HIBP MUST be configurable signals. Disabled
  providers MUST remain absent rather than replaced by allow-all fakes.
- Delivery, social providers, One Tap, OAuth proxy, enterprise SSO, SCIM and
  OAuth/OIDC signing/client keys MUST have validated configuration, secret
  ownership, rotation and safe readiness semantics.
- `identity/http` MUST receive only public feature/protocol interfaces. The
  reference package is the sole place where concrete adapter imports become a
  mandatory all-features dependency.
- Core HTTP rate state MUST use the selected PostgreSQL or Valkey-backed
  `rate-limit` adapter with explicit outage/fallback semantics; process memory
  MUST not be the authoritative multi-instance profile.
- Typed extension modules MUST register routes, OpenAPI components and owned
  migration contributors through the same validated composition path as
  built-in modules; no extension may mutate global registries.

## Startup, migration, and recovery behavior

- Construction MUST be side-effect free. Start MUST order configuration/key
  validation, dependency connections, migration compatibility checks,
  background workers and HTTP readiness explicitly; partial failure MUST close
  every acquired resource.
- Migrations MUST run through one bounded ownership mechanism, preserve each
  module's independent migration identity/order and refuse incompatible or
  missing migrations. This module MUST not copy package migration SQL.
- Readiness MUST fail safely for unavailable mandatory stores/keys and report
  optional-provider degradation without exposing endpoints or secrets.
- Shutdown MUST stop admission, drain bounded requests/workers, flush owned
  evidence/outbox state where guaranteed, close resources once and return
  aggregated safe failures without goroutine leaks.
- Reconciliation entry points MUST cover every package-declared unknown outcome
  and be resumable/idempotent. The composition MUST not translate an adapter's
  unknown result into success.
- Public operational APIs MUST enumerate/generate the selected schema plan,
  apply owned migrations, generate cryptographically strong configuration
  secrets and emit a bounded redacted diagnostic summary. They MUST NOT expose
  provider credentials, DSNs, keys, token material or raw configuration.
- Backup/restore and key/provider rotation rehearsal MUST prove the documented
  validity or revocation of sessions, credentials, grants, links and replay
  state after recovery.

## End-state acceptance

The package's integration suite MUST execute every journey in `END_STATE.md`
through the real `identity/http` handler and selected real PostgreSQL/Valkey
profiles. Provider/specification evidence MUST remain attributable to the
owning package; the composition suite MUST additionally prove cross-feature
effects such as password-change session revocation, SSO organization mapping,
SCIM deprovisioning, MFA/passkey step-up, API-key ownership, consent and global
compromise revocation.

Exact coverage/mutation for owned composition code, race/stress/leak, startup/
shutdown/failure injection, migration/restore rehearsals, resource benchmarks,
clean-consumer, API/docs/examples/changelog and supply-chain gates are REQUIRED.
The unit MUST remain unverified if any selected store/provider is replaced by
consumer glue, an adapter is imported into HTTP to bypass composition, startup
leaks resources, migrations are ambiguous, or an end-state journey is unproved.
