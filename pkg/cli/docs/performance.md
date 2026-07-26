# Performance

Correctness, stable semantics, startup safety, and bounded work outrank headline
benchmarks. Comparative benchmarks measure graph construction and prepared
dispatch separately. Dispatch uses the same tree, argv, typed validation, JSON
encoding, and discarded writer for `cli`, Cobra, `urfave/cli`, Kong, and
standard `flag` where their behavior is equivalent.

The root benchmark suite covers small, broad, deep, and maximum construction;
root and deep dispatch; typed conversion; help; completion; manifest
generation; JSON output; success; usage and validation errors; suggestions;
cancellation; and repeated in-process allocation behavior. Comparative output
is encoded during every iteration and sent to `io.Discard`; validation and
construction are never omitted from only one implementation.

Prepared `cli` dispatch builds fresh internal parser state to preserve
concurrent and repeated invocation isolation. Direct Cobra dispatch reuses its
mutable prepared graph. The release budget permits up to four times that Cobra
latency and 100 allocations for this exact fixture; this is an explicit cost of
the stronger isolation contract, not a universal performance claim. Standard
`flag` remains a parsing floor because it has no command graph.

Run:

```sh
make benchmark
make benchmark-compare
```

Checked-in evidence records Go version, operating system, architecture, commit,
fixture hash, benchmark command, raw results, and statistical comparison.
Results describe that fixture and machine only; no universal speed claim is
made. A materially safer or better-maintained engine, or a proven regression at
the adapter boundary, can reopen the parser decision.

Current checked-in evidence: [2026-07-22 Darwin arm64](benchmarks/2026-07-22-darwin-arm64.md).
