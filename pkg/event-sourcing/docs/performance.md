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

The isolated [competitor harness](../benchmarks/competitors/README.md) compares
equivalent record-and-apply behavior and public aggregate reconstruction at 1,
10, 100, and 1,000 events against current pinned releases of EventHorizon,
Hallgren Eventsourcing, and TheFabric Eventsourcing. It documents
implementation-required decoding, iteration, and state-copying costs instead of
normalizing them away, and it does not put those dependencies in the core
module graph.

The PostgreSQL adapter's
[`BenchmarkPostgreSQLAppendEquivalentWork`](../postgres/append_benchmark_integration_test.go)
compares one durable append through the public event store with direct `pgx`
application code. Both paths use the same validated envelope and perform one
transaction, stream creation and lock, global-position allocation, message
insert, stream-head update, and commit. The direct path remains a cost floor;
it does not make the event store's reusable validation and error guarantees.
The PostgreSQL harness separately times typed not-committed rejection of a
stale exact version and verifies that the rejected workload changes no rows.

The core benchmark suite separately measures lifecycle reconstitution at 0, 1,
10, 100, 1,000, and 10,000 events; small, normal, maximum, and rejected hostile
payloads; warm and cold JSON codec registries; synchronous dispatch batches;
and determinism-checked upcaster chains with no rule and depths 1, 4, and 16.
Fixture construction is outside the timer except for the deliberately cold
registry case. Validation, defensive ownership, event application, and
deterministic upcaster double-execution remain inside because they are part of
the public guarantees.

Snapshot break-even benchmarks compare full replay with JSON state decoding
plus the remaining history after snapshots at 10%, 25%, 50%, 75%, and 90% of
total stream lengths 100, 1,000, and 10,000. They measure state-codec and
lifecycle work without storage I/O; PostgreSQL snapshot retrieval must be
measured separately on deployment-shaped data.

The projection benchmark measures bounded replay batches of 10, 100, and 1,000
messages through the public runner, including replay delivery construction,
one handler call, and one optimistic checkpoint save per message. Its reader,
handler, and checkpoint store are deterministic in-memory fixtures, so the
result isolates orchestration cost rather than durable read-model or checkpoint
I/O. A separately named live-catch-up workload resumes from an existing
1,000,000-message checkpoint and handles bounded tails of the same sizes.
Benchmark deployment-owned read-model and checkpoint transactions separately.

The in-memory concurrency benchmark compares workloads of 200 single-message
appends from eight writers across independent streams and one hot stream. Event
and message construction stays outside the timer; store construction, goroutine
coordination, append validation, defensive ownership, and global-position
assignment stay inside. Both shapes serialize global-position assignment in
the reference store. Independent streams require new-stream expectations; the
hot-stream fixture explicitly uses any-version appends to isolate lock and
growth contention without conflict retries. This is a contention comparison,
not an optimistic-concurrency or durable-throughput claim.

The optional gooutbox adapter owns a real PostgreSQL benchmark that compares
the same single-message append with and without one encoded outbox row in the
commit transaction. It reports adapter staging overhead separately from relay
publication and Kafka delivery.

The PostgreSQL benchmark suite also drives parallel independent-stream appends
through a pool restricted to two connections. It requires observed empty-pool
waits, reports waits and wait duration per operation, and verifies every
successful append durably. This measures client pool saturation plus the
database's global-position contention on the capture host.

## Reproducibility

The [performance evidence harness](../benchmarks/README.md) captures the core,
competitor, real PostgreSQL, and real outbox layers as separate raw files and
analyzes each with the dependency-pinned `benchstat` version.

Publish the Go version, exact module versions and checksums, hardware, operating
system, CPU and power mode, database image digest and settings, connection-pool
limits, schema, payload corpus, stream-length distribution, concurrency,
sample count, benchmark command, raw output, `benchstat` analysis, latency
distributions, throughput, and allocations.

Use repeated independent samples after warm-up. Report variance and confidence
intervals; do not publish only the fastest run. Keep functional correctness
tests separate and require them to pass before timing. The evidence harness
captures CPU, allocation, mutex, block, and GC profiles for representative core
and PostgreSQL paths. PostgreSQL client profiles expose database and network-I/O
wait from the Go process; pair them with server statistics when server-side
attribution is required.

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
