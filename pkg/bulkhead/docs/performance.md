# Performance

The benchmark suite compares:

- this package's admission policy and owned permit;
- direct `golang.org/x/sync/semaphore` v0.22.0 permit accounting; and
- Failsafe-Go v0.9.6 unit-permit bulkhead behavior.

The fast-path comparison has equivalent capacity-one acquire/release behavior.
It is not semantically identical: direct x/sync lacks resource identity,
duplicate-release protection, snapshots, bounded external queue policy,
observers, and drain; Failsafe-Go lacks weighted capacity and this package's
bounded explicit partition and queue-cardinality contracts. Rejection
benchmarks cover this package separately.

Run repeated samples on an idle pinned host:

```sh
GOWORK=off go test -run '^$' -bench 'Benchmark(Bulkhead|XSync|Failsafe)' \
  -benchmem -benchtime=1s -count=10
```

Record Go version, GOOS/GOARCH, CPU, `GOMAXPROCS`, GC settings, dependency
versions, raw output, and `benchstat` confidence intervals before making a
regression or superiority claim. The repository smoke gate proves only that
the benchmark harness executes; it is not stable comparative evidence.

## 2026-08-02 exploratory baseline

Five one-second samples on Go 1.26.5, darwin/arm64, Apple M4 Max,
`GOMAXPROCS=16`, and default GC produced these sample medians:

| Benchmark | Median ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Bulkhead acquire/release | 129.6 | 32 | 1 |
| Bulkhead immediate rejection | 59.99 | 0 | 0 |
| x/sync semaphore acquire/release | 9.271 | 0 | 0 |
| Failsafe-Go acquire/release | 31.50 | 0 | 0 |

The statistic is the median of five independent benchmark process samples,
not a confidence interval. The [raw samples](benchmarks/2026-08-02-m4-max.txt)
are preserved. These numbers establish a local baseline only; semantic work
performed differs as disclosed above, and no cross-library superiority claim
is made.
