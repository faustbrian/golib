# Performance

The core is optimized for correctness under bounded resources. Measure with
your value sizes, codec, backend latency, and contention pattern.

Included benchmarks report allocations for fresh hits, misses, stale reads,
contended `GetOrLoad`, and OTel observation:

```sh
make benchmark
```

The 2026-07-15 Apple M4 Max baseline with Go 1.25 was:

| Path | Time | Bytes | Allocations |
| --- | ---: | ---: | ---: |
| Fresh hit | 636 ns/op | 2,616 B/op | 11 allocs/op |
| Miss | 216 ns/op | 168 B/op | 4 allocs/op |
| Stale | 770 ns/op | 2,616 B/op | 11 allocs/op |
| Contended load, 32 callers | 35.4 us/iteration | 79,213 B/iteration | 393 allocs/iteration |
| OTel event | 206 ns/op | 328 B/op | 6 allocs/op |

These numbers are comparison points, not portable thresholds. The contended
benchmark creates a new key and 32 callers per iteration.

Backend keys use SHA-256 and base64url to prevent sensitive logical-key leakage
and provide fixed-size collision-resistant identity. JSON emphasizes
interoperability and strict evolution rather than minimum allocations; custom
codecs can improve throughput while preserving the documented contract.

Memory capacity counts key and payload bytes but not Go runtime/container
overhead. Leave headroom when setting `MaxBytes`. Redis and Valkey reads use a
server-side length check before fetching the value, and adapters cap the full
wire record.

Tune `MaxConcurrent` below downstream connection and rate limits.
`MaxWaitersPerKey` bounds retained caller state during a hot-key stampede.
Jitter spreads expiration work but shortens effective fresh TTL. Sliding TTL
adds a conditional backend write to each fresh hit.

Treat benchmark regression thresholds as environment-specific. CI records
benchmark output and uses semantic tests, race checks, leak checks, and exact
coverage as hard correctness gates.
