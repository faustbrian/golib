# Backend Encoding Benchmarks

## Status and scope

These are pre-v1 component microbenchmarks for the only implemented
cryptographic boundary: canonical Banderwagon commitment and scalar encoding.
They do not measure a Verkle tree, vector commitment, proof, witness, storage
adapter, or an equivalent end-to-end workload, and they support no comparative
performance claim.

The accepted-input benchmarks exclude fixture construction from the measured
loop. Rejection benchmarks measure the complete fail-closed decoder path,
including typed error construction. Every benchmark reports processed bytes,
allocations, and allocation bytes.

## Method

Command:

```console
GOWORK=off go test ./internal/backend -run '^$' \
  -bench '^(BenchmarkDecode|BenchmarkEncode)' -benchmem -count=5
```

Environment:

- Date: 2026-07-29
- Go: `go1.26.5`
- OS: macOS 27.0 (`26A5388g`)
- Architecture: `darwin/arm64`
- CPU: Apple M4 Max
- `GOMAXPROCS`: Go default, reported by the benchmark suffix as `16`
- Backend: `github.com/crate-crypto/go-ipa`
  `v0.0.0-20240223125850-b1e8a79f509c`

The Go benchmark harness calibrates each sample independently. No latency
threshold is enforced because no stable cross-runner baseline exists yet.
Reproduce measurements on the target deployment hardware before using them for
capacity planning.

## Raw samples

The values below are the five samples emitted by the command above. Times are
nanoseconds per operation.

| Benchmark | ns/op samples | B/op | allocs/op |
| --- | --- | ---: | ---: |
| Decode canonical commitment | 6186, 6276, 6203, 6144, 6157 | 32 | 1 |
| Reject identity commitment | 65.73, 65.19, 66.37, 73.76, 66.75 | 64 | 2 |
| Encode commitment | 20.08, 19.94, 19.95, 20.08, 20.26 | 0 | 0 |
| Decode canonical scalar | 39.94, 39.55, 40.14, 40.19, 40.43 | 0 | 0 |
| Reject non-canonical scalar | 148.5, 141.4, 141.7, 146.1, 143.9 | 176 | 5 |
| Encode scalar | 8.678, 8.683, 8.585, 8.687, 8.743 | 0 | 0 |

These results are descriptive evidence for this source and environment, not a
portable performance guarantee.
