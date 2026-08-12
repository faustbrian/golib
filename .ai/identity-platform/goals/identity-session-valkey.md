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
- Public contracts: unit ID `contract:unit:identity/session/valkey:v1`; owned operation IDs: none
- Requires: `identity/session`
- Consumes existing primitives: `cache`, `identifier`, `audit`
- Unlocks after verification: `identity/reference`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/session/valkey` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/session/valkey` module that owns
a bounded low-latency positive cache of authoritative PostgreSQL session
snapshots and generation-bound invalidation checkpoints. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns only a positive projection of session state. It is never an
authoritative session store and does not own issuance, rotation, refresh-family
single-winner decisions, revocation, maximum-session enforcement, active
organization selection, durable identity/session state, or cookie policy.
Those exclusions MUST remain outside its public API and dependency graph.
`identity/session/postgres` and the other dimension owners in
`LIFECYCLE_CASCADES.md` remain authoritative on every cache miss, epoch loss,
invalidated entry, or stronger-consistency boundary.

## Required public contract

The design MUST define key schema, compare-and-set invalidation atomicity, TTL
ownership, deployment epoch, cluster-slot policy, outage behavior, and
reconciliation limits. Its callable cache surface MUST be limited to typed
positive-snapshot lookup, generation-checked fill/replace, exact-key/family
invalidation, generation checkpoint/status, and bounded orphan cleanup. It MUST
NOT expose issue, refresh, rotate, revoke, list-authoritative-sessions,
set-active-organization, or enforce-session-limit operations. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST prove that only a primary-authority snapshot
may populate the cache; a stale fill cannot win after invalidation; no miss,
eviction, flush, restart, replication lag, or failover can become an
authenticated result; scans and values remain bounded; and declared Valkey
versions interoperate. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Key namespaces MUST bind deployment, tenant and purpose and use hash tags
  deliberately for supported cluster atomicity. Raw session tokens MUST never
  appear in keys, values visible to diagnostics, or channel messages.
- Every supported profile MUST declare itself `positive-cache`; construction
  MUST reject authoritative, secondary-source, write-behind, offline-fallback,
  or cache-only authentication profiles. A hit is usable only when it contains
  the complete durable-session snapshot and every required authority version,
  is within the five-minute bound, and matches the current healthy deployment
  epoch. A miss or unusable hit is `miss`, never denial proof or authentication
  proof, and requires the caller to read all primary authorities.
- Generation-checked fill/replace, stale-fill rejection, exact-key/family
  invalidation, checkpoint publication and TTL refresh MUST be atomic within the
  declared topology or return unsupported at construction. Session issuance,
  refresh/rotation, revocation, family single-winner decisions,
  maximum-session enforcement, and active organization selection MUST execute
  in `identity/session/postgres`; this cache may only discard or replace their
  committed projections.
- Eviction, flush, failover, replication lag, MOVED/ASK, partial pipeline and
  script-cache loss MUST map to explicit unavailable or unknown outcomes.
- Projection indexes used for bounded family invalidation MUST be cleaned with
  the cache key; they MUST NOT be exposed as an authoritative list or used to
  decide revoke-all completeness. Orphan repair MUST discard entries and MUST
  NOT reconstruct, authorize, or resurrect revoked or expired sessions.
- Real standalone and declared cluster-profile tests MUST cover clock/TTL
  boundaries, failover, hot keys, restart, concurrent invalidation versus fill,
  and a primary rotation/revocation racing a stale projection. A mock Redis
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
- Active organization is read and changed only through the authoritative
  session/PostgreSQL command. A cached enrichment MAY repeat the committed
  session-scoped selection and organization version, but cache hit, absence,
  CAS, TTL refresh, or eviction MUST NOT choose, clear, or validate a selection.

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
