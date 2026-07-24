# Benchmark baseline

This is a local comparison point, not a CI latency budget or a claim of equal
feature coverage.

- Date: 2026-07-19
- Go: 1.26.5
- OS/architecture: macOS, darwin/arm64
- CPU: Apple M4 Max
- Command: `make benchmark BENCH_TIME=50ms`

| Workload | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Parse schema | 8,370 | 11,041 | 127 |
| Compile schema | 10,033 | 16,408 | 165 |
| Validate instance | 2,918 | 7,960 | 66 |
| Marshal schema | 2,822 | 7,896 | 37 |

Each benchmark runs a correctness check before timing. CI records new raw
results as artifacts but does not apply a timing threshold until repeated
runner measurements establish variance and an evidence-based budget.

`make benchmark` also runs compile and validation workloads through the JDK
JAXP reference engine. The comparison uses the same files under
`testdata/benchmark`; JAXP must accept `valid.xml` and reject `invalid.xml`
before timing. Local and CI execution use the same digest-pinned Eclipse
Temurin 25 container, while raw output records the exact Java runtime.
