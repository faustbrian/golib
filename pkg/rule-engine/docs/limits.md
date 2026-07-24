# Limits

`Limits` independently bounds rule count, fact count, AST depth, operands,
collection size, string bytes, JSON definition bytes, regex bytes, identifier
bytes, tags, tag bytes, path bytes, path segments, chaining iterations,
derived facts, diagnostics, explanation entries, and evaluation duration.

Start with `DefaultLimits` and reduce budgets for the owning service. Every
field must be positive. Limits are captured in a compiler and copied into its
plans. They do not mutate globally.

Definition byte limits apply before JSON decoding. Path and value limits apply
during construction. AST and operator limits apply during compilation.
Iteration, output, diagnostics, and time limits apply during evaluation.
