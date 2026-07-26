# Benchmark baseline

The local baseline was captured on 20 July 2026 with Go 1.26.5 on Darwin arm64
using an Apple M4 Max and `-benchtime=20ms`. Timings are diagnostic rather than
CI thresholds because shared runners are noisy; deterministic allocation
ceilings are enforced by `TestRepresentativeAllocationBudgets`.

| Workload | Time | Bytes | Allocations |
| --- | ---: | ---: | ---: |
| 2,048-bit rational normalization | 1.62 us | 2,899 | 11 |
| 1,000-place rational expansion | 10.16 us | 7,866 | 24 |
| 4,096-bit float square root and conversion | 543.61 us | 11,118 | 20 |
| Decimal binary round trip | 1.62 us | 1,248 | 28 |

The benchmark suite also measures bounded integer powers and roots, decimal
division and formatting, and equivalent `math/big`, `apd`, and
`shopspring/decimal` operations. Functional resource caps are tested separately
so a timing fluctuation cannot turn a safe rejection into an unreliable timeout.
