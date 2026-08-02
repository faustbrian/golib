# Performance

The root benchmark suite measures disabled and no-match selection,
deterministic and seeded matches, observer delivery, injected latency with both
an injected sleeper and the system timer, bounded byte corruption, and a direct
error-returning test double. A parallel selection benchmark exposes mutex
contention under the documented total ordering. The direct double establishes the minimum cost of
the same caller-visible failure outcome; it does not perform rule selection,
accounting, attribution, or observation.

The isolated [`benchmarks/comparison`](../benchmarks/comparison) module compares
an always-injected error with goresilience v0.2.0 and the closest Failsafe-Go
v0.9.6 execution outcome. Failsafe-Go does not provide a failure-injection
policy, so its case times a caller-supplied failing function through its
executor and is explicitly not an injection-fidelity comparison.
goresilience requires a fresh runner per sample to keep its feedback-driven
percentage mode on the first, injected call, so its construction cost remains
inside the timed workload.

Run repeated samples on an idle pinned host:

```sh
GOWORK=off go test -run '^$' -bench . -benchmem -benchtime=1s -count=10
go -C benchmarks/comparison test -run '^$' -bench . -benchmem \
  -benchtime=1s -count=10
```

Record Go version, GOOS/GOARCH, CPU, `GOMAXPROCS`, GC settings, dependency
versions, raw output, and `benchstat` confidence intervals before making a
regression claim. The repository smoke gate proves only that benchmark
harnesses execute. These in-process results must not be compared with proxy,
kernel, broker, or cluster throughput as if fidelity were equivalent.

The engine starts no goroutines. The system-timer benchmark therefore measures
timer cost but no package-owned goroutine lifecycle; the leak gate verifies
that tests leave no unexpected goroutines behind.

## 2026-08-02 exploratory baseline

Five sequential 500-millisecond samples on Go 1.26.5, darwin/arm64, Apple M4
Max, `GOMAXPROCS=16`, and default GC produced these sample medians. Approximate
throughput is derived from the median latency and rounded; it is not a separate
measurement.

| Root workload | Median ns/op | Approx M ops/s | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Disabled decision | 3.147 | 317.8 | 0 | 0 |
| Constructed injector, no rules | 9.805 | 102.0 | 0 | 0 |
| Deterministic match | 156.5 | 6.39 | 80 | 1 |
| Seeded probability | 29.88 | 33.47 | 8 | 0 |
| Observed match | 471.0 | 2.12 | 192 | 2 |
| Corrupt reader | 187.4 | 5.34 | 80 | 1 |
| Latency with injected sleeper | 131.4 | 7.61 | 80 | 1 |
| Latency with system timer | 737.7 | 1.36 | 328 | 4 |
| Direct error double | 2.546 | 392.8 | 0 | 0 |
| Parallel ordered selection | 190.7 | 5.24 | 80 | 1 |

The isolated comparison harness, run sequentially with the same sample count
and duration, produced:

| Failure outcome | Median ns/op | Approx M ops/s | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| fault-injection | 296.9 | 3.37 | 80 | 1 |
| goresilience fresh runner and first injection | 608.3 | 1.64 | 176 | 6 |
| Failsafe-Go caller-supplied failure | 650.2 | 1.54 | 344 | 12 |
| Direct error double | 2.792 | 358.2 | 0 | 0 |

These are medians, not confidence intervals, and the comparison workloads do
different internal work as disclosed above. They establish a local baseline
only and do not support a cross-library superiority claim.
