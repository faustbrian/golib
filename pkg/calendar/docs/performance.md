# Performance

Benchmarks use `make benchmark`. On an Apple M4 Max with Go 1.26.5, representative
local measurements were approximately:

| Operation | Time | Allocations |
| --- | ---: | ---: |
| Parse date | 13 ns | 0 |
| Add month | 6 ns | 0 |
| ISO week | 18 ns | 0 |
| Business add 20 days | 2.0 µs | 0 |
| Resolve timezone fold | 2.5 µs | 4 |

These figures are evidence, not portable service-level guarantees. Compare on
the same hardware/toolchain and investigate material regressions. Calendar
lookup is map-based; business navigation remains intentionally bounded linear
work in examined civil days.

Allocation ceilings are blocking because they are deterministic under the
pinned Go toolchain: date parse/month/ISO and business lookup/navigation permit
zero allocations, timezone fold resolution permits four, wire encode/decode
permit four/four, and native pgx binary encoding permits two. The package-local
`Test*AllocationBudget*` tests enforce these limits during `make test` and race
testing. The race build permits two instrumentation-only allocations in the wire
codec. Wall-clock benchmark values remain evidence rather than noisy CI
thresholds.
