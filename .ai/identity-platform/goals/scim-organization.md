# Goal: pkg/scim/organization

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `scim/organization`
- Canonical module: `pkg/scim/organization`
- Canonical goal after scaffolding: `pkg/scim/organization/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:scim/organization:v1`; owned operation IDs: none
- Requires: `scim`, `identity`, `organization`, `sso`
- Consumes existing primitives: `authorization`, `audit`
- Unlocks after verification: `scim/postgres`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `scim/organization` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/scim/organization` module that owns mapping SCIM Users and Groups onto identity users, organization memberships, teams, and role-safe attributes. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns mapping SCIM Users and Groups onto identity users, organization memberships, teams, and role-safe attributes. It does not own SCIM wire parsing, persistence, SSO orchestration, and arbitrary custom-schema engines. It implements only the SSO-owned directory-delta contributor boundary. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define UserMapper, GroupMapper, AttributePolicy, MembershipPolicy, DeprovisionPolicy, Conflict, Projection, and Reconciler contracts and implement the consumer-owned `sso.DirectorySyncContributor` exactly. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST provision and update identities without taking over existing accounts; map groups to teams; forbid role escalation through unmapped attributes; choose suspend/remove/delete deprovision policy explicitly; reconcile drift; handle rename and membership races; keep SCIM and organization IDs traceable. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Connection ownership MUST bind tenant, organization and external provider;
  list/get/update/delete, token rotation/revocation and reconciliation MUST
  require their exact organization administration permissions and MUST NOT
  expose another provider's mappings or secrets. Mapping updates and
  reconciliation MUST preserve versioned source-of-truth and deprovision
  policy; token revocation MUST never expose bearer material.
- User mapping MUST define externalId, userName, emails, active, names and
  enterprise/custom attributes; matching order and collision behavior MUST be
  deterministic and MUST NOT take over a pre-existing account without policy.
  External-ID lookup MUST preserve the exact `(tenant, organization, provider
  connection, resource type)` partition and schema `caseExact` semantics
  supplied by `scim`, while retaining RFC `uniqueness: none`. Multiple matches
  MUST produce explicit mapping policy/conflict handling; this package MUST NOT
  independently normalize, widen, or treat externalId as account authority.
- Group mapping MUST define organization membership, teams and optional role
  bindings. Unmapped/unknown group display names MUST NOT become roles or
  permissions automatically.
- Create/replace/PATCH/deactivate/delete MUST map to explicit identity and
  membership state transitions with suspension/removal/delete policy and
  preserved recovery administrators.
- Attribute mapping configuration MUST be typed, validated, versioned and
  migration-safe. Sensitive or write-only identity fields MUST never be
  exposed through SCIM.
- The mapper contract MUST contain no readable or persistable password field.
  The reference protocol profile MUST reject `password` at the `scim`
  write-only credential decision seam before mapper invocation. A future
  approved profile may route the write only to the public `identity/password`
  contract; this package MUST never compare, log, retain or project the value
  or implement credential storage.
- Reconciliation MUST compare external projections, classify drift/conflict,
  resume from stable cursors, be idempotent and avoid overwriting concurrent
  local changes without an explicit source-of-truth policy.
- Bulk mapping MUST consume the parent/child checkpoint contract from
  `.ai/identity-platform/TRANSACTION_CONTRACT.md`: each admitted child maps as
  its own idempotent command, committed results survive later child failures,
  and resume/reconciliation MUST use the durable child status rather than
  replaying already committed identity or organization transitions.
  Core `scim`, not this mapping collaborator, owns Bulk admission, apply-child,
  and skip-child protocol audit actions. This mapper MUST contribute its mapped
  domain outcome to the core child result without emitting a competing Bulk
  protocol event.
- Every mutating child MUST enlist the public identity and organization
  contributors before the coordinator's first write and reserve, apply, commit
  or recover under one stable child command. This mapper owns mapping policy
  and orchestration, while identity and organization remain sole durable
  authorities; partial success MUST retain exact checkpoints.
- Directory-sync generations, provider cursors, status, cancellation,
  reconciliation and events belong to `sso`. This package MUST translate each
  admitted version-bound delta batch into public identity and organization
  mapping commands, preserve its generation, mapping version, stable child
  command IDs and predecessor checkpoint, and return exact committed, denied,
  or unknown per-child outcomes. It MUST NOT create a second sync
  authority, cursor, scheduler or event stream.
- The mapper MUST use only public `identity` and `organization` contracts for
  authoritative users, identifiers, memberships, teams and roles. It MUST NOT
  define substitute user, membership, team or role repositories inside SCIM.

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

Verification applicability is exact for this unit: `race=required`,
`fuzz=required`, `hostile=required`, `leak=required`, `benchmark=required`,
`infrastructure=required`, and `provider_interoperability=required`; a gate
MAY be satisfied by the required composed reference evidence but MUST NOT be
silently skipped.

## Release blockers

The unit MUST remain `implemented-unverified` or `blocked` if any prerequisite
is not `verified`, any ownership boundary is unresolved, a protocol claim
lacks pinned specification and interoperability evidence, a durable transition
has unhandled ambiguity, a secret can escape redaction, or any required gate is
stale, skipped, warning-only, or failing.
