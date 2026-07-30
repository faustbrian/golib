# Performance evidence

Performance evidence is organized by observable Kafka contract. A result is
comparable only when acknowledgement, idempotence, ordering, partition count,
keying, compression, payload, retry, timeout, and commit behavior are aligned.
Unsupported guarantees are exclusions, not faster results.

## Current equivalent client captures

The independently versioned
[`benchmarks/clients`](../benchmarks/clients) module isolates comparison
dependencies from the production module. Its ranked workloads measure one
warmed producer sending either one synchronous record, one synchronous
10/100-record batch, one bounded 10/100-record asynchronous window to one
partition, or one 80-record keyed or explicitly partitioned batch across eight
partitions until all in-sync replicas acknowledge the complete operation.
Idempotence and ordering are enabled for every ranked client. Topic creation,
client construction, metadata warm-up, fixture startup, and shutdown are
outside the timer.

The capture covers:

- the package policy over franz-go v1.21.5;
- raw franz-go v1.21.5 as the policy-overhead floor;
- IBM/Sarama v1.60.1 with idempotence, all-ISR acknowledgements, and its
  required single in-flight request;
- keyed and explicitly allowed unkeyed records;
- 128-byte, 1 KiB, and 64 KiB single-record payloads;
- 128-byte and 1 KiB payloads in 10-record and 100-record batches;
- 128-byte and 1 KiB payloads in bounded 10-record and 100-record asynchronous
  windows;
- balanced Murmur2-keyed and explicitly assigned 80-record batches across
  eight partitions with 128-byte and 1 KiB payloads; and
- uncompressed and Snappy-compressed workloads.

Kafka-go v0.4.51 participates in the real-broker correctness check as an
explicitly unranked control. Its `Writer` provides all-ISR acknowledgements but
does not expose an idempotent-producer mode, so placing its producer latency in
the durable ranking would compare different delivery contracts.

Separate correctness checks independently read the exact key and value
published by each client's single, batch, and asynchronous APIs in input order.
The keyed and explicit multi-partition check reads each partition independently
and proves exact order within that partition.
The timed single-record capture recorded 20 samples of 50 acknowledged records
for each of 36 ranked workload/client combinations: 720 benchmark samples and
36,000 timed deliveries. The batch capture recorded 20 samples of 50
acknowledged batches for each of 48 ranked combinations: 960 benchmark samples,
48,000 timed batch operations, and 2,640,000 timed records.
The asynchronous capture uses the same 48 combinations, 48,000 timed bounded
windows, and 2,640,000 timed records.
The multi-partition capture has 24 ranked combinations, 24,000 timed batches,
and 1,920,000 timed records distributed evenly across eight partitions.

The same harness separately measures one stable single-partition consumer-group
member with automatic commits disabled. Record mode invokes one handler and
synchronously commits one record; batch mode presents 10 or 100 contiguous
records to one application batch handler and synchronously commits the last
offset.
Handler success precedes every commit. Group join, input production, fixture
growth, construction, and shutdown are outside the timer. Fetch delivery,
public record mapping, handler invocation, and commit are inside it.

The consumer capture covers the package policy, raw franz-go, kafka-go, and
Sarama across 128-byte and 1 KiB payloads produced without compression or with
Snappy. It recorded 20 samples of 10 operations for each of 48 combinations:
960 benchmark samples, 9,600 timed operations, and 355,200 timed records.
Independent correctness checks prove exact record and batch handler order plus
the final committed broker offset for every client. One member and one
partition produce the same assignment outcome, but kafka-go uses range
assignment while the other clients use cooperative-sticky; rebalance behavior
is therefore outside this comparison. Sarama's synchronous commit method
returns no error, so its healthy-broker timing is backed by the external offset
check but does not prove equivalent commit-failure reporting.

A separate eight-partition workload compares the package policy and raw
franz-go because both expose the same bounded poll-and-commit cycle. One
operation handles one record per partition with 256 SHA-256 rounds of fixed
application work per record, either sequentially or with concurrency bounded
at eight while preserving per-partition order. Automatic commits are disabled
and every non-empty poll is synchronously committed. Kafka may split one
logical operation across one to eight polls, so the benchmark reports the
observed commit count; this capture observed exactly one commit per operation.
Kafka-go and Sarama remain covered by the equivalent single-partition workload
but are excluded here because their group APIs do not expose the same bounded
multi-partition poll-and-commit boundary.

The cross-partition capture recorded 20 samples of 10 operations for each of 16
combinations: 320 samples, 3,200 timed operations, and 25,600 timed records.
Independent correctness checks prove offsets `0`, `1`, and `2` in each
partition and final committed offset `3`. Observable synchronization separately
proves the raw comparison runner's bounded overlap; the policy module's
concurrency suite proves its worker bound and per-partition serialization.

## Environment and interpretation

The 2026-07-30 capture used Go 1.26.5 on Darwin arm64 with an Apple M4 Max,
Docker Desktop engine 29.6.2, and the immutable Confluent Local 7.5.0 fixture.
The running broker reported `7.5.0-ccs`. Exact module versions, input hashes,
raw samples, and benchstat distributions are stored with the
[capture](performance-results/2026-07-30/README.md).

The local single-node broker shares CPU and networking with the benchmark
process. In the refreshed single-record capture, observed median end-to-end
latency ranged from 275 microseconds to 1.14 milliseconds for the policy path,
239 microseconds to 1.01 milliseconds for raw franz-go, and 6.5 to 7.7
milliseconds for Sarama. Batch-operation medians ranged from 204 to 626
microseconds for the policy path, 195 to 883 microseconds for raw franz-go, and
6.4 to 8.6 milliseconds for Sarama. Bounded asynchronous-window medians ranged
from 6.14 to 7.15 milliseconds for the policy path, 6.16 to 7.88 milliseconds
for raw franz-go, and 6.23 to 7.29 milliseconds for Sarama. Eight-partition
batch medians ranged from 0.89 to 2.77 milliseconds for the
policy path, 0.82 to 3.04 milliseconds for raw franz-go, and 7.04 to 9.17
milliseconds for Sarama. Individual distributions spread as far as 184 percent
in the earlier synchronous captures, 7 percent for asynchronous latency, and
38 percent for multi-partition latency on the shared local fixture. Those
ranges describe local fixture noise as well as client work; they do not
establish superiority or a stable production budget.

Consumer record-operation medians ranged from 0.49 to 5.70 milliseconds for
the policy path, 0.38 to 3.65 milliseconds for raw franz-go, 0.46 to 2.06
milliseconds for kafka-go, and 82.7 to 97.8 milliseconds for Sarama. Consumer
batch-operation medians ranged from 0.28 to 3.04 milliseconds for the policy
path, 0.27 to 0.46 milliseconds for raw franz-go, 0.29 to 0.65 milliseconds
for kafka-go, and 0.37 to 0.88 milliseconds for Sarama. Consumer latency
distributions spread as far as 320 percent on the shared fixture. These
distributions are descriptive evidence, not a stable production budget or a
superiority claim.

Cross-partition sequential-operation medians ranged from 1.56 to 2.95
milliseconds for the policy path and 1.00 to 2.12 milliseconds for raw
franz-go. Bounded-parallel medians ranged from 0.86 to 1.15 milliseconds for
the policy path and 0.66 to 1.10 milliseconds for raw franz-go. Distributions
spread as far as 66 percent, so these results likewise describe the exact
shared local fixture rather than a production budget or client ranking.

Allocations are reported but include client serialization and network request
handling. The policy path intentionally owns caller bytes before admission, so
its allocation delta from raw franz-go is part of the current public ownership
contract. Existing package microbenchmarks isolate individual validation,
failure-policy, replay-progress, worker, and inspection operations, but a
complete end-to-end policy-overhead decomposition remains outstanding.

## Remaining benchmark matrix

Release evidence still requires equivalent and reproducible captures for:

- rebalance cost under multi-member consumer-group changes;
- producer transactions and consume-transform-produce;
- replay and inspection operations;
- reconnect allocations plus idle CPU, memory, goroutines, and connections;
- TLS and other deployment-representative transport costs; and
- a previous released package version after one exists.

Future runs must retain raw samples and environment fingerprints, report
variance, and avoid CI latency thresholds until a controlled runner can support
a justified budget.
