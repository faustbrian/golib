# Benchmark evidence

## 2026-08-09 baseline

Environment:

- Go `go1.26.5 darwin/arm64`;
- Apple M4 Max;
- logical benchmark parallelism suffix `-16`;
- adapter module at its unreleased implementation state; and
- no broker, network, schema registry, or external service involved.

Command:

```sh
go test . -run '^$' -bench . -benchmem -count=10
```

The reported latency is the median of ten independently calibrated Go
benchmark samples. The range is included because other package-local work was
active on the development host; these results establish an allocation and
order-of-magnitude baseline, not a release regression budget.

| Benchmark | Median | Range | Approx. throughput | Bytes/op | Allocs/op | Corpus |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| Kafka binary decode round trip | 14,663 ns/op | 11,765-36,728 ns/op | 68,199 ops/s | 926 | 26 | one binary-mode event with required attributes and a small JSON payload |
| Queue conversion round trip | 10,886 ns/op | 7,137-13,817 ns/op | 91,861 ops/s | 808 | 14 | one queue job with a small JSON payload and retained execution state |

Allocation counts were identical across all ten samples. A future performance
claim must rerun the same corpus on a controlled host, preserve the raw Go
benchmark output, and compare statistically equivalent samples rather than a
single result.
