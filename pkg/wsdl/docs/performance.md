# Performance

`make benchmark` records parse, marshal, and compile allocations and latency in
`benchmark.txt`. Benchmarks use a fixed valid description and fail if a named
benchmark disappears. They are correctness-gated by the normal test suite.

The project does not promise machine-independent nanosecond thresholds.
Release review compares results on the same runner and investigates material
regressions alongside allocation changes. Security limits take precedence over
throughput.
