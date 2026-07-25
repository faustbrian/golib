# Decision and precedence tables

## Composition

| Inputs encountered | Deny overrides | Allow overrides | First applicable | Priority order |
| --- | --- | --- | --- | --- |
| no applicable outcome | `NotApplicable` | `NotApplicable` | `NotApplicable` | `NotApplicable` |
| allow only | allow | allow | first allow | highest-priority allow |
| deny only | deny | deny | first deny | highest-priority deny |
| allow and deny | deny | allow | first applicable | highest-priority applicable |
| invalid outcome or evaluator error | error, root deny | error, root deny | error, root deny | error, root deny |

The root engine converts a final `NotApplicable` to `Deny` with
`default-deny`. Priority ties are ordered by stable policy ID. Deny-overrides
and allow-overrides short-circuit only when their decisive outcome is observed.

## ACL

| Matching entries | Model outcome | Reason |
| --- | --- | --- |
| none | `NotApplicable` | empty |
| one or more allows, no deny | allow | `acl-allow` |
| any deny | deny | `acl-explicit-deny` |
| group, match, or work bound exceeded | deny plus error | `acl-limit-exceeded` |

An entry must match principal kind and ID, action, resource type, optional
resource ID, and tenant scope. Global entries do not apply to tenant requests
unless global inheritance is enabled. Duplicate principals are evaluated once.

## RBAC

| Effective matching permissions | Model outcome | Reason |
| --- | --- | --- |
| none | `NotApplicable` | empty |
| one or more allows, no deny | allow | `rbac-allow` |
| any deny, at any priority | deny | `rbac-explicit-deny` |
| graph, group, match, or work bound exceeded | deny plus error | `rbac-limit-exceeded` |

Permissions are ordered by descending priority and stable ID for deterministic
evaluation and explanation. Priority never weakens explicit deny. Assignments,
roles, parents, and permissions must remain in one tenant unless explicit
global inheritance is enabled. Cycles, missing parents, and cross-tenant edges
are rejected before activation.

## ABAC

| Matching rules | Model outcome | Reason |
| --- | --- | --- |
| none | `NotApplicable` | empty |
| one or more allows, no deny | allow | `abac-allow` |
| any deny, at any priority | deny | `abac-explicit-deny` |
| missing, null, or type mismatch in a condition | condition does not match | empty unless another rule applies |
| cost, depth, set, match, or work bound exceeded | deny plus error | `abac-limit-exceeded` |

Rules are ordered by descending priority and stable ID for deterministic
evaluation and explanation. Priority never weakens explicit deny. Conditions
use exact types without coercion. Missing, null, no-match, and type-mismatch
statuses are distinct for direct condition evaluation, while all are
non-matches for a rule.

Every table is exercised by model-specific tests, the shared model conformance
suite, and the exhaustive composition truth table.
