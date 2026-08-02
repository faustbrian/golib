# Benchmarks and simulation evidence

`BenchmarkAcquireComplete` measures the complete owned admission path.
`BenchmarkAlgorithmWindows` compares fixed, AIMD, Vegas-style, and Gradient2
with the same window semantics, CPU, Go toolchain, and benchmark duration. Run:

```sh
make benchmark BENCH_TIME=5s
```

The report includes latency and allocations. Record the CPU, OS, architecture,
Go version, command, duration, and raw output when publishing results; do not
compare results from unlike semantics or environments.

`TestEveryAlgorithmUpdateMatchesDeterministicReference` compares all 512 AIMD,
Vegas, and Gradient2 updates against independent reference equations using the
fixed seed `0x5eed`. `TestSeededWorkloadCampaignsRemainBoundedAndDeterministic`
uses seeds 101 through 109 for constant, bursty, ramp, bimodal, heavy-tail,
periodic, sparse, capacity-collapse, and workload-class-shift traces. It records
limit bounds, goodput, rejection, modeled p90 latency, finite state, recovery,
and adaptation time and proves identical results on a repeated run.

The workload campaigns are per-window control models with fixed modeled
capacity and baseline latency, not end-to-end production measurements or
claims about every deployment. `TestVegasSimulationConvergesAndRecoversWithReproducibleWorkloads`
and `TestAlgorithmsRemainDeterministicAcrossNoisyWorkloadClasses` provide
additional deterministic controls, while fuzzing extends them with arbitrary
bounded histories. Replay representative service traces before changing
defaults.

The conservative Vegas default was selected because its equation is
explainable, its queue target detects latency growth before widespread timeout,
its application-limited guard prevents idle drift, and the throughput signal
prevents growth when added in-flight work no longer increases goodput.

## Comparative evidence

The non-releasable [comparison harness](../benchmarks/comparison/README.md)
pins Netflix `concurrency-limits`, Failsafe-Go, and Platinum sources separately
from this public module. Its checked-in [results](../benchmarks/comparison/results/README.md)
include all nine fixed-seed workload classes, every limit and modeled capacity,
per-workload SVG convergence plots, utilization, goodput, rejection, modeled queue and p99
latency, collapse/recovery adaptation, and raw five-run CPU, bytes, and
allocation benchmarks.

The normalized report uses one successful aggregate RTT observation per
candidate and excludes implementation-specific overload/drop signals. Netflix
is a transparent Go equation port rather than a JVM benchmark, and Failsafe-Go
still owns wall-clock sampling. Treat the results as bounded implementation
evidence, not a universal performance ranking.
