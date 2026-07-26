# Performance evidence

This directory owns reproducible capture and analysis for the event-sourcing
benchmark layers. The competitor module remains nested so comparison libraries
do not enter the core dependency graph.

Create an empty result directory, verify a quiet machine on external power,
then run:

```sh
mkdir benchmarks/results/YYYY-MM-DD-environment
make -C benchmarks test
make -C benchmarks test-integration POSTGRES_VERSION=18
make -C benchmarks capture \
  OUTPUT_DIR="$PWD/benchmarks/results/YYYY-MM-DD-environment" \
  COUNT=20 BENCH_TIME=250ms POSTGRES_VERSION=18
make -C benchmarks analyze \
  OUTPUT_DIR="$PWD/benchmarks/results/YYYY-MM-DD-environment"
make -C benchmarks environment \
  OUTPUT_DIR="$PWD/benchmarks/results/YYYY-MM-DD-environment" \
  POSTGRES_VERSION=18
```

`capture` keeps core, competitor, PostgreSQL, and outbox samples in separate
raw Go benchmark files. `analyze` uses the dependency-pinned `benchstat` tool.
The database benchmarks each start a fresh PostgreSQL container with the
module's migrations and default image configuration. They do not reuse a
database across result files.

Before publishing, add a result-specific README that records the exact command,
deliberate concurrent load, power mode, PostgreSQL settings, sample count,
duration, exclusions, and interpretation boundaries. Keep raw samples; do not
publish only summary values or select the fastest runs. Record the resolved
container image ID and repository digest from `environment.txt`.

These benchmark results are comparative evidence for the named workloads, not
service-level objectives. They do not predict application latency, production
database contention, relay throughput, Kafka delivery, or recovery duration.
