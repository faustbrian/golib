# Performance

The non-releasable `benchmarks/comparison` module compares:

- this package's admission policy and owned permit;
- direct `golang.org/x/sync/semaphore` v0.22.0 permit accounting;
- Failsafe-Go v0.9.6 unit-permit bulkhead behavior; and
- Fortify v1.6.0 unit-permit bulkhead execution.

The fast-path comparison has equivalent capacity-one acquire/release behavior.
It is not semantically identical: direct x/sync lacks resource identity,
duplicate-release protection, snapshots, bounded external queue policy,
observers, and drain; Failsafe-Go lacks weighted capacity and this package's
bounded explicit partition and queue-cardinality contracts. Fortify has a
bounded queue and close operation, but queued work uses a worker goroutine,
ordering is not strict FIFO, and close does not drain admitted work. The direct
semaphore and Failsafe-Go rejection cases expose a boolean try-acquire; Fortify
and this package return typed errors.

Package-specific benchmarks additionally measure parallel admitted throughput,
wait wake-up p50/p95/p99, FIFO violations across eight queued callers,
cancellation churn, synchronous observers, and registry lookup across 128
partitions. The wake-up percentiles measure release-to-waiter-return latency;
the surrounding `ns/op` includes goroutine and queue orchestration. Fairness is
also a behavioral test invariant, not a timing claim.

Run repeated samples on an idle pinned host:

```sh
cd benchmarks/comparison
go test -run '^$' -bench . -benchmem -benchtime=1s -count=10
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

## 2026-08-02 hardening baseline

Five 500-millisecond samples on the same machine expanded the candidate and
workload matrix. Selected sample medians are:

| Workload | Candidate | Median | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| admitted acquire/release | Bulkhead | 199.5 ns/op | 32 | 1 |
| admitted acquire/release | x/sync semaphore | 19.85 ns/op | 0 | 0 |
| admitted acquire/release | Failsafe-Go | 94.52 ns/op | 0 | 0 |
| admitted execute | Fortify | 318.2 ns/op | 0 | 0 |
| saturated rejection | Bulkhead | 226.8 ns/op | 0 | 0 |
| saturated try-acquire | x/sync semaphore | 6.606 ns/op | 0 | 0 |
| saturated try-acquire | Failsafe-Go | 6.690 ns/op | 0 | 0 |
| saturated execute rejection | Fortify | 117.5 ns/op | 16 | 1 |
| parallel admitted throughput | Bulkhead | 2,178,448 admitted/s | 32 | 1 |
| cancellation churn | Bulkhead | 10,640 ns/op | 1,468 | 18 |
| synchronous observer | Bulkhead | 373.3 ns/op | 32 | 1 |
| 128-partition lookup | Bulkhead | 10.37 ns/op | 0 | 0 |

Median release-to-waiter-return latency was 458 ns p50, 1,000 ns p95, and
2,209 ns p99. Parallel throughput reported zero rejected operations in every
sample, and all five fairness samples reported zero FIFO violations. The
[raw hardening samples](benchmarks/2026-08-02-hardening-m4-max.txt) preserve
every result and environment field. Candidate operations expose different
metadata, error, queue, fairness, panic, and shutdown semantics, so the table
is a cost baseline rather than a ranking.
