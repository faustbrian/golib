# Performance

Run allocation benchmarks with:

```sh
make benchmark BENCH_TIME=100ms
```

The suite measures complete-document parse, semantic validation, canonical
serialization, reference resolve, bundle collection, semantic diff, discovery,
large-schema compilation, and hostile bounded rejection. Use multiple runs and
`benchstat` before claiming a regression or improvement.

The checked [benchmark baseline](../benchmarks/baseline.txt) records one
reproducible evidence run. It is a comparison input, not a portable latency
claim; release comparisons must use identical hardware, Go, and benchmark
settings.

Resource policies are part of performance correctness. Lower limits for known
small documents. Reuse immutable parsed documents and compiled validators where
their policy and resource set are identical. Discovery caching is opt-in and
must be invalidated explicitly.

Schema compilation accounts for the root and every explicit resource before
registration. Lower `MaxResources` and `MaxSchemaBytes` when applications use a
small fixed schema graph.

Do not optimize by dropping duplicate-key detection, exact numeric lexemes,
ownership copies, diagnostics bounds, reference authorization, cancellation,
or schema semantics. Record baseline hardware, Go version, command, and
allocations when proposing a performance budget.
