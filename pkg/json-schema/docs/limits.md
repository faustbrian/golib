# Resource limits

`DefaultLimits` returns conservative standalone defaults. Copy it, change the
fields needed for the deployment, validate it through `WithLimits`, and load
test realistic worst cases before reducing a value.

| Field | Bounds |
| --- | --- |
| `MaxInputBytes` | Each schema, instance, value encoding, or loaded document |
| `MaxNestingDepth` | JSON parser recursion |
| `MaxTotalValues` | Values in one parsed document |
| `MaxObjectMembers` | Members in one object |
| `MaxArrayItems` | Items in one array |
| `MaxNumberBytes` | Exact number text, including exponent |
| `MaxSchemaResources` | Root plus loaded schema resources |
| `MaxTotalSchemaBytes` | Aggregate schema resource bytes |
| `MaxSchemaNodes` | Compiled schema and subschema plans |
| `MaxCombinatorBranches` | Schema-array fan-out |
| `MaxRegexCount` / `MaxRegexBytes` | Schema patterns and asserted regex format values |
| `MaxRegexBacktracking` | Backtracking stack slots per match |
| `MaxRegexMatchMilliseconds` | Approximate wall-clock duration per match |
| `MaxReferenceDepth` | Nested reference evaluation |
| `MaxDynamicScopeDepth` | Recursive/dynamic resource scope |
| `MaxEvaluationOps` | Total deterministic evaluation work |
| `MaxUniqueComparisons` | Pairwise or hash/collision work for `uniqueItems` |
| `MaxFormatChecks` | Built-in and custom format checks |
| `MaxCustomKeywordCompiles` | Custom keyword compilation calls |
| `MaxCustomKeywordCalls` | Custom keyword evaluation calls |
| `MaxAnnotationBytes` | One custom annotation |
| `MaxOutputUnits` | Generated diagnostic and annotation units |

All values must be positive. Exhaustion returns `*LimitError`, classifiable as
`ErrLimitExceeded`, and identifies the resource. Cancellation is checked
through parsing, compilation callbacks, loaders, evaluation, output, and
extension callbacks. Limits bound owned work; application callbacks must also
honor their context and avoid hidden goroutines or unbounded external work.
