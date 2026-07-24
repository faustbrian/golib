# Performance

Benchmarks report allocations for configuration parsing, classification, pool
construction, acquisition wrapper overhead, transaction runner overhead,
observer dispatch, the OpenTelemetry adapter, real pooled acquire, and real
transaction round trips:

```sh
make benchmark BENCH_TIME=100ms
```

Results are environment-specific evidence, not service-level objectives. The
wrapper overhead benchmark uses a deterministic backend to isolate package
cost; integration benchmarks include PostgreSQL and container/network cost.

Reference allocation snapshot from 2026-07-16 on Go 1.26.5, darwin/arm64,
Apple M4 Max:

| Benchmark | Bytes/op | Allocs/op |
| --- | ---: | ---: |
| parse config | 7,268 | 55 |
| classify error | 8 | 1 |
| pool acquire wrapper | 272 | 4 |
| pool construction | 8,765 | 80 |
| observer dispatch | 0 | 0 |
| transaction runner | 0 | 0 |
| OpenTelemetry observer | 1,240 | 21 |
| real PostgreSQL pool acquire | 480 | 7 |
| real PostgreSQL transaction | 1,048 | 8 |

Use the benchmark artifact produced by CI for comparisons on its stable runner;
rerun the complete command before treating a local difference as a regression.

Optimize only after measuring the deployed workload. Connection establishment,
TLS, server execution, locks, rows, codecs, and exporter work normally dominate
these helpers. Keep telemetry labels bounded, avoid unnecessary parsing on hot
paths, reuse the pool, and do not trade cancellation or cleanup safety for a
microbenchmark improvement.
