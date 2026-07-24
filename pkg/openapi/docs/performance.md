# Performance and complexity evidence

This document defines the benchmark methodology and release regression policy.
The current raw run is
[`2026-07-22-darwin-arm64.txt`](benchmarks/2026-07-22-darwin-arm64.txt).
It records source revision `08dbc126c2b170ed988fae6309ca9d89ab26a98b`,
Go 1.26.5, Darwin arm64, and an Apple M4 Max.

## Method

`make benchmark-evidence
BENCHMARK_OUTPUT=docs/benchmarks/YYYY-MM-DD-GOOS-GOARCH.txt` requires a clean
tracked `openapi` tree and records the exact revision, UTC capture time,
command, Go version, operating system, architecture, and CPU. The capture uses
one logical Go processor, a 250 ms target per benchmark, three independent
samples, and `-benchmem`. A second one-iteration run under the platform
`/usr/bin/time` records peak process memory for the complete workload set.

The three samples expose short-run latency variability. `B/op` and `allocs/op`
measure total allocation work per operation, not live heap. The process-memory
probe includes the Go tool, test binary, runtime, fixtures, and the largest
one-iteration workload, so it is a conservative whole-process measurement and
not an attribution to one operation.

`make performance` runs every benchmark for 100 ms with one logical processor
and enforces named allocation-count ceilings. Allocation counts are used for
the blocking budget because wall-clock thresholds are unreliable across CI
hosts. Latency and bytes remain recorded evidence and must be reviewed when a
budget or implementation changes.

## Workloads

All corpora are deterministic and generated in `benchmark_test.go`; benchmarks
perform no internet access.

| Workload | Shape and assertion |
| --- | --- |
| JSON parse scaling | Equivalent valid OpenAPI 3.2 descriptions with 1, 100, and 1,000 paths; every parse must succeed |
| Invalid JSON | A truncated 100-path description; every parse must return `parse.ErrInvalidJSON` |
| Hostile depth | 257 nested arrays under default limits; rejection must be `parse.ErrLimitExceeded` |
| Validation | Warm and cold validation of a 100-path description, plus a 250-property schema-heavy description; every report must be valid |
| Serialization | Canonical bounded JSON for the 100-path description |
| References | Internal pointer lookup, bounded file and loopback HTTP retrieval, 100-reference bundling and dereferencing, and a two-node dereference cycle that must terminate with `ErrDereferenceCycle` |
| Composition and diff | A one-operation directional change, keep-all filtering, and merging two non-overlapping 50-path descriptions |
| Conversion | Forward and lossy paths across Swagger 2.0 and OpenAPI 3.0, 3.1, and 3.2 with asserted diagnostic counts |

The loopback HTTP case measures the complete resolver policy and HTTP stack but
not real network latency. File results include the local filesystem cache. Cold
validation constructs a new validator each iteration; warm validation reuses
one after an untimed priming validation.

## Current evidence

The 2026-07-22 run completed all samples and the peak-memory probe. The
one-iteration probe reported a peak memory footprint of 23,118,448 bytes.
The raw file remains authoritative for every latency, throughput, byte, and
allocation value rather than a rounded summary here.

The 1, 100, and 1,000 path parse samples provide an explicit scaling curve.
Invalid and over-depth cases demonstrate that rejection work is measured, not
only happy-path throughput. Reference-heavy, cyclic, schema-heavy, warm, cold,
filesystem, and loopback HTTP cases keep materially different costs separate.

## Regression policy

Allocation ceilings in `scripts/check-performance.sh` are intentionally above
the reviewed 2026-07-22 samples. A ceiling change requires:

1. a source-level explanation of the added work;
2. fresh raw evidence from a clean committed revision;
3. confirmation that parsing, validation depth, resolver state, limits,
   diagnostics, and outputs did not change merely to improve a number; and
4. a changelog entry when users can observe the regression or tradeoff.

The gate does not reward lower latency obtained by skipping validation,
diagnostics, limits, cancellation, or output. Benchmark functions assert their
semantic result so a narrowed operation cannot silently appear faster.

## Competitor comparisons

No competitor ranking is published. A future comparison with
`getkin/kin-openapi`, `pb33f/libopenapi`, or another implementation must pin
exact versions and licenses and match dialect, validation depth, reference
policy, resolver state, input and output limits, diagnostics, cold or warm
state, and successful result semantics. Until those conditions are met,
independent implementations are interoperability evidence only and their
numbers are not comparable to this suite.
