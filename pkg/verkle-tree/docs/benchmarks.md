# Backend Microbenchmarks

## Status and scope

These are pre-v1 component microbenchmarks for the implemented cryptographic
boundary: canonical Banderwagon commitment and scalar encoding plus the
commitment-to-field map and serial fixed-width vector commitment. They do not
measure a Verkle tree, proof, witness, storage adapter, or an equivalent
end-to-end workload, and they support no comparative performance claim.

The accepted-input benchmarks exclude fixture construction from the measured
loop. Rejection benchmarks measure the complete fail-closed decoder path,
including typed error construction. Every benchmark reports processed bytes,
allocations, and allocation bytes.

## Method

Command:

```console
GOWORK=off go test ./internal/backend -run '^$' \
  -bench '^(BenchmarkDecode|BenchmarkEncode|BenchmarkCommitmentToScalar|BenchmarkCommitVector)' \
  -benchmem -count=5
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
| Decode canonical commitment | 26570, 22148, 25850, 28067, 11645 | 32 | 1 |
| Reject identity commitment | 286.4, 304.8, 214.2, 168.1, 241.0 | 64 | 2 |
| Encode commitment | 79.31, 57.34, 31.62, 50.70, 33.85 | 0 | 0 |
| Map commitment to scalar | 1474, 1392, 2558, 1942, 3718 | 8 | 1 |
| Decode canonical scalar | 77.17, 73.11, 70.89, 74.57, 74.84 | 0 | 0 |
| Reject non-canonical scalar | 309.7, 375.0, 215.9, 195.7, 176.3 | 176 | 5 |
| Encode scalar | 13.64, 23.18, 10.80, 11.81, 19.64 | 0 | 0 |
| Commit sparse five-term vector | 108785, 69867, 64012, 231937, 70566 | 1321 | 20 |
| Commit dense 256-term vector | 4835446, 16280795, 15844953, 2671274, 2909500 | 67632-67655 | 1024 |

These results are descriptive evidence for this source and environment, not a
portable performance guarantee. The vector samples show substantial local
variance and therefore are not suitable as a release threshold or comparative
ranking.
