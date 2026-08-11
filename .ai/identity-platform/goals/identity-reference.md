# Goal: pkg/identity/reference

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/reference`
- Canonical module: `pkg/identity/reference`
- Canonical goal after scaffolding: `pkg/identity/reference/.ai/GOAL.md`
- Requires: `identity/http` and every concrete adapter listed explicitly in `INVENTORY.md`
- Consumes existing primitives: `postgres`, `migrations`, `capability/postgres`, `capability/valkey`, `audit/postgres`, `authorization/postgres`, `authorization/valkey`, `rate-limit/postgres`, `rate-limit/valkey`, `idempotency/postgres`, `idempotency/valkey`, `outbox/postgres`, `workflow/postgres`, `secret-envelope`, `outbox`, `workflow`, `telemetry`
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
- Delivery intents, attempts and terminal outcomes MUST use
  `identity/delivery/postgres`; anonymous identities and
  upgrade/reconciliation state MUST use `identity/anonymous/postgres`;
  immutable security audit MUST use
  `audit/postgres`; and enterprise-domain ownership MUST use the canonical
  `sso/domain-verification` package and its PostgreSQL-backed proof state. Consumer
  callbacks MAY send bounded email/SMS intents but MUST NOT replace durable
  queueing, workflow or outcome persistence.
- Authorization decisions and policy/version state MUST compose the existing
  `authorization/postgres` authoritative adapter and the optional
  `authorization/valkey` secondary profile. Core HTTP rate limits MUST select
  `rate-limit/postgres`; `rate-limit/valkey` MAY cache only the positive
  metadata permitted by the reference configuration. Mutating HTTP rows that
  declare key idempotency MUST select `idempotency/postgres` and MUST NOT use
  `idempotency/valkey` as authority. Each Valkey selection MUST retain the
  owning package's explicit cache, invalidation and outage contract.
- Durable asynchronous publication and orchestration MUST use
  `outbox/postgres` and `workflow/postgres` for every package-declared intent,
  retry, compensation, cleanup and reconciliation path. The reference package
  MUST wire package-owned contributors and workers; it MUST NOT implement a
  second queue, poll package tables directly or leave applications to provide
  orchestration.
- All four CAPTCHA adapters and HIBP MUST be configurable signals. Disabled
  providers MUST remain absent rather than replaced by allow-all fakes.
- Delivery, social providers, One Tap, OAuth proxy, enterprise SSO, SCIM and
  OAuth/OIDC signing/client keys MUST have validated configuration, secret
  ownership, rotation and safe readiness semantics.
- `identity/http` MUST receive only public feature/protocol interfaces. The
  reference package is the sole place where concrete adapter imports become a
  mandatory all-features dependency.
- Core HTTP rate state MUST use `rate-limit/postgres` as authority with the
  declared outage semantics. `rate-limit/valkey` MAY cache only permitted
  positive metadata; neither Valkey nor process memory may become the
  authoritative multi-instance profile.
- Typed extension modules MUST register routes, OpenAPI components and owned
  migration contributors through the same validated composition path as
  built-in modules; no extension may mutate global registries.

### RiskEvidence issuance composition

The reference composition MUST wire `identity.risk.evaluate` through
`identity/postgres`, `identity/risk/postgres`, `audit/postgres`, and
`outbox/postgres` so an evidence-producing decision and `tx.risk_evidence.issue`
commit with one command result before any reference is returned. The reference
composition MUST invoke `identity.risk.evaluate` for both phone-reset phases and
MUST return only its opaque reference and safe freshness/one-use metadata; raw
signals, provider evidence, embedded evidence payloads, digests, signatures, journal
identifiers, and persistence records MUST NOT cross into `identity/phone`.
Denied, failed, unknown, and matching replay outcomes MUST preserve the core
operation semantics without local reconstruction or a second issuance path.

### Phone recovery initiation composition

For `identity.phone.password-reset-request`, the reference composition MUST
enlist `identity/postgres`, `identity/risk/postgres`, `identity/otp/postgres`,
`capability/postgres`, `audit/postgres`, and `outbox/postgres` in one coordinator
unit of work before the reservation transaction's first write. The reservation
transaction MUST reserve only the command and initiation RiskEvidence; the
domain commit MUST apply and finalize that evidence while issuing the OTP
challenge and reset capability and recording audit/outbox and command-result
state. Acceptance MUST prove one concurrent reservation winner, stable
same-command replay without replacement issuance, exact-generation takeover,
fail-closed unknown recovery, and no partial challenge, capability, audit,
outbox, command-result, or RiskEvidence finalization.

### Phone recovery completion composition

For `identity.phone.password-reset-complete`, the reference composition MUST
enlist `identity/risk/postgres`, `identity/otp/postgres`, `capability/postgres`,
`identity/password/postgres`, and `identity/session/postgres` in one coordinator
unit of work before the reservation transaction's first write. Acceptance MUST
prove all five contributors reserve and finalize together, retry and recover
the same command generation, and never permit a partial subset or a second
command to reuse RiskEvidence, OTP, or reset capability.

### Tenant and bootstrap composition

The reference composition MUST own one tenant resolver used consistently by
HTTP authentication, stores, authorization, idempotency, audit, delivery,
workflow, provider routing and instrumentation. It MUST resolve only from an
explicit deployment mapping or trusted network facts, reject missing,
conflicting and unknown tenants before state access, and never infer tenant
identity from untrusted host/header/body values. Cross-tenant identifiers MUST
remain indistinguishable from absent identifiers at public boundaries.

The reference package MUST provide an explicit one-time administrator
bootstrap operation. It MUST require an empty eligible authority scope, a
short-lived single-use bootstrap capability supplied out of band, exact tenant
binding and transactional creation of the administrator, authorization state
and immutable audit record. It MUST be disabled after success, reject races and
partial/unknown outcomes safely, never expose an unauthenticated standing HTTP
route, and provide a documented recovery procedure that cannot silently create
a second authority root.
Global compromise is invoked and coordinated by this composition, but the
public `identity` unit owns the authority transition and acknowledgement and
`identity/postgres` durably persists its global version, cascade generation,
and owner checkpoint. This composition MUST NOT become a parallel owner or emit
a duplicate semantic acknowledgement.
Its typed configuration MUST select only the offline operator transport,
default disabled, bound to a short-lived one-time capability; enabling it MUST
not change the `identity/http` route or OpenAPI manifests.
The bootstrap and all later platform-administrator changes MUST persist roles,
permission statements and subject assignments as versioned
`authorization/postgres` authority, not identity metadata or configuration.
The reference transaction coordinator MUST compose identity creation,
role/assignment mutation, authority-version invalidation, immutable audit and
required outbox state with recoverable unknown outcomes; the optional
`authorization/valkey` layer is positive cache only and MUST NOT become an
assignment authority.

## Startup, migration, and recovery behavior

- Construction MUST be side-effect free. Start MUST order configuration/key
  validation, dependency connections, migration compatibility checks,
  background workers and HTTP readiness explicitly; partial failure MUST close
  every acquired resource.
- Migrations MUST run through one bounded ownership mechanism, preserve each
  module's independent migration identity/order and refuse incompatible or
  missing migrations. This module MUST NOT copy package migration SQL.
- Operational migration APIs MUST separate `Check`, `Plan` and `Apply`.
  `Check` is read-only and reports compatibility/current/required versions;
  `Plan` is deterministic, ordered by contributor ownership and includes a
  machine-checkable digest; `Apply` requires that exact plan/digest, obtains
  bounded ownership, records each completed contributor atomically and resumes
  after interruption without replaying completed work. Startup MUST check but
  MUST NOT implicitly apply migrations unless an explicit deployment profile
  enables the reviewed apply policy.
- Readiness MUST fail safely for unavailable mandatory stores/keys and report
  optional-provider degradation without exposing endpoints or secrets.
- Shutdown MUST stop admission, drain bounded requests/workers, flush owned
  evidence/outbox state where guaranteed, close resources once and return
  aggregated safe failures without goroutine leaks.
- Reconciliation entry points MUST cover every package-declared unknown outcome
  and be resumable/idempotent. The composition MUST NOT translate an adapter's
  unknown result into success.
- Public operational APIs MUST enumerate/generate the selected schema plan,
  apply owned migrations, generate cryptographically strong configuration
  secrets and emit a bounded redacted diagnostic summary. They MUST NOT expose
  provider credentials, DSNs, keys, token material or raw configuration.
- Backup/restore and key/provider rotation rehearsal MUST prove the documented
  validity or revocation of sessions, credentials, grants, links and replay
  state after recovery.

The application MUST own bounded delivery, outbox, workflow, cleanup,
reconciliation and audit-retention workers with explicit admission, leases,
concurrency, retry/backoff, poison-item handling, checkpointing and drain order.
Audit retention MUST preserve immutable actor/tenant/correlation integrity,
legal holds and required security evidence while applying documented
data-minimization, partitioning and deletion/anonymization boundaries. No
worker may begin before migration/key readiness or continue after its owning
shutdown context is canceled.

The reference composition MUST treat the `audit/postgres` durable runtime
retention policy as sole authority after it is initialized exactly once from
the startup bootstrap defaults. Concurrent first starts MUST converge on one
version-1 row. Every restart MUST load the durable version and MUST NOT
overwrite it from configuration; a redacted diagnostic MAY report differing
bootstrap defaults but MUST NOT change readiness or policy. Only
`identity.audit-retention.policy.update` may replace the durations with an
expected-version transaction. Both unset/reset are unsupported; restoring an older
duration requires another explicit versioned update. Retention plans MUST pin
the policy version and legal-hold checkpoint and abort before deletion if
either changes.
The version-1 initialization MUST emit
`identity.audit_retention.change_policy` with the system bootstrap actor and
selected defaults atomically with the durable row; it MUST NOT synthesize or
duplicate the event on restart.

Instrumentation MUST cover HTTP operations, authorization, rate/idempotency
decisions, provider/store calls, delivery/outbox/workflow transitions,
migrations and worker lifecycle with bounded stable labels and trace
correlation. Exporter failure MUST NOT alter security decisions or block
shutdown; secrets, credentials, token digests, DSNs, provider payloads and PII
MUST be absent from metrics, traces, logs and diagnostics.

The reference application MUST expose process-only liveness and dependency-
aware readiness probes through `identity/http`. Readiness MUST cover mandatory
PostgreSQL, selected authoritative Valkey-backed contracts, migration
compatibility, active signing/envelope keys and required worker admission;
optional provider degradation is reported only as bounded non-secret status.
Startup, drain and shutdown transitions MUST be observable and race-safe.

Backup/restore rehearsals MUST use a production-shaped snapshot plus encrypted
key material and prove tenant isolation, referential integrity, replay state,
worker checkpoints and the documented validity/revocation of every credential
class. Key rotation rehearsals MUST cover envelope, session, capability,
OAuth/OIDC signing, API/SCIM credential and provider/client-secret keys,
overlap/retirement/compromise policy, mixed old/new binaries and rollback
boundaries. A restore or rotation with unverifiable input identity remains a
failed readiness condition.

## Adoption and operations documentation

Before verification, the module MUST publish cross-cutting documentation for
architecture and trust boundaries; complete configuration and secret
ownership; tenant resolution; authorization/admin bootstrap; canonical HTTP
operations and OpenAPI export; migration check/plan/apply; delivery and worker
operations; instrumentation and probes; audit/privacy retention; provider and
store degradation; backup/restore; key and client-secret rotation; global
compromise revocation; unknown-outcome reconciliation; and upgrade/rollback
compatibility. Each runbook MUST name the observable decision points and safe
recovery actions without embedding environment credentials.

Its implementation and adoption material MUST consume the final, non-pending
contracts in `API_OPERATIONS.md`, `REFERENCE_CONFIGURATION.md`,
`TRANSACTION_CONTRACT.md`, `LIFECYCLE_CASCADES.md`, `SECURITY_EVENTS.md`,
`PROTOCOL_BASELINES.md` and `UPSTREAM_DISPOSITIONS.md`. A placeholder,
unresolved decision or locally reinterpreted value in any of those documents
blocks reference verification.

A clean production example MUST use only public modules from a fresh consumer,
configure explicit tenant/base URL/proxy/origin/cookie/timeouts, PostgreSQL and
selected Valkey adapters, authorization/rate/idempotency/audit/delivery/
outbox/workflow stores, keys, providers, senders, telemetry and probes, execute
check-plan-apply as separate deployment steps, bootstrap the first
administrator out of band, start and drain workers/server, and contain no
allow-all policy, fake provider, in-memory authority, hidden migration or
development bypass.

## End-state acceptance

The package's integration suite MUST execute every journey in `END_STATE.md`
through the real `identity/http` handler and selected real PostgreSQL/Valkey
profiles. Provider/specification evidence MUST remain attributable to the
owning package; the composition suite MUST additionally prove cross-feature
effects such as password-change session revocation, SSO organization mapping,
SCIM deprovisioning, MFA/passkey step-up, API-key ownership, consent and global
compromise revocation.

This package's own pre-verification suite MUST exercise those journeys directly
through public package and HTTP contracts without importing
`identity/identitytest`; otherwise the inventory edge would form a verification
cycle. After this unit and then `identity/identitytest` are independently
verified, the coordinator MUST run the final integrated end-state suite using
the public `identity/identitytest` helpers against the verified reference
application. That coordinator suite is program acceptance evidence and MUST
NOT be retroactively treated as a prerequisite of either package's unit gate.

Exact coverage/mutation for owned composition code, race/stress/leak, startup/
shutdown/failure injection, migration/restore rehearsals, resource benchmarks,
clean-consumer, API/docs/examples/changelog and supply-chain gates are REQUIRED.
The unit MUST remain unverified if any selected store/provider is replaced by
consumer glue, an adapter is imported into HTTP to bypass composition, startup
leaks resources, migrations are ambiguous, or an end-state journey is unproved.
