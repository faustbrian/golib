# Performance methodology

Correctness, exact numbers, deterministic output, cancellation, and bounded
behavior take priority over benchmark scores. Compile once and reuse `Schema`
concurrently; compilation includes meta-schema validation and resource
indexing, while validation includes exact JSON parsing.

Run reproducible local benchmarks with:

```sh
go test -run '^$' -bench . -benchmem ./...
```

The checked-in [pre-v1 baseline](../benchmarks/baseline.txt) records the
machine, toolchain, command, and raw results for representative compile,
validation, reference, and adversarial scaling cases. `make benchmark`
provides the short local regression run; release evidence should use multiple
longer samples and preserve the raw output.

`make benchmark-comparison BENCH_TIME=1s` runs a separate pinned benchmark
module against `kaptinlin/jsonschema` v0.9.3 and
`santhosh-tekuri/jsonschema` v6.0.2. It measures Draft 2020-12 compilation and
precompiled validation of the same decoded value. The compile result is
context, not a league table: this package performs official meta-schema
validation during compilation, while competitor compilation policies differ.
The separate module keeps benchmark-only dependencies out of the core module.
The checked-in [comparison baseline](../benchmarks/comparison/baseline.txt)
records the machine, toolchain, versions, command, and raw results.

Record toolchain, GOOS/GOARCH, CPU, commit, benchmark count, and limits with
published results. Measure compilation and validation separately for scalar,
object, array, reference, combinator, format, and adversarial scaling cases.
Track `ns/op`, `B/op`, allocations, and input-size scaling; use an external
process profiler when peak RSS matters.

Comparisons with other maintained Go validators must use equivalent dialect,
format, resolver, exact-number, and output policy. A faster result obtained by
skipping meta-validation, annotations, formats, or limits is not equivalent.
Regression budgets will be set only after stable multi-run baselines exist;
the pre-v1 repository does not invent percentage thresholds without evidence.

`uniqueItems` uses direct comparisons for small arrays and canonical SHA-256
buckets for larger arrays. Hash matches are always confirmed with exact JSON
equality, so a collision cannot change validation. Collision fallback and
distinct-item hashing both consume `MaxUniqueComparisons` work.
The checked-in [optimization evidence](../benchmarks/unique-items-optimization.txt)
records the before/after scaling deltas and raw benchmark configuration.
