# Performance

`System.Now` and an established `System.Measure` closure allocate zero bytes in
the maintained baseline. Manual scheduling uses a binary heap: create, reset,
and stop are `O(log n)`, and advancement is
`O(k log n)` for `k` processed deadlines.

The 2026-07-16 Apple M4 Max cold baseline (`-benchtime=1x`) measured:

| Benchmark | Time | Allocations |
| --- | ---: | ---: |
| `SystemNow` | 208 ns | 0 |
| `SystemMeasure` | 250 ns | 0 |
| Manual fan-out, 1 timer | 7.8 µs | 13 |
| Manual fan-out, 100 timers | 27.4 µs | 416 |
| Manual fan-out, 10,000 timers | 1.90 ms | 40,026 |

Absolute numbers vary by toolchain and hardware. Compare changes on the same
machine with `make benchmark`; prioritize scaling shape, allocations, and
regressions over cross-machine nanoseconds.

The 2026-07-17 Apple M4 Max cold/contended baseline used five 3-second
samples:

| Benchmark | Range | Allocations |
| --- | ---: | ---: |
| Manual cold start | 453-578 ns | 3 |
| Manual contended `Now`, 16 CPUs | 92.5-117.9 ns | 0 |

Reproduce it with:

```sh
go test -run '^$' \
  -bench 'BenchmarkManual(ColdStart|NowContended)$' \
  -benchmem -benchtime=3s -count=5
```

Ticker advancement processes each logical deadline so ordering, work budgets,
and backpressure remain explicit. Large periods or fan-out should use limits
that reflect the owning test or application.
