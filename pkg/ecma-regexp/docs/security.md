# Limits and security

ECMAScript backreferences and lookaround require a backtracking engine and do
not have RE2's linear-time guarantee. Treat untrusted patterns and inputs as
potentially adversarial.

`ParseOptions` bounds pattern bytes, AST depth and nodes, captures, and
character classes. `CompileOptions` bounds program instructions.
`MatchOptions` bounds input bytes and runes, VM steps, backtracks, stack and
recursion depth, logical allocations, results, replacement/split output, and
wall time. Zero means no allowance, not unlimited.

Use a request-scoped context in addition to finite budgets. Cancellation,
wall-time exhaustion, and resource exhaustion are reported separately as
`context` errors, `TimeoutError`, and `LimitError`. Do not convert these errors
to a normal non-match without an explicit application policy.

Execution is synchronous. The package creates no timeout goroutine, uses no
`unsafe`, and maintains no global mutable cache. Applications may add a
caller-owned bounded cache of immutable `Program` values.

`make hostile` exercises catastrophic backtracking, zero-width loops, capture
and replacement growth, nested assertions, Unicode sets, and malformed UTF-8.
`make leak` repeats budget failures under goroutine and retained-heap checks.
`make race` shares immutable programs through a caller-owned synchronized cache.

Default limits are safe starting points, not universal policy. Lower them for
interactive or multi-tenant validation and measure real workloads before
raising them.
