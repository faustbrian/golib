# Goal: pkg/identity/session/valkey

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/session/valkey`
- Canonical module: `pkg/identity/session/valkey`
- Canonical goal after scaffolding: `pkg/identity/session/valkey/.ai/GOAL.md`
- Requires: `identity/session`
- Consumes existing primitives: `cache`, `identifier`, `audit`
- Unlocks after verification: `identity/reference`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/session/valkey` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/session/valkey` module that owns low-latency Valkey session storage with atomic rotation and revocation. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns low-latency Valkey session storage with atomic rotation and revocation. It does not own durable identity storage and cookie policy. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define key schema, Lua or transactional atomicity, TTL ownership, family index, cluster-slot policy, outage behavior, and reconciliation limits. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST prove single-winner refresh; prevent resurrection after revocation; handle eviction and failover honestly; bound scans and values; interoperate with supported Valkey versions. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Key namespaces MUST bind deployment, tenant and purpose and use hash tags
  deliberately for supported cluster atomicity. Raw session tokens MUST never
  appear in keys, values visible to diagnostics, or channel messages.
- The adapter MUST declare whether it is authoritative session storage,
  secondary storage or cookie-cache support. Each profile MUST define fallback
  and MUST NOT silently accept stale or missing state as authenticated.
- Issue, rotate, compare-and-delete, family revoke, active selection,
  maximum-session enforcement, version invalidation and TTL refresh MUST be
  atomic within the declared topology or return unsupported at construction.
- Eviction, flush, failover, replication lag, MOVED/ASK, partial pipeline and
  script-cache loss MUST map to explicit unavailable or unknown outcomes.
- Indexes for list/revoke-all MUST be bounded and cleaned with the primary key;
  orphan repair MUST NOT resurrect revoked or expired sessions.
- Real standalone and declared cluster-profile tests MUST cover clock/TTL
  boundaries, failover, hot keys, restart and concurrent rotation. A mock Redis
  protocol server is not interoperability proof.
- Every supported profile MUST apply authoritative invalidation versions and
  events from `.ai/identity-platform/LIFECYCLE_CASCADES.md`. Replication lag,
  eviction or cache fallback MUST NOT resurrect a session after identity,
  password, factor, tenant or global revocation; audit outcomes MUST use
  `.ai/identity-platform/SECURITY_EVENTS.md`.
- Persistent (`rememberMe`) and non-persistent sessions MUST retain identical
  server-side revocation and version checks. The non-persistent profile MUST
  not become an untracked bearer token, and its exact TTL/renewal behavior MUST
  match `.ai/identity-platform/REFERENCE_CONFIGURATION.md`.
- Authentication time, method and assurance stored here MUST remain versioned
  evidence. The adapter MUST NOT issue or upgrade reauthentication proof and
  MUST reject atomic updates based on stale proof/session versions.

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
