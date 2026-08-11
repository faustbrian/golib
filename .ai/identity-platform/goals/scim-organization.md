# Goal: pkg/scim/organization

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals, as shown here.

## Execution metadata

- Unit: `scim/organization`
- Canonical module: `pkg/scim/organization`
- Canonical goal after scaffolding: `pkg/scim/organization/.ai/GOAL.md`
- Requires: `scim`, `organization`
- Consumes existing primitives: `identity`, `authorization`, `audit`
- Unlocks after verification: No program unit.

## Start gate

The agent MUST read and satisfy `../COMMON_REQUIREMENTS.md`. It MUST NOT begin
until `../INVENTORY.md` marks `scim/organization` as `ready` and every unit listed in
Requires is `verified`. The agent MUST claim only this unit and record its
owner before any implementation edit.

## Objective and observable completion

Build an independently releasable `pkg/scim/organization` module that owns mapping SCIM Users and Groups onto identity users, organization memberships, teams, and role-safe attributes. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns mapping SCIM Users and Groups onto identity users, organization memberships, teams, and role-safe attributes. It does not own SCIM wire parsing, persistence, SSO, and arbitrary custom-schema engines. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define UserMapper, GroupMapper, AttributePolicy, MembershipPolicy, DeprovisionPolicy, Conflict, Projection, and Reconciler contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST provision and update identities without taking over existing accounts; map groups to teams; forbid role escalation through unmapped attributes; choose suspend/remove/delete deprovision policy explicitly; reconcile drift; handle rename and membership races; keep SCIM and organization IDs traceable. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Security and abuse requirements

- Inputs MUST be bounded before parsing, allocation, storage, hashing, or
  cryptographic work.
- Subject, tenant, organization, purpose, audience, action, and redirect scope
  MUST be bound wherever applicable and MUST fail closed on mismatch.
- Enumeration, replay, fixation, confused-deputy, downgrade, race, and
  cross-scope attacks MUST have deterministic regression cases.
- Logs, traces, metrics, examples, fixtures, and errors MUST preserve the
  redaction requirements in `../COMMON_REQUIREMENTS.md`.

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
