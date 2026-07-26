# Benchmark methodology

Each result records fixture hash, item and container counts, constraints,
objective order, feasibility and verification time, score, container count,
utilization, known bound or optimum, solver work, allocations, bytes, machine,
Go version, execution revision, input fingerprint, seed, and parallelism. Raw
results are evidence, not API. Freshness follows the complete input
fingerprint; the execution revision is retained only for traceability.
Dependency selection and integrity are fingerprinted through `go.mod` and
`go.sum`, including owned dependencies used by each benchmark profile. The
BoxPacker profile excludes only the nested reference module's checksum of the
parent Knapsack zip because that zip contains this evidence and would create a
self-referential digest cycle; its `go.mod` checksum and every other checksum
remain inputs.

Reference comparisons use identical normalized lattices, weights, rotations,
stock, constraints, and objectives. Invalid or lower-quality output is not a
runtime win. Claims such as faster, better, or optimal require reproducible raw
evidence and a valid common semantic subset.

The pinned BoxPacker 4.2.0 adapter uses millimetres, grams, unrestricted
orthogonal rotation, unlimited copies of one box type, and pack-all semantics.
`make reference-integration` runs PHP and independently verifies every emitted
placement with the Go verifier before comparing exact statistics.

`make benchmark-compare` additionally builds the Go adapter once, performs one
warm-up process per implementation, then alternates ten fresh PHP and Go
processes. Wall time includes process and runtime startup, Composer autoload,
fixture construction, solving, and JSON serialization. It excludes compilation,
Composer installation, and independent verification. Every result is decoded
and independently verified before its timing, internal solve time, quality, or
peak RSS is recorded. Both delayed adapters are also killed through the same
50 ms external deadline. The raw evidence reports that PHP and Go do not expose
comparable cross-process allocation counts. No speed ratio or superiority claim
is made from this single tiny common-subset fixture.

The checked D-Wave `sample_data_1` fixture omits weight because the source does
not define it, preserves all permitted orthogonal rotations, and records a
reproducible heuristic upper bound of one container. That bound is a regression
threshold, not an independently proven optimum. The pinned revision, source
SHA-256, Apache-2.0 license, and conversion notice are recorded in
`specification/corpora.tsv` and `NOTICE`.

Checked-in raw evidence:

- [2026-07-24 Apple M4 Max](benchmarks/2026-07-24-darwin-arm64.md)
- [BoxPacker fresh-process comparison](benchmarks/raw/2026-07-24-boxpacker-runtime.json)

`benchmark-compare` collects ten fresh samples for tiny exact, ordinary,
orientation-heavy, weight-limited, stability-heavy, finite-stock, impossible,
fragmented adversarial, large bounded, verifier, and cancellation workloads.
The stability-heavy fixture combines exact support, transitive load, stack
count, and exact three-axis content center-of-gravity bounds.
It enforces reviewed p50, p95, allocation, byte, solution-quality, and
search-work budgets from
`specification/benchmark-thresholds.tsv`. It does not claim that a change beats
an external solver or prior release.

`make benchmark` also compiles the solver benchmark binary once and executes
every benchmark in five fresh processes under `/usr/bin/time`. The gate records
the maximum resident set size for each workload and fails unless the benchmark
inventory exactly matches `specification/benchmark-rss-thresholds.tsv`. The RSS
measurement includes the Go runtime and benchmark process but excludes
compilation. Darwin reports bytes directly; GNU time reports KiB, which the gate
converts to bytes before comparison.
