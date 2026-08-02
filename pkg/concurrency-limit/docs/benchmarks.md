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

`TestVegasSimulationConvergesAndRecoversWithReproducibleWorkloads` publishes a
deterministic capacity-rise, collapse, and recovery model.
`TestAlgorithmsRemainDeterministicAcrossNoisyWorkloadClasses` covers constant,
bursty, bimodal, heavy-tail, periodic, and sparse traces. Fuzzing extends this
with arbitrary bounded histories. These are control-model proofs, not claims
about every production workload; replay representative service traces before
changing defaults.

The conservative Vegas default was selected because its equation is
explainable, its queue target detects latency growth before widespread timeout,
its application-limited guard prevents idle drift, and the throughput signal
prevents growth when added in-flight work no longer increases goodput.
