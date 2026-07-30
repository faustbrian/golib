# Component Microbenchmarks

## Status and scope

These are pre-v1 component microbenchmarks for the implemented cryptographic
boundary: canonical Banderwagon commitment and scalar encoding, strict raw
aggregate-opening-proof decoding, strict profile-bound root decoding, the
commitment-to-field map, serial fixed-width vector commitment, and internal
aggregate tree-proof generation and verification. They do not measure a public
API, witness, storage adapter, or an equivalent end-to-end workload, and they
support no comparative performance claim.

One additional component benchmark measures rebuilding the implemented
immutable committed-node arena and mathematical root for the pinned four-entry,
two-stem corpus. It excludes builder and generator initialization, proof work,
serialization, persistence, and incremental updates. It is not an end-to-end
tree benchmark or a comparison with either reference implementation.

Two authenticated-state component benchmarks measure an immutable lookup and a
single-value replacement that rebuilds a one-entry committed tree through an
already initialized snapshot builder. They exclude snapshot construction,
generator initialization, proofs, witnesses, serialization, and persistence.
Two further component benchmarks measure canonicalizing sixteen fixed-size tree
claims and returning an owned copy of the resulting claim set. They exclude
path construction, opening generation, proof encoding, and verification.
One snapshot proof-material benchmark derives eight membership claims, eight
missing-stem absence claims, their canonical topology, and their deduplicated
non-root commitments from one already constructed immutable snapshot. It
excludes snapshot construction, aggregate-opening generation, proof encoding,
verification, and persistence.
Two proof-container benchmarks measure canonical validation and ordering of
sixteen claims, sixteen stem paths, and sixteen non-root commitment paths, plus
owned copies of the retained stem and commitment metadata. They reuse an
already decoded raw opening payload and exclude opening generation,
cryptographic verification, and public interoperability. Three serialization
benchmarks measure canonical encoding, strict decoding, and wrong-length
rejection for the same internal unverified proof boundary.
Two proof-engine benchmarks measure generation and independent verification of
one sixteen-key membership proof through the complete internal snapshot,
query-reconstruction, aggregate-opening, and tree-proof container path. They
reuse initialized snapshot and proof engines and exclude setup, persistence,
witness processing, and public API ownership costs.

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

GOWORK=off go test ./internal/committedtree -run '^$' \
  -bench '^BenchmarkBuildFourEntries$' -benchmem -count=5

GOWORK=off go test ./internal/authstate -run '^$' \
  -bench '^(BenchmarkSnapshotGet|BenchmarkSnapshotApply|BenchmarkNewClaimSet|BenchmarkClaimSetCopy|BenchmarkSnapshotProofMaterial|BenchmarkNewTreeProof|BenchmarkTreeProofCopy|BenchmarkEncodeTreeProof|BenchmarkDecodeTreeProof)' \
  -benchmem -count=5

GOWORK=off go test ./internal/authstate -run '^$' \
  -bench '^BenchmarkProofEngine$' -benchmem -benchtime=1x -count=5
```

Environment:

- Date: 2026-07-30
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
| Decode committed 42-byte root | 9574, 7986, 8663, 8545, 7773 | 32 | 1 |
| Decode explicit empty 42-byte root | 20.94, 26.36, 25.16, 21.12, 25.21 | 0 | 0 |
| Reject 41-byte root | 159.1, 139.5, 165.8, 131.1, 128.1 | 80 | 2 |
| Decode canonical 576-byte raw opening proof | 429197, 110929, 115482, 111094, 107296 | 544 | 17 |
| Reject 575-byte raw opening proof | 77.65, 78.30, 81.36, 79.81, 79.23 | 80 | 2 |
| Commit sparse five-term vector | 108785, 69867, 64012, 231937, 70566 | 1321 | 20 |
| Commit dense 256-term vector | 4835446, 16280795, 15844953, 2671274, 2909500 | 67632-67655 | 1024 |
| Build four-entry, two-stem committed root | 504199, 500008, 447677, 1315124, 1282870 | 7450-7452 | 89 |
| Extract one immutable committed-tree proof path | 3765, 3824, 4398, 4883, 6526 | 4864 | 1 |
| Get one present snapshot value | 22.06, 23.42, 22.77, 21.85, 20.84 | 0 | 0 |
| Replace one value and rebuild its committed root | 355831, 219311, 199943, 165296, 152352 | 2860 | 37 |
| Canonicalize sixteen tree claims | 2886, 1112, 1013, 1374, 1169 | 2304 | 2 |
| Copy sixteen canonical tree claims | 211.9, 241.0, 356.6, 728.3, 300.0 | 1152 | 1 |
| Assemble sixteen-key snapshot proof material | 186928, 54189, 59131, 61148, 50293 | 161793-161800 | 32 |
| Canonicalize sixteen-claim unverified tree proof | 33948, 46259, 33676, 31726, 31940 | 50816-50818 | 7 |
| Copy sixteen-claim tree-proof metadata | 1306, 1680, 3757, 2622, 2787 | 3456 | 2 |
| Encode canonical unverified tree proof | 5136, 16481, 16351, 13961, 6149 | 1024 | 1 |
| Decode canonical unverified tree proof | 253440, 627923, 368237, 277831, 307604 | 4354-4355 | 30 |
| Reject wrong-length encoded tree proof | 474.3, 374.8, 377.9, 267.9, 338.3 | 96 | 2 |
| Generate sixteen-key aggregate tree proof | 23815417, 24936125, 24801834, 24196208, 27617792 | 10085560-10086816 | 5247-5270 |
| Verify sixteen-key aggregate tree proof | 3257666, 3220208, 3738459, 3016125, 3131959 | 610968-611408 | 1143-1150 |

These results are descriptive evidence for this source and environment, not a
portable performance guarantee. The vector samples show substantial local
variance and therefore are not suitable as a release threshold or comparative
ranking.
