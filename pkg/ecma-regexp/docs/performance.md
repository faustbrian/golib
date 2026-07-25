# Performance and benchmarks

Parse and compile patterns once, then reuse the immutable `Program`. Avoid a
shared unbounded cache; use an application-owned cache with explicit size and
eviction limits when pattern reuse warrants it.

Execution cost depends on the pattern, input, and chosen budgets. Nested
quantifiers, ambiguous alternatives, backreferences, and lookaround can cause
large backtracking search spaces. The engine terminates those searches at the
configured step, backtrack, stack, allocation, wall-time, or context boundary.

Repository benchmarks must pair timing with correctness checks and include:

- parse and compile cost separately;
- anchored and unanchored matching;
- ASCII, BMP, and astral Unicode inputs;
- successful, failing, and adversarial matches;
- comparable semantics when another engine is included.

Benchmark numbers are environment-dependent and are not published until the
release benchmark gate and pinned corpus are complete.
