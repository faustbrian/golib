# Set-operation semantics

All operations return new values. Caller slices and returned slices are never
retained as mutable aliases.

- Intersection includes an equal boundary only when both operands include it.
- Union merges an equal boundary when either operand represents it.
- Difference inverts intersection boundaries; surviving singleton endpoints
  are retained.
- Gap includes an adjacent boundary exactly when neither neighboring interval
  includes it.
- Normalization removes empty values, sorts stably, and merges only set-safe
  overlap or adjacency.
- Daily complement is `FullDay - set`, not an implicit ambient universe.

Set output is normalized and disjoint. Input and output cardinality are checked
against `Limits` before unbounded allocation. Intersection is linear after
normalization; repeated subtraction is bounded by the configured output limit.
