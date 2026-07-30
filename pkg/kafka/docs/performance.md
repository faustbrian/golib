# Performance evidence

Performance evidence is organized by observable Kafka contract. A result is
comparable only when acknowledgement, idempotence, ordering, partition count,
keying, compression, payload, retry, timeout, and commit behavior are aligned.
Unsupported guarantees are exclusions, not faster results.

## Current equivalent producer capture

The independently versioned
[`benchmarks/clients`](../benchmarks/clients) module isolates comparison
dependencies from the production module. Its current ranked workload measures
one warmed synchronous producer sending one record at a time to one
pre-created partition until all in-sync replicas acknowledge it. Idempotence
and ordering are enabled for every ranked client. Topic creation, client
construction, metadata warm-up, fixture startup, and shutdown are outside the
timer.

The capture covers:

- the package policy over franz-go v1.21.5;
- raw franz-go v1.21.5 as the policy-overhead floor;
- IBM/Sarama v1.60.1 with idempotence, all-ISR acknowledgements, and its
  required single in-flight request;
- keyed and explicitly allowed unkeyed records;
- 128-byte, 1 KiB, and 64 KiB payloads; and
- uncompressed and Snappy-compressed batches.

Kafka-go v0.4.51 participates in the real-broker correctness check as an
explicitly unranked control. Its `Writer` provides all-ISR acknowledgements but
does not expose an idempotent-producer mode, so placing its producer latency in
the durable ranking would compare different delivery contracts.

The correctness check independently read the exact key and value published by
each client. The timed capture recorded 20 samples of 50 acknowledged records
for each of 36 ranked workload/client combinations: 720 benchmark samples and
36,000 timed deliveries in total.

## Environment and interpretation

The 2026-07-30 capture used Go 1.26.5 on Darwin arm64 with an Apple M4 Max,
Docker Desktop engine 29.6.2, and the immutable Confluent Local 7.5.0 fixture.
The running broker reported `7.5.0-ccs`. Exact module versions, input hashes,
raw samples, and benchstat distributions are stored with the
[capture](performance-results/2026-07-30/README.md).

The local single-node broker shares CPU and networking with the benchmark
process. Observed median end-to-end latency ranged from 198 to 730 microseconds
for the policy path, 173 to 491 microseconds for raw franz-go, and 6.3 to 7.3
milliseconds for Sarama across the matrix. One franz-based case retained 239
percent distribution spread even after the extended
capture. Those ranges describe local fixture noise as well as client work;
they do not establish superiority or a stable production budget.

Allocations are reported but include client serialization and network request
handling. The policy path intentionally owns caller bytes before admission, so
its allocation delta from raw franz-go is part of the current public ownership
contract. Existing package microbenchmarks isolate individual validation,
failure-policy, replay-progress, worker, and inspection operations, but a
complete end-to-end policy-overhead decomposition remains outstanding.

## Remaining benchmark matrix

Release evidence still requires equivalent and reproducible captures for:

- synchronous batches and bounded asynchronous production;
- multiple partitions and partition-distribution behavior;
- consumer record and batch handling, including kafka-go;
- sequential and cross-partition handling, commits, and rebalance cost;
- producer transactions and consume-transform-produce;
- replay and inspection operations;
- reconnect allocations plus idle CPU, memory, goroutines, and connections;
- TLS and other deployment-representative transport costs; and
- a previous released package version after one exists.

Future runs must retain raw samples and environment fingerprints, report
variance, and avoid CI latency thresholds until a controlled runner can support
a justified budget.
