# Goal: pkg/organization

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `organization`
- Canonical module: `pkg/organization`
- Canonical goal after scaffolding: `pkg/organization/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:organization:v1`; owned operation IDs: `contract:operation:identity.organization.active-get:v1`, `contract:operation:identity.organization.active-set:v1`, `contract:operation:identity.organization.archive:v1`, `contract:operation:identity.organization.create:v1`, `contract:operation:identity.organization.delete:v1`, `contract:operation:identity.organization.get:v1`, `contract:operation:identity.organization.invitation-send:v1`, `contract:operation:identity.organization.invitation.accept:v1`, `contract:operation:identity.organization.invitation.cancel:v1`, `contract:operation:identity.organization.invitation.expire:v1`, `contract:operation:identity.organization.invitation.get:v1`, `contract:operation:identity.organization.invitation.list:v1`, `contract:operation:identity.organization.invitation.list-mine:v1`, `contract:operation:identity.organization.invitation.reject:v1`, `contract:operation:identity.organization.invitation.resend:v1`, `contract:operation:identity.organization.list:v1`, `contract:operation:identity.organization.member.active-get:v1`, `contract:operation:identity.organization.member.active-role-get:v1`, `contract:operation:identity.organization.member.add:v1`, `contract:operation:identity.organization.member.leave:v1`, `contract:operation:identity.organization.member.list:v1`, `contract:operation:identity.organization.member.remove:v1`, `contract:operation:identity.organization.member.update:v1`, `contract:operation:identity.organization.owner-transfer:v1`, `contract:operation:identity.organization.permission-check:v1`, `contract:operation:identity.organization.restore:v1`, `contract:operation:identity.organization.role.create:v1`, `contract:operation:identity.organization.role.delete:v1`, `contract:operation:identity.organization.role.get:v1`, `contract:operation:identity.organization.role.list:v1`, `contract:operation:identity.organization.role.update:v1`, `contract:operation:identity.organization.slug-available:v1`, `contract:operation:identity.organization.team.active-set:v1`, `contract:operation:identity.organization.team.create:v1`, `contract:operation:identity.organization.team.delete:v1`, `contract:operation:identity.organization.team.list:v1`, `contract:operation:identity.organization.team.list-mine:v1`, `contract:operation:identity.organization.team.member-add:v1`, `contract:operation:identity.organization.team.member-list:v1`, `contract:operation:identity.organization.team.member-remove:v1`, `contract:operation:identity.organization.team.update:v1`, `contract:operation:identity.organization.update:v1`
- Requires: `identity`, `identity/session`, `identity/delivery`, `primitive/authorization-identity-contracts`, `primitive/capability-identity-contracts`, `primitive/capability-postgres-identity-contracts`
- Consumes existing primitives: `tenancy`, `authorization`, `identifier`, `audit`, `capability`, `capability/postgres`
- Unlocks after verification: `identity/apikey`, `organization/postgres`, `sso`, `sso/domain-verification`, `scim`, `scim/organization`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `organization` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/organization` module that owns organizations, memberships, invitations, teams, role assignments, ownership transfer, domains, and lifecycle policy. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns organizations, memberships, invitations, teams, role assignments, ownership transfer, domains, and lifecycle policy. It does not own enterprise SSO protocol, SCIM wire behavior, billing, UI, and persistence adapters. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define Organization, Member, Invitation, InvitationDelivery,
Team, RoleBinding, DomainClaim, DomainProof, DomainProofReader,
DomainEvidenceTransition, Repository, UnitOfWork, Policy, Hook, and Event
contracts and consume `identity/session`'s active-organization selection
contract rather than persisting user-global selection state. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.
`organization` is the sole domain-proof authority. `DomainProofReader` MUST
expose exactly `GetDomainProof(context.Context, DomainProofKey) (DomainProof, error)`;
`DomainEvidenceTransition` MUST expose exactly
`ApplyDomainEvidence(context.Context, DomainEvidenceCommand) (DomainProof, error)`.
The key and command MUST bind tenant, organization, canonical domain, claim ID,
claim version, proof version, method, observed-at time, evidence expiry, and
revocation state. No SSO or verifier package may define or persist a competing
proof type or authority version.

## Required behavior

The implementation and tests MUST create and archive organizations; invite and accept exactly once; add/remove members and teams; prevent last-owner removal; transfer ownership atomically; scope roles and resources; verify domain claims through pluggable proof; handle identity deletion and invitation races. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Organization operations MUST include create with policy hook, check slug
  availability without reservation guarantees, list for user, get full bounded
  view, update, and the separate `identity.organization.archive`,
  `identity.organization.restore` and `identity.organization.delete`
  transitions; archive or delete MUST NOT alias the other. They MUST also set/
  get active organization and enforce configured per-user/per-tenant limits.
- Active-organization selection MUST be owned by an exact authenticated session,
  not by the user globally. Simultaneous sessions MUST select and clear
  independently; every use MUST revalidate the session, tenant, membership and
  organization lifecycle, and membership removal, archive or session revocation
  MUST invalidate the affected selection without permitting stale authority.
- Invitation operations MUST include send/resend with deduplication, get, list
  by organization, list by recipient, accept, reject, cancel and expire.
  Acceptance MUST bind the intended verified recipient and organization and
  handle existing membership and role changes explicitly.
- `identity.organization.invitation.resend` MUST supersede the predecessor
  proof and `identity.organization.invitation.expire` MUST terminally revoke
  every outstanding proof, with exactly the callable-operation semantics and
  audit events declared in `API_OPERATIONS.md`.
- Invitation issuance and the delivery intent MUST have one idempotency key and
  an explicit transaction/outbox boundary. Results MUST distinguish not
  queued, queued, delivered, failed and unknown without rolling back a durable
  invitation merely because external delivery is ambiguous.
- Invitation acceptance MUST use the shared `capability` contract with a
  purpose-bound, recipient- and organization-bound, expiring, revocable and
  atomically single-use capability. Resend MUST define whether it reuses or
  supersedes the previous invitation capability; neither path may leave two
  independently consumable grants, and bearer material MUST NOT be stored or
  exposed through list/get/audit surfaces.
- Member operations MUST include list/search, add by authorized administrator,
  remove, leave, update role, get active member/role and transfer ownership.
  The last owner and last recovery administrator invariants MUST survive races.
- Static and dynamic access control MUST support roles, permission statements,
  custom permissions and organization-scoped role CRUD with maximum-role
  limits. Role deletion/update MUST define effects on existing bindings and
  MUST NOT broaden permission through unknown statements.
- Role evaluation MUST define deterministic precedence for static roles,
  organization custom roles, team-derived statements and explicit deny policy.
  Authorization inputs MUST carry an organization policy version; role,
  binding, team-membership and ownership changes MUST advance it and invalidate
  cached or session-derived decisions so stale allows fail closed.
- Team operations MUST include create, list, get/update/remove, active team,
  list user teams, list/add/remove members and team permission checks. Team
  membership MUST require compatible organization membership.
- Organization, member, invitation, role and team additional fields MUST use
  typed schemas with input/output/write/sensitivity policy rather than
  unbounded metadata maps.
- Hooks MUST cover organization, member, invitation, role and team before/after
  transitions with the common ordering, transaction and failure semantics.
- Domain claims MUST distinguish requested, challenge-issued, verified,
  expired, revoked and conflict states and MUST NOT route SSO until proof is
  current and uniquely owned.
- Archive and delete MUST define a deterministic cascade for active selections,
  invitations, memberships, teams, role bindings, domain claims and owned
  integration links. Archive MUST deny new authority while preserving declared
  recovery/audit access; hard deletion MUST respect audit, legal-hold, outbox
  and externally owned SSO/SCIM cleanup boundaries from
  `.ai/identity-platform/COMMON_REQUIREMENTS.md`. The composed lifecycle MUST
  satisfy journeys 9--11 in `.ai/identity-platform/END_STATE.md` and the
  organization/SSO rules in `.ai/identity-platform/REFERENCE_PROFILE.md`.

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
