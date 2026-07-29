# Benchmark methodology and results

## Claim boundary

Benchmarks characterize this pre-v1 implementation on one recorded machine.
They are not service-level objectives, portable regression thresholds, or
evidence that a supplied Ethereum root is canonical. The CI benchmark gate is
a reproducibility smoke test; it does not rank clients.

The committed raw data, summaries, environment, and benchmark-input checksums
are under [`benchmarks`](../benchmarks). Results are invalid after any checksum
in `benchmarks/inputs.sha256` changes.

## Workloads

All local corpus keys are eight-byte big-endian indexes. Values are 32-byte
big-endian words. Setup is outside the timed region unless construction,
commit, pruning, or recovery setup is the operation being measured.

| Surface | Corpus and measured operation |
| --- | --- |
| Empty/populated `Get` | Empty trie or key 512 in a 1,024-entry raw trie |
| Update/replace/delete | One immutable mutation derived from the same 1,024-entry snapshot |
| Atomic batch | Sixteen puts against the populated snapshot |
| Root | Read the already-calculated 32-byte commitment |
| Transaction/receipt roots | 256 London type-2 envelopes with exact RLP-index keys |
| State/storage | One canonical account or non-zero storage slot |
| Commit | Persist and publish the 1,024-entry trie to a fresh memory store |
| Proof | Key 512 membership proof |
| Multiproof | 32 present keys selected at a stride of 31 |
| Range proof | Complete interval containing 32 consecutive keys |
| EIP-1186 proof set | One account proof followed by 16 storage-slot proofs |
| Iteration | Full or prefix traversal of 1,024 entries |
| Construction | 1,024 ordinary immutable updates or the sorted builder |
| Rebuild | Raw 1,024-entry rebuild or 256-account state rebuild |
| Stored reads | Loaded or reloaded snapshot over the memory store, with reads reported |
| Filesystem warm read | Loaded 256-entry snapshot; exact node files remain in the operating-system cache |
| Filesystem reopen/read | Open and close the durable directory for every lookup; operating-system caches are not purged |
| Filesystem commit | One new key update plus synced node files and atomic root publication |
| Rejection | One mutated proof or one corrupt root-node response |
| Recovery/pruning | One exact-node overlay recovery or atomic memory-store prune |
| Parallel read | Populated immutable `Get` through `testing.B.RunParallel` |

The memory adapter is process-local. Its loaded/reloaded tracks measure the
core's store boundary and node-read count. The filesystem track separately
measures a reused warm handle, a newly opened handle, and durable commit. The
reopen track is not presented as physical cold-cache latency because the
benchmark does not purge operating-system or device caches.

## Environment and method

The 2026-07-29 baseline used:

- macOS 27.0 build `26A5388g`, `darwin/arm64`;
- Apple M4 Max;
- Go 1.26.5;
- `GOMAXPROCS=1` for local distributions and the Geth comparison;
- `GOMAXPROCS=16` only for the parallel-read track;
- ten samples per workload;
- a 250 ms minimum sample time for the complete local matrix;
- a 300 ms minimum sample time for the filesystem track;
- a 500 ms minimum sample time for comparison and parallel tracks; and
- `golang.org/x/perf/cmd/benchstat` at
  `v0.0.0-20260709024250-82a0b07e230d`.

`benchstat` reports the median and its non-parametric 95% confidence interval.
The comparison uses its two-sided Mann-Whitney U test. Allocation counts,
proof bytes, proof nodes, and store reads are deterministic in this corpus.
Timing variance was high on several workloads, so the raw distributions and
intervals are part of every result.

## Local baseline

Selected medians from the complete matrix are:

| Workload | Time/op | Bytes/op | Allocs/op | Additional metric |
| --- | ---: | ---: | ---: | ---: |
| Populated `Get` | 217.3 ns | 104 | 4 | — |
| Update | 498.6 µs | 274.3 KiB | 187 | — |
| Atomic batch | 848.0 µs | 298.4 KiB | 657 | 16 puts |
| Transaction root | 1.997 ms | 625.9 KiB | 7,569 | 256 values |
| Receipt root | 1.134 ms | 635.2 KiB | 7,570 | 256 values |
| Proof generation | 44.62 µs | 34.96 KiB | 335 | — |
| Proof verification | 154.0 µs | 16.86 KiB | 156 | 1,288 B / 5 nodes |
| Multiproof verification | 378.4 µs | 242.3 KiB | 2,413 | 20,461 B / 70 nodes |
| Range verification | 83.68 µs | 55.46 KiB | 776 | 3,437 B / 38 nodes |
| EIP-1186 proof set | 228.7 µs | 146.7 KiB | 1,027 | 7,104 B / 45 nodes |
| Full iteration | 205.5 µs | 89.07 KiB | 4,165 | 1,024 entries |
| Ordinary construction | 157.9 ms | 35.03 MiB | 271,000 | 1,024 entries |
| Sorted construction | 5.916 ms | 3.653 MiB | 22,390 | 1,024 entries |
| State rebuild | 2.800 ms | 1.277 MiB | 11,660 | 256 accounts |
| Malformed-proof rejection | 396.0 ns | 16 | 1 | — |
| Corrupt-node rejection | 1.480 µs | 632 | 10 | 1 read |

The exact confidence intervals and every workload are in
`benchmarks/raw/2026-07-29-local-benchstat.txt`. The one-iteration process
measurement reported 105,644,032 bytes maximum resident set size; this includes
the Go test process and harness and is not a retained-trie-only measurement.

At 16 workers, populated immutable reads measured 121.7 ns/op median with a
20% confidence interval, 104 B/op, and four allocations. No serial/parallel
ranking is made because `RunParallel` reports aggregate operation time under a
different worker configuration.

## Filesystem store

The dependency-free filesystem adapter used a separate 256-entry string-key
corpus. Ten serial samples produced:

| Workload | Time/op | 95% interval | Bytes/op | Allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Warm loaded `Get` | 232.1 µs | ±32% | 28.18 KiB | 207 |
| Reopen and `Get` | 1.262 ms | ±31% | 190.5 KiB | 2,167 |
| Update and durable commit | 50.13 ms | ±19% | 41.68 KiB | 360 |

Every commit sample syncs content-addressed node files, the node directory, the
root record, and the store directory. The reopen result includes root-record
validation, temporary-file recovery scans, and a bounded node inventory. The
complete observed distributions are retained; no outlier was removed.
Raw samples and the pinned summary are in
`benchmarks/raw/2026-07-29-filesystem.txt` and
`benchmarks/raw/2026-07-29-filesystem-benchstat.txt`.

## Geth comparison

The only ranked candidate workload implemented here is populated raw lookup
with owned output:

- both implementations contain the exact same 1,024 key/value pairs;
- setup asserts identical roots before timing;
- both read key 512;
- the local API's returned value is already owned;
- the Geth result is copied during the timed operation to match ownership; and
- both execute serially with `GOMAXPROCS=1`.

The pinned comparison oracle is go-ethereum v1.17.3 at
`117e067f0f0bae1a17082321f224dedb6765b10f`.

| Implementation | Time/op | 95% interval | Bytes/op | Allocs/op |
| --- | ---: | ---: | ---: | ---: |
| This module | 482.4 ns | ±113% | 104 | 4 |
| Geth v1.17.3 | 200.9 ns | ±33% | 80 | 3 |

Although `benchstat` reports a difference for this sample, the local interval
is too wide for a stable performance ranking. The result is published as a
reproducible comparison and an optimization signal, not a claim that either
implementation is generally faster.

The execution-time implementation inventory was:

| Implementation | Upstream HEAD resolved 2026-07-29 | Treatment |
| --- | --- | --- |
| go-ethereum | `b988c00bf4cba86ef5c43691ce84f4f2aea2821f` | Pinned v1.17.3 Go comparison and compatibility oracle |
| Erigon | `7efe658854c9db74dd28fce76dfc9803d0fbdb4d` | Go candidate; no ranked track until ownership and storage behavior are equivalent |
| Nethermind | `f55671d78768eb55cec891697b1a2652be03f1c7` | Interoperability and algorithm review only |
| Hyperledger Besu | `15f0783bf1e4b0e6676359d73e543a69879bfb07` | Interoperability and algorithm review only |
| ethereumjs | `3fa006c51b21877d160960e2d87dc3da6c58a71c` | Pinned MPT v10.1.2 interoperability oracle; no cross-runtime ranking |

Mutable versus immutable updates, commit ownership, proof strictness, cache
state, and persistence differ across clients. Those surfaces remain separate
until a harness can normalize every listed behavior. Cross-language process
and runtime overhead is not presented as trie performance.

## Reproduction

The CI-compatible smoke matrix is:

```sh
make benchmark
```

The pinned Geth comparison is:

```sh
make benchmark-comparison
```

To reproduce the release distributions:

```sh
GOMAXPROCS=1 GOWORK=off go test -run '^$' -bench '^Benchmark' \
  -benchmem -benchtime=250ms -count=10 .

GOMAXPROCS=16 GOWORK=off go test -run '^$' \
  -bench '^BenchmarkParallelGet$' -benchmem \
  -benchtime=500ms -count=10 .

GOMAXPROCS=1 GOWORK=off go test -run '^$' \
  -bench '^BenchmarkFilesystem(WarmGet|OpenAndGet|Commit)$' \
  -benchmem -benchtime=300ms -count=10 ./filesystem
```

Capture raw output before running the pinned `benchstat` revision. Do not
compare results unless the input checksum manifest, toolchain, worker count,
corpus, store behavior, and ownership contract match.
