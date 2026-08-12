# Goal: pkg/identity/session/postgres

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/session/postgres`
- Canonical module: `pkg/identity/session/postgres`
- Canonical goal after scaffolding: `pkg/identity/session/postgres/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/session/postgres:v1`; owned operation IDs: none
- Requires: `identity/session`, `identity/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `audit`
- Unlocks after verification: `identity/impersonation/postgres`, `identity/reference`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/session/postgres` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/session/postgres` module that owns durable relational session store and transactional revocation. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns durable relational session store and transactional revocation. It does not own cookie serialization and credential verification. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define digest-indexed session schema, rotation transaction, family revocation, device query, expiry cleanup, and migration contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

The adapter MUST publish an open versioned `identitypostgres.Contributor` for
composition-root registration. Its `Apply` accepts the generation-bound
`identitypostgres.Work` and a closed `session.SessionCommand`; it stages the
session row, token transition, complete `session.AuthoritySnapshot`, required
audit and outbox effects without beginning, committing, rolling back or
retaining the transaction. The constructor MUST NOT accept a coordinator,
Work, carrier, enlister or contributor registry. Core `identity/session`
contracts remain storage-neutral and contain no concrete PostgreSQL type.

## Required behavior

The implementation and tests MUST race refresh rotation; revoke a token family atomically; survive commit ambiguity; clean expiry without deleting active sessions; preserve tenant isolation and stable pagination. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- The schema MUST support opaque sessions, families, active-account selection,
  device metadata, last-login-method persistence and one durable, immutable
  authority snapshot without storing raw bearer tokens. That snapshot MUST
  store the exact scope key and positive version for `global`, `tenant`, `user`,
  `identifier`, `credential`, `session_family`, `session` and `authorization`.
  It MUST additionally store the exact active organization ID and
  `organization` version when an organization is selected, the `factor`
  version when MFA or trusted-device authority is asserted, and the `passkey`
  version when a passkey authenticated or stepped up the session. Optional
  dimensions MUST use an explicit absent discriminator; zero, missing, unknown,
  cross-tenant or dimension/scope-mismatched values are invalid and fail
  closed. Enrichment metadata is a projection of this snapshot, never another
  authority vector.
- Token lookup MUST use a keyed or collision-resistant digest and constant-work
  comparison. Prefixes MAY aid operations but MUST NOT authenticate.
- Rotation, family replay revocation, active-session switch, maximum-session
  enforcement and version increments MUST be atomic under concurrent requests.
  Issue and refresh/rotation MUST persist the authority snapshot selected by
  `LIFECYCLE_CASCADES.md` in the same transaction as the session row and token
  transition. Selecting, switching or clearing an active organization MUST
  compare current membership, organization and authorization authority,
  atomically persist the exact selection/absence, and bump the session version;
  it MUST NOT retain a prior organization's version or treat selection as an
  authorization grant.
- List/revoke-one/revoke-other/revoke-all MUST use stable bounded pagination and
  return outcomes that distinguish already absent from unknown commit without
  revealing another tenant's sessions.
- Expiry cleanup MUST use bounded indexed batches, database time and safe
  ownership; it MUST preserve sessions refreshed concurrently and expose lag.
- Cookie-cache/store refresh races and stateless version invalidation MUST be
  tested against real PostgreSQL transactions and reconnect/commit ambiguity.
- Migration evidence MUST include live rows, old/new binary rotation,
  constraint rollout, version-counter backfill, query plans and restoration of
  a backup containing active and revoked sessions.
- At every lifecycle comparison point, the adapter MUST compare the stored
  snapshot against every applicable current authority: `global`, `tenant`,
  `user`, `identifier`, `credential`, `session_family`, `session`,
  `authorization`, selected `organization`, asserted `factor`, and asserted
  `passkey`, with their exact scope keys. Any missing authority, unsupported
  dimension, version regression/overflow, scope mismatch or stale value MUST
  deny use; refresh MUST NOT silently replace a stale snapshot and thereby
  resurrect the session. Authoritative lifecycle events MAY accelerate local
  invalidation or durable family revocation, but event delivery, local expiry
  and cache freshness MUST NOT substitute for the required online comparisons
  or override a required revocation. These rules and comparison cadence MUST
  match `.ai/identity-platform/LIFECYCLE_CASCADES.md`; revocation audit outcomes
  MUST use `.ai/identity-platform/SECURITY_EVENTS.md`.
- Persistent (`rememberMe`) and non-persistent session records MUST be
  distinguishable without weakening server-side revocation. Non-persistent
  means no persistent browser credential, not an untracked or unversioned
  server session; exact lifetime and renewal defaults belong to
  `.ai/identity-platform/REFERENCE_CONFIGURATION.md`.
- Stored authentication time, method and assurance are evidence inputs only.
  This adapter MUST NOT manufacture a reauthentication proof; it MUST preserve
  the proof ID/version/freshness linkage defined by the session consumer and
  reject stale version updates atomically. A passkey-authenticated or
  passkey-step-up proof MUST preserve the credential ID plus bound `passkey`
  version (and `factor` version when used as MFA); removing that linkage or
  changing authentication-method metadata MUST NOT erase the applicable
  lifecycle dimension.

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
