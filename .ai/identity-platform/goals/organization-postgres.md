# Goal: pkg/organization/postgres

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `organization/postgres`
- Canonical module: `pkg/organization/postgres`
- Canonical goal after scaffolding: `pkg/organization/postgres/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:organization/postgres:v1`; owned operation IDs: none
- Requires: `organization`, `identity/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `outbox`, `audit`
- Unlocks after verification: `identity/apikey/postgres`, `sso/postgres`, `scim/postgres`, `identity/reference`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `organization/postgres` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/organization/postgres` module that owns durable organization repositories, relational invariants, transactions, indexes, migrations, and outbox events. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns durable organization repositories, relational invariants, transactions, indexes, migrations, and outbox events. It does not own organization business policy, SSO protocols, and SCIM protocol. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define schema, membership and invitation constraints, ownership locking, role/team queries, domain claims, transaction, cleanup, reconciliation, and migration contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.
The repository MUST implement `organization.DomainProofReader` and
`organization.DomainEvidenceTransition` with the exact core signatures. Its
`ApplyDomainEvidence` and `RecoverDomainEvidence` implementations MUST return
`organization.DomainEvidenceResult`, preserve the exact command identity and
fingerprint, and retrieve committed proof state from primary authority without
repeating external DNS or HTTPS observation.

The adapter MUST publish an open, versioned `identitypostgres.Contributor` for
registration by the composition root. The contributor maps
`organization.Command` to this adapter's registered participant input and
stages it through `identitypostgres.Work`; the core `organization.UnitOfWork`
and `organization.DomainEvidenceTransition` remain free of those concrete
types. The repository constructor MUST NOT accept or retain a coordinator,
transaction, work handle, carrier, or contributor registry. Only the
composition root registers the contributor and supplies the resulting
storage-neutral `organization.UnitOfWork` implementation to the core service.

## Required behavior

The implementation and tests MUST enforce membership and invitation uniqueness; serialize last-owner transitions; accept invitations atomically; prevent cross-organization joins; paginate deterministically; recover ambiguous commits; upgrade with mixed binaries and realistic query plans. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- The schema MUST persist organizations, memberships, invitations,
  static/dynamic roles, permission statements,
  teams, team membership, typed additional fields, domain claims and aggregate
  versions with tenant-scoped foreign keys.
- This module MUST NOT persist or expose an active-organization selection.
  `identity/session/postgres` is the sole durable owner of the selection keyed
  by tenant and stable session ID, and Valkey is only a positive projection.
  The active-organization switch command MUST coordinate
  `identity/session/postgres` and this module through the one
  `identity/postgres` `Coordinator`: lock the session and organization/member
  rows in the global order, prove that the session subject has usable
  membership in an active organization, update the session-owned selection,
  bind its organization/policy versions, and publish invalidation in one
  commit. A user-level singleton, organization-owned duplicate column, or cache
  write as authority is forbidden.
- Constraints/locking MUST enforce unique slugs in declared scope, unique
  membership, bounded effects on session-owned active selections, invitation identity, last-owner safety,
  role/team limits and organization-compatible team membership under races.
- Invitation accept/reject/cancel/expire/resend and ownership transfer MUST be
  atomic with events/outbox and idempotent by stable command identity. Resend
  MUST atomically supersede the prior proof; expiry MUST atomically make every
  outstanding proof unusable.
- Dynamic-role update/delete MUST lock affected bindings and produce a
  deterministic permission result for concurrent authorization checks.
- Static/custom role, binding, team-membership and ownership mutations MUST
  atomically advance the persisted organization policy version and publish the
  invalidation needed by authorization/session consumers. Unknown invalidation
  outcomes MUST reconcile before a stale cached allow can be trusted.
- Stable cursor plans are REQUIRED for organizations, members, invitations,
  roles and teams. Search/filter fields and case/collation semantics MUST be
  explicit and indexed at production-shaped cardinality.
- Domain-claim challenge, verification, expiry, uniqueness and takeover
  prevention MUST be database-enforced where possible and reconciled after
  unknown external proof outcomes. Applying domain evidence MUST atomically
  validate the expected claim/proof versions and stable command fingerprint;
  recovery MUST classify the same command as not committed, committed, or
  unknown and return the identical committed proof/version when committed.
- Invitation rows MUST persist only a digest/reference for the capability-owned
  bearer value and MUST atomically couple acceptance, single-use consumption,
  membership/role effects and outbox state. Resend/supersession races MUST
  leave at most one consumable invitation grant.
- Separate archive, restore and delete commands MUST have database-enforced,
  resumable disposition for pending invitations, memberships, teams, role bindings, domain
  claims and integration references. Cleanup MUST follow the audit/legal-hold/
  outbox boundaries in `.ai/identity-platform/COMMON_REQUIREMENTS.md`; tests
  MUST prove no orphan can retain organization authority after partial or
  ambiguous cascade execution. Archive/delete and membership removal MUST
  advance organization authority first and enlist the session owner for
  bounded active-selection cleanup; an unknown cleanup outcome cannot preserve
  access because every cached or durable selection remains bound to the stale
  organization version.
- Migration evidence MUST include existing organizations, legacy/static roles,
  team enablement, added typed fields, mixed binaries, interrupted backfills,
  backup/restore and query-plan budgets.

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
