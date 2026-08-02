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
