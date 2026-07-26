# Performance and capacity planning

Performance evidence is valid only when implementations perform equivalent
observable work. Removing validation, serialization, optimistic conflict
checks, transaction durability, dispatch, or acknowledgement from one side
invalidates a comparison.

## Benchmark layers

Measure and report these layers separately:

1. aggregate event recording and immediate application;
2. aggregate reconstitution at representative stream lengths;
3. payload and envelope serialization plus upcasting;
4. in-memory append and reads;
5. PostgreSQL append, conflict, and bounded-read behavior;
6. snapshot restoration and the measured break-even point;
7. projection replay and checkpoint persistence; and
8. optional outbox staging and publication overhead.

The isolated [competitor harness](../benchmarks/competitors/README.md) begins
with equivalent record-and-apply behavior against current pinned releases of
EventHorizon, Hallgren Eventsourcing, and TheFabric Eventsourcing. It does not
put those dependencies in the core module graph.

The PostgreSQL adapter's
[`BenchmarkPostgreSQLAppendEquivalentWork`](../postgres/append_benchmark_integration_test.go)
compares one durable append through the public event store with direct `pgx`
application code. Both paths use the same validated envelope and perform one
transaction, stream creation and lock, global-position allocation, message
insert, stream-head update, and commit. The direct path remains a cost floor;
it does not make the event store's reusable validation and error guarantees.

The core benchmark suite separately measures lifecycle reconstitution at 10,
100, 1,000, and 10,000 events, deterministic JSON payload round trips, and
determinism-checked upcaster chains at depths 1, 4, and 16. Fixture construction
is outside the timer, while validation, defensive ownership, event application,
and deterministic upcaster double-execution remain inside because they are part
of the public guarantees.

Snapshot break-even benchmarks compare full replay with JSON state decoding
plus the remaining history after snapshots at 10%, 25%, 50%, 75%, and 90% of
total stream lengths 100, 1,000, and 10,000. They measure state-codec and
lifecycle work without storage I/O; PostgreSQL snapshot retrieval must be
measured separately on deployment-shaped data.

## Reproducibility

Publish the Go version, exact module versions and checksums, hardware, operating
system, CPU and power mode, database image digest and settings, connection-pool
limits, schema, payload corpus, stream-length distribution, concurrency,
sample count, benchmark command, raw output, `benchstat` analysis, latency
distributions, throughput, and allocations.

Use repeated independent samples after warm-up. Report variance and confidence
intervals; do not publish only the fastest run. Keep functional correctness
tests separate and require them to pass before timing. Capture CPU, allocation,
mutex, block, database, and I/O profiles when a regression needs explanation.

## Capacity decisions

Measure the median, tail, and maximum stream length plus the number of active
and hot streams. A hot aggregate is serialized by its stream version even when
independent aggregates remain parallel. The PostgreSQL global-position
allocator is another deliberate serialization point and must be included in
write-capacity tests.

Snapshots can reduce reconstitution cost but do not reduce append contention.
Choose a threshold only after measuring full replay and snapshot-plus-tail with
the application's real state codec and event distribution. Partitioning,
archive, retention, pool sizing, and projection batch sizes likewise require a
production-shaped corpus and recovery drill.

No benchmark result is a service-level objective. Deployments own latency and
throughput budgets, workload forecasts, headroom, alerting, and regression
policy on stable infrastructure.
