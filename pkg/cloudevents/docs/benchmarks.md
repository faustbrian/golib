# Benchmarks

The equivalent-event benchmark compares Golib with `cloudevents/sdk-go`
v2.16.2. Both implementations are initialized from, and decode, the same
canonical JSON bytes. Setup rejects any difference in the decoded specification
version, ID, source, type, data content type, or payload before timing begins.
The benchmark measures only in-process encoding and decoding; broker, HTTP,
schema, and application work are excluded.

## 2026-08-11 equivalent-event result

- Go: 1.26.5
- Platform: Darwin arm64, Apple M4 Max
- Command: `go test . -run '^$' -bench '^BenchmarkJSONEquivalentEvent$' -benchmem -benchtime=100ms -count=10`
- Method: median of ten samples; throughput is derived from median latency

| Operation | Implementation | Median | Approx. throughput | Bytes/op | Allocs/op |
| --- | --- | ---: | ---: | ---: | ---: |
| JSON encode | Golib | 3,110 ns/op | 321,543 ops/s | 1,667 | 33 |
| JSON encode | sdk-go v2.16.2 | 1,991 ns/op | 502,260 ops/s | 746 | 6 |
| JSON decode | Golib | 12,949 ns/op | 77,226 ops/s | 4,965 | 157 |
| JSON decode | sdk-go v2.16.2 | 3,707 ns/op | 269,760 ops/s | 904 | 21 |

The ten-sample ranges were 2,025-6,270 ns/op for Golib encoding,
1,337-2,893 ns/op for SDK encoding, 9,443-24,453 ns/op for Golib decoding,
and 1,817-8,304 ns/op for SDK decoding. The range is reported because local
machine scheduling made latency variable; allocation counts were stable.

## Superseded 2026-08-09 baseline

The measurements below predate the shared-corpus setup check. The two decoders
received implementation-specific encodings, so the decode figures are retained
only as historical observations and are not comparable with current runs.

- Go: 1.26.5
- Platform: Darwin arm64, Apple M4 Max
- Command: `go test . -run '^$' -bench '^BenchmarkJSONEquivalentEvent$' -benchmem -benchtime=100ms -count=10`
- Method: median of ten samples; throughput is derived from median latency

| Operation | Implementation | Median | Approx. throughput | Bytes/op | Allocs/op |
| --- | --- | ---: | ---: | ---: | ---: |
| JSON encode | Golib | 42,004 ns/op | 23,807 ops/s | 1,650 | 32 |
| JSON encode | sdk-go v2.16.2 | 25,389 ns/op | 39,387 ops/s | 746 | 6 |
| JSON decode | Golib | 122,876 ns/op | 8,138 ops/s | 4,962 | 157 |
| JSON decode | sdk-go v2.16.2 | 32,501 ns/op | 30,768 ops/s | 905 | 21 |

The ten-sample ranges were 12,840-178,185 ns/op for Golib encoding,
5,624-61,228 ns/op for SDK encoding, 74,360-443,552 ns/op for Golib decoding,
and 17,821-56,712 ns/op for SDK decoding. Concurrent repository verification
made latency noisy, so this baseline is evidence of relative cost and
allocations, not a release regression budget. A budget requires an isolated,
repeatable runner and statistical comparison against this corpus.
