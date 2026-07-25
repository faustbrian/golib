# Performance

Compile once and reuse plans. Construct one immutable context per fact
snapshot. Avoid resolver calls on hot paths by supplying known facts directly.
Use canonical hashes and a bounded plan cache for stored definitions.

The local benchmark fixture evaluates three equivalent country, numeric, and
collection propositions. On an Apple M4 Max with Go 1.26.5, the initial
100-millisecond smoke run measured approximately 703 ns/op and 920 B/op for
compilation, and 621 ns/op and 720 B/op for evaluation. These numbers are a
development baseline, not a cross-machine guarantee.

Run `make benchmark BENCH_TIME=1s` for stable local numbers. The separate
competitor benchmark pins maintained engines and reports setup boundaries so
unlike work is not presented as equivalent.

The isolated [`benchmarks/competitors`](../benchmarks/competitors) module pins
Expr 1.17.8 and Grule 1.20.6. All three engines evaluate the same compiled
country-and-weight boolean decision. Parsing and compilation happen outside
the timed loop. Grule's timed loop resets its retracted rule state, which is
required for repeated equivalent evaluation; the other engines reuse their
immutable compiled program directly. This benchmark compares execution cost,
not the projects' broader feature sets or security models.
