# Goal: primitive/authorization-identity-contracts

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `primitive/authorization-identity-contracts`
- Canonical module: `pkg/authorization`
- Canonical goal after scheduling: `pkg/authorization/.ai/GOAL_IDENTITY_CONTRACTS.md`
- Public contracts: externally owned operation IDs: `contract:operation:identity.admin.permission-check:v1`, `contract:operation:identity.admin.user-role-set:v1`
- Requires: None
- Exact identity-platform consumers: `identity`, `identity/apikey`, `identity/http`, `identity/impersonation`, `identity/reference`, `identity/session`, `organization`
- Unlocks after verification: `identity`, `identity/apikey`, `identity/http`, `identity/impersonation`, `identity/reference`, `identity/session`, `organization`

## Start gate and pinned contract

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and MUST
start only after the coordinator marks this unit `in-progress`. The current API
MUST match
`sha256:1e414b74593a28a414de2dd1bf7cff3bd5d37e06fb11846bfb0c65dd28c1b666`.
The completed required contract MUST reproduce
`sha256:2b6ad43b43cbd75c8f6bb5c81d6290bdb87bcbc2a70e8152165f7802dfea004d`
under the canonical digest algorithm in
`.ai/identity-platform/fragments/public_contracts_meta.json`. A current-API mismatch MUST block the
goal pending reconciliation and MUST NOT be accepted by changing the pin.

## Objective and exact additions

Extend the existing authorization primitive with the exact typed policy and
assignment vocabulary consumed by the identity platform. The module MUST add:

- `AssignmentDisposition`: `enum{AssignmentDispositionUnspecified,AssignmentDispositionReject,AssignmentDispositionRemove,AssignmentDispositionReassign}`.
- `AssignmentImpact`: `struct{AssignmentCount uint32; SubjectCount uint32; Truncated bool}`.
- `DecisionContext`: `struct{Request Request; CorrelationID string; PolicyRevision Revision}`.
- `PermissionID`: defined string containing 1..128 canonical ASCII characters.
- `PermissionStatement`: `struct{PermissionID PermissionID; ResourceType ResourceType; ResourceID ResourceID; Action Action; Conditions []Condition}`.
- `RoleDisposition`: `enum{RoleDispositionUnspecified,RoleDispositionReject,RoleDispositionDetach,RoleDispositionReplace}`.
- `StatementID`: defined string containing exactly 22 canonical base64url characters.
- `SubjectProjection`: `struct{Subject Subject; Roles []string; StatementIDs []StatementID; Revision Revision}`.
- `VersionedPermissionStatement`: `struct{StatementID StatementID; Statement PermissionStatement; Version uint64}`.
- `Service`: interface with exactly `Decide(context.Context, DecisionContext) (Decision, error)`.

The supporting required contract MUST retain the exact pinned declarations for
`Condition`, `ConditionOperator`, `Request`, and `Decision`. It MUST expose
`NewPermissionID`, `NewStatementID`, `PermissionStatement.Validate`, and
`Request.Validate` with the exact pinned signatures. It MUST expose the exact
required error set: `ErrAssignmentLimit`, `ErrBatchLimitExceeded`,
`ErrInvalidPermissionStatement`, `ErrInvalidOutcome`, `ErrInvalidRequest`,
`ErrPolicyLimitExceeded`, `ErrPolicyPanic`, `ErrRevisionConflict`, and
`ErrStatementNotFound`. It MUST NOT add a generic policy payload, untyped
attribute map, hidden service locator, or identity-owned parallel authorizer.

## Behavioral and security requirements

- Unspecified assignment and role dispositions MUST be invalid.
- Assignment removal MUST cover every affected assignment; reassignment MUST
  require one explicit replacement role and MUST cap impact at 10,000 assignments.
- `AssignmentImpact` counts MUST be bounded to 10,000, and `Truncated` MUST be
  forbidden for an authorized mutation decision.
- `PermissionStatement` MUST require permission ID, resource type, and action;
  conditions MUST be canonical, duplicate-free, and limited to 64 entries.
- `PermissionID` and `StatementID` MUST be exact stable identities and MUST NOT
  grant authority through prefixes, wildcards, or numeric conversion.
- `SubjectProjection` roles and statement IDs MUST be bytewise sorted,
  duplicate-free, defensively copied, and limited to 128 and 256 entries.
- `VersionedPermissionStatement` MUST require a positive version and matching
  immutable statement identity.
- `DecisionContext` MUST carry a valid complete request and positive immutable
  policy revision; correlation IDs MUST be at most 128 UTF-8 bytes.
- `Service.Decide` MUST fail closed for errors, stale revisions,
  `NotApplicable`, and invalid outcomes.
- Validation MUST NOT perform implicit string conversion between incompatible
  condition value kinds.
- Public errors MUST be stable, typed, bounded, and redacted.
- Public documentation MUST state zero values, limits, ordering, revision,
  ownership, concurrency, and fail-closed semantics.

## Compatibility, migration, and evidence

The extension MUST be additive to the pinned current API. Existing `Authorizer`,
request, decision, condition, and value behavior MUST remain source- and
behavior-compatible. Consumers MUST migrate from duplicated assignment,
projection, statement, and decision-context types to the canonical additions;
persisted policy or statement data MUST use an explicit versioned migration
with rollback and mixed-version compatibility evidence.

Focused tests MUST prove enum closure, limits, canonical ordering, defensive
copying, revision conflicts, validation, fail-closed decisions, and concurrent
read safety. Applicable fuzz tests MUST exercise hostile conditions and
statements. The worker MUST run exact coverage, mutation, race, fuzz,
API-baseline, clean-consumer, documentation, example, inventory, supply-chain,
and changed reverse-dependant gates. `pkg/authorization/CHANGELOG.md` MUST
describe the additive contract and migration impact. Every new public symbol
MUST have useful Go documentation. The unit MUST remain unverified until the
required digest and every applicable gate pass; skipped, stale, warning-only,
or missing evidence MUST NOT count as a pass.

## Scope boundary

The worker MUST NOT redesign the policy engine, role model, identity packages,
persistence adapters, or administrative flows. Changes MUST be limited to the
exact extension, its tests, API baseline, documentation, examples, changelog,
and mechanically required manifests.
