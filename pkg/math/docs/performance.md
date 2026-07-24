# Performance

Exact arithmetic cost scales with operand size. Decimal exponent alignment,
division, powers, and roots can allocate substantially, so limits are part of
the API rather than an operational afterthought. Reuse immutable values and
contexts freely, but do not raise limits for untrusted input.

Run `make benchmark` for allocation and scaling comparisons with direct
`math/big`, `apd`, and `shopspring/decimal`. Benchmark semantics are aligned
where possible; different rounding or representation contracts are identified
in benchmark names.

The suite covers bounded powers, roots, division, rational normalization,
decimal expansion, formatting, binary-float square roots, and encoding
conversion. See the [recorded baseline](benchmark-baseline.md). Allocation
budgets are deterministic gates; wall-clock results are recorded but are not
used as noisy shared-runner assertions.
