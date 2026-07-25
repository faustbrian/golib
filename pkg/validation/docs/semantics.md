# Normative semantics

## Presence

The following table is normative. “Pass” means the rule emits no violation.

| State | Required | Present | Omitted | Prohibited | Empty | ZeroValue |
| --- | --- | --- | --- | --- | --- | --- |
| Missing | fail | fail | pass | pass | fail | fail |
| Null | fail | fail | fail | fail | fail | fail |
| Present empty | fail | pass | fail | fail | pass | pass when Go-zero |
| Present zero non-empty | type-dependent | pass | fail | fail | type-dependent | pass |
| Present non-zero | pass | pass | fail | fail | fail | fail |

No rule coerces, defaults, trims, normalizes, or replaces a value.

## Composition

| Expression | Pass condition | Evaluation |
| --- | --- | --- |
| `All` | every child passes | declaration order |
| `Any` | at least one child passes | failed alternatives discarded after success; successful warnings retained |
| `Not` | child has a blocking error | child details discarded |
| `When` | selected branch passes | exactly one branch |
| `Dependent` | prerequisite and then dependent pass | dependent skipped after failure; prerequisite warnings retained |

`ShortCircuit` stops at the first decisive result. `CollectAll` evaluates every
relevant child. Warning-only reports are non-blocking.

## Paths

Field segments render as `profile.name`, indexes as `items[2]`, map keys as
`labels[key]`, and generic items as `[]`. JSON pointers use RFC 6901 escaping:
`a/b~c` becomes `/a~1b~0c`. Segments retain their kind. Reports replace a path
longer than `MaxPathLength` with a root `path_limit` violation.

## Aggregation

Reports preserve declaration/traversal order. Deduplication identity is typed
path segments (kind, length, and value), code, severity, and sorted safe
parameters; rendered-path collisions cannot collapse findings. Causes do not
affect identity.
Once `MaxViolations` is reached, additional findings are omitted and
`Truncated` becomes true. `HasErrors` and `Err` remain blocking if any omitted
finding was an error. Merge retains source order, truncation, and blocking
state.
