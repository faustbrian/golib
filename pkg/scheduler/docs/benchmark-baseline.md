# Benchmark baseline

This baseline was captured on 2026-07-16 with Go 1.26.5 on darwin/arm64, an
Apple M4 Max with 16 logical CPUs, and 128 GiB RAM. Commands used a 500 ms
benchmark window and five samples:

```sh
go test . -run '^$' \
  -bench 'Benchmark(CompileSchedules|CompileAtScheduleLimit|DueScan|DueAtOccurrenceScanLimit|MemoryLeaseContention)$' \
  -benchmem -benchtime=500ms -count=5
```

| Benchmark | Documented load | Observed range | Allocation baseline |
|---|---:|---:|---:|
| compile schedules | 1,000 definitions | 0.832-0.902 ms/op | 1.61 MB, 29,021 allocs/op |
| compile at limit | 10,000 definitions | 8.10-8.22 ms/op | 16.31 MB, about 290,064 allocs/op |
| due scan | 1,440 minute boundaries, retain 100 | 1.069-1.092 ms/op | 366,848 B, 7,201 allocs/op |
| occurrence scan rejection | 10,000 candidates | 14.13-14.28 ms/op | 2.58 MB, 50,001 allocs/op |
| memory lease contention | parallel shared key | 237-242 ns/op | 8 B, 0 allocs/op |

These numbers are a comparison baseline, not portable latency promises.
Hardware, Go, parser, and backend versions must be recorded with new results.
For release review, investigate a median regression above 25 percent at the
same environment and a repeatable allocation increase above 10 percent.

The broad safety ceilings are intentionally looser than this machine snapshot:
compiling 10,000 definitions should remain below 25 ms and 25 MiB per
operation; rejecting a 10,000-candidate scan should remain below 40 ms and
5 MiB; and memory lease contention should remain below 1 us/op with at most one
allocation. These thresholds leave room for shared CI variance while detecting
algorithmic regressions.

Persistent lease latency is deployment-specific. Record PostgreSQL or Valkey
version, topology, client pool settings, network round-trip time, contention,
and percentile distribution. A backend p99 above the configured lease
operation timeout is a correctness risk, not merely a throughput issue.
