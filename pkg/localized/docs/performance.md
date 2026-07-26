# Performance guide

Use exact lookup for hot paths: it is map-backed and currently reports zero
allocations in the smoke benchmark. Shared `Text` and `Plan` values require no
locks for concurrent reads.

Matching constructs an `x/text` matcher per call and is intentionally more
expensive. Cache an immutable application plan outside this package only when
measurement justifies it; do not introduce a mutable global cache. Fallback
plans themselves are reusable and current exact graph resolution reports zero
allocations.

Construction, merge, and decoding copy data to preserve ownership. Canonical
JSON also escapes every key and string deterministically. These allocations are
part of the immutability contract.

Run:

```sh
make benchmark BENCH_TIME=1s
```

Benchmarks report allocations for construction, exact lookup, matching,
fallback, merge, maximum-size locale sets, encoding, and decoding. Treat results
as machine-specific baselines; investigate regressions using repeated runs and
statistical comparison rather than a single number.

`TestAllocationBudgets` enforces generous regression ceilings under the pinned
Go toolchain. Exact lookup and exact fallback permit no allocations;
construction permits 16, matching 256, merge 24, canonical JSON encoding 128,
and canonical JSON decoding 384 allocations per operation. The ceilings are
guardrails against amplification, not optimization targets. A deliberate
toolchain or dependency update MAY revise them only with recorded benchmark
evidence and changelog classification.
