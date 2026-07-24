# Performance and cost budgets

Schema cost is a conservative deterministic API weight. Field, relationship
edge, filter, and sort costs are summed with a base cost and rejected before an
adapter when `Bounds.MaxCost` is exceeded. It is not a PostgreSQL planner cost,
latency prediction, or authorization substitute.

The suite enforces allocation ceilings of 180 allocations for a representative
16-node compile, 80 for canonicalization, 3,000 for a 1,000-field schema, 30 for
cursor encoding, and 70 for decoding. These ceilings intentionally leave room
for compiler and race instrumentation while catching material regressions.

Apple M4 Max, Go 1.26.5 baseline (1 second sample, July 16, 2026):

| Operation | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Compile, 16 filters | 7,817 | 6,781 | 136 |
| Canonical plan | 6,555 | 3,560 | 34 |
| Compile, 48 filters | 18,128 | 17,318 | 362 |
| Build 1,000-field schema | 108,565 | 134,887 | 1,762 |
| Cursor encode | 2,510 | 3,814 | 18 |
| Cursor decode | 4,661 | 4,104 | 47 |

Run `make benchmarks` on comparable hardware and retain multiple samples before
changing budgets. Tune public structural bounds from workload evidence; never
raise them merely to accept a hostile or accidental expensive query.
