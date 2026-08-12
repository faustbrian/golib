# Goal: pkg/identity/impersonation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/impersonation`
- Canonical module: `pkg/identity/impersonation`
- Canonical goal after scaffolding: `pkg/identity/impersonation/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/impersonation:v1`; owned operation IDs: `contract:operation:identity.impersonation.approve:v1`, `contract:operation:identity.impersonation.deny:v1`, `contract:operation:identity.impersonation.quorum:v1`, `contract:operation:identity.impersonation.request:v1`, `contract:operation:identity.impersonation.revoke:v1`, `contract:operation:identity.impersonation.start:v1`, `contract:operation:identity.impersonation.stop:v1`
- Requires: `identity`, `identity/session`, `identity/risk`, `primitive/authorization-identity-contracts`
- Consumes existing primitives: `authorization`, `audit`, `capability`
- Unlocks after verification: `identity/impersonation/postgres`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/impersonation` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/impersonation` module that owns privileged, time-bounded impersonation grants and sessions with reason, target, actor, policy, revocation, and complete audit lineage. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns privileged, time-bounded impersonation grants and sessions with reason, target, actor, policy, revocation, and complete audit lineage. It does not own administrative UI, general authorization policy, account suspension, and ordinary session management. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define Grant, Actor, Target, Reason, Policy, Approval, Status,
SessionIssuer, Lineage, Revoker, Observer, and presentation-marker contracts.
`Status` MUST be the closed enum `StatusUnspecified`, `StatusRequested`,
`StatusPendingApproval`, `StatusApproved`, `StatusActive`, `StatusStopped`,
`StatusExpired`, `StatusDenied`, and `StatusRevoked`; unspecified and unknown
values MUST fail closed. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST require explicit authorization and recent authentication; prohibit recursive impersonation; bind actor and target; preserve both identities in principals and audit; cap duration and scope; display machine-readable impersonation state; revoke immediately; prevent target privilege from authorizing actor-only operations. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- The public contract MUST expose distinct `identity.impersonation.request`,
  `identity.impersonation.approve`, `identity.impersonation.deny`,
  `identity.impersonation.quorum` and `identity.impersonation.revoke`
  operations with exactly the exposure, access, CSRF, rate, idempotency and
  result semantics in `API_OPERATIONS.md`; start MUST NOT substitute for any
  approval-lifecycle transition.
- Start MUST require a named authorization decision, recent non-impersonated
  authentication, target eligibility, non-empty bounded reason and optional
  approval/reference according to policy.
- Actor authority MUST be evaluated from the actor's non-impersonated principal
  against the requested target and scope at request, approval and activation
  boundaries. Target-effective permissions MUST NOT satisfy actor-only
  authority, and policy MUST define revalidation or revocation when actor
  authority, freshness, role, tenant or organization membership changes.
- Approval policy MUST declare when approval is required, eligible approvers,
  separation-of-duties/self-approval rules, quorum, expiry and scope binding.
  An approval MUST bind the exact actor, target, tenant, requested scope,
  reason digest and policy version and MUST NOT be reusable after any of those
  inputs change.
- Ban/suspension, organization boundary, protected-account and actor-role rules
  MUST be evaluated explicitly. Super-admin or service accounts MUST NOT be
  impersonable merely because they exist.
- The issued session MUST contain actor, target, grant ID, start/expiry, scope
  and immutable lineage and MUST be distinguishable by every downstream audit
  and presentation surface.
- Nested/recursive impersonation and privilege use outside the granted scope
  MUST fail. Authorization MUST be able to evaluate both actor authority and
  target-effective permissions without confusing them.
- Stop, expiry, actor disable, target disable, grant revoke and global session
  revoke MUST terminate access with documented propagation. Stop MUST restore
  only the original still-valid actor session, never mint a stronger one.
- Grants MUST store and return the exact closed `Status` value and have one
  explicit state machine covering requested, pending-approval, approved,
  active, stopped, expired, denied and revoked states. Allowed actors, guards,
  idempotency and single-winner behavior MUST
  be defined for every transition; denied, expired, stopped or revoked grants
  MUST never return to an authority-bearing state.
- Impersonation sessions MUST NOT use the stateless-session compatibility
  profile or make a self-contained token the authority. Every use MUST perform
  an authoritative online grant/version/revocation check; inability to perform
  that check MUST fail closed. These requirements refine the administration and
  session journeys in `.ai/identity-platform/END_STATE.md` and the impersonation
  profile in `.ai/identity-platform/REFERENCE_PROFILE.md`.
- List/search of grants and sessions MUST be administrator-authorized,
  tenant-scoped, bounded and auditable. Reasons are sensitive and MUST NOT be
  exposed to the target unless policy explicitly says so.

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
