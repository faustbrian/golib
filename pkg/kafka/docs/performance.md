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

A separate cooperative-sticky rebalance workload compares the package policy,
raw franz-go, and Sarama across two partitions. One stable member remains in
the group while each operation constructs a second public client, joins it,
handles and commits one record, and waits for broker inspection to prove a
stable one-partition-per-member assignment. It then closes that client and
waits for the first member to stably regain both partitions. Topic creation,
input production, first-member setup, and final shutdown are excluded. Kafka-go
v0.4.51 is excluded because it does not expose cooperative-sticky assignment.
The capture contains 30 single-operation samples with independently proven
initial, joined, handled, left, and restored outcomes.

A verified-TLS workload compares the package policy, raw franz-go, and Sarama
against a separately pinned Apache Kafka 4.3.1 broker that accepts only TLS 1.3.
The persistent path warms one idempotent all-ISR producer, then measures
128-byte and 1 KiB keyed deliveries. The connection path measures complete
public client construction, verified connection, one 128-byte keyed delivery,
and bounded shutdown. Ten samples of 100 operations produce 6,000 persistent
deliveries and 3,000 connection lifecycles. An independent race-enabled check
asserts TLS 1.3 and reads every exact result through a separate verified client.
Kafka-go is excluded because it cannot match the idempotent producer contract.

A producer-only transaction workload compares the package policy, raw
franz-go, and Sarama. One operation begins a transaction, synchronously
publishes one or ten keyed records through the candidate's public transaction
surface, and commits. Kafka-go is excluded because its writer does not expose
Kafka transactions. The capture spans 128-byte and 1 KiB payloads with no
compression and Snappy: 20 samples of 10 operations for each of 24
combinations, totaling 480 samples, 4,800 committed transactions, and 26,400
records. Independent read-committed and read-uncommitted checks prove exact
committed visibility and aborted-record invisibility for every ranked client.

A consume-transform-produce workload compares the package policy and raw
franz-go `GroupTransactSession`. One operation polls one or ten read-committed
source records, copies and deterministically transforms them, publishes one
equally sized output per source record, and atomically commits the source
offsets and outputs. Kafka-go and Sarama are excluded because their public
group APIs do not expose the same bounded poll-and-transaction boundary. The
capture spans the same payload and compression matrix: 320 samples, 3,200
logical operations, and 17,600 source plus 17,600 output records. Every sample
completed in one Kafka transaction. Independent checks prove exact transformed
output, source-offset advancement, abort invisibility, unchanged offsets after
abort, and successful redelivery.

A direct-partition replay workload compares the package policy, raw franz-go,
kafka-go, and Sarama. Each operation constructs the public client resources,
validates an exact inclusive-start/exclusive-end range against broker bounds,
handles every requested record in ascending offset order, and closes all
resources without joining or mutating a consumer group. The matrix spans 10
and 100 records, 128-byte and 1 KiB payloads, and no compression plus Snappy:
640 independent samples, 640 complete operations, and 35,200 handled records.
Construction and shutdown remain inside the timer because the policy replay
reader is intentionally single-use. An independent check proves exact offsets,
keys, and values for `[1,3)` across all four clients.

A read-only topic inspection workload compares one stable policy, raw
franz-go, kafka-go, or Sarama client. Each operation issues metadata,
beginning-offset, end-offset, and topic-configuration requests and normalizes
the same leader epoch, replica/ISR/offline state, offsets, and durability,
retention, compaction, segment, and unclean-election policy. The one- and
eight-partition matrix contains 160 samples, 1,600 complete inspections, and
7,200 normalized partition states. Independent checks prove exact four-client
agreement for a three-partition topic.

A restart workload uses the same four stable inspectors against a dedicated
fixed-endpoint broker. Each sample proves a complete inspection fails under a
two-second context while the broker is stopped, restarts the broker without
changing its endpoint, then measures the post-readiness operation until the
client recovers the exact pre-failure three-partition state. Ten samples per
client report latency, allocation count, and allocated bytes. A separate
500-millisecond no-request workload reports ten garbage-collected point samples
per client for retained heap, heap objects, goroutines, active and verified
closed connections, and Go runtime user, GC, and scavenger CPU; every sample
proves shutdown returns all observed connections to zero.

## Environment and interpretation

The 2026-07-30 producer, consumer, and transaction capture and the 2026-07-31
replay, inspection, rebalance, reconnect, and resource captures used Go 1.26.5
on Darwin arm64 with an Apple M4 Max, Docker Desktop engine 29.6.2, and the
immutable Confluent Local 7.5.0 fixture. The running broker reported
`7.5.0-ccs`. The TLS capture used the same host and toolchain with the immutable
Apache Kafka 4.3.1 fixture; runtime checks asserted Kafka 4.3.1, OpenSSL 3.5.7,
and TLS 1.3. Exact module versions, input hashes, raw samples, and benchstat
distributions are stored with the
[2026-07-30 capture](performance-results/2026-07-30/README.md) and
[2026-07-31 capture](performance-results/2026-07-31/README.md).

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

Complete cooperative-sticky rebalance medians were 1.516 seconds for the policy
path, 1.509 seconds for raw franz-go, and 2.189 seconds for Sarama.
Construction-through-commit-and-stability medians were 1.010, 1.004, and 1.132
seconds respectively; close-through-one-member-stability medians were 506.1
milliseconds, 507.7 milliseconds, and 1.053 seconds. Bounded broker inspection
is part of these observable stability boundaries, so they are not protocol-only
wire timings.

Persistent verified-TLS medians for 128-byte records were 458.9 microseconds
for the policy path, 328.0 microseconds for raw franz-go, and 7.051 milliseconds
for Sarama; 1 KiB medians were 289.5 microseconds, 290.0 microseconds, and 6.852
milliseconds. Complete construction, TLS connection, 128-byte delivery, and
shutdown medians were 16.38 milliseconds, 16.08 milliseconds, and 13.41
milliseconds. Persistent distributions spread as far as 99 percent on the
shared local host, so they are descriptive evidence rather than client rankings
or production budgets.

Producer transaction medians for one record ranged from 6.37 to 25.92
milliseconds for the policy path, 6.63 to 24.07 milliseconds for raw
franz-go, and 8.24 to 8.62 milliseconds for Sarama. Ten-record transaction
medians ranged from 17.75 to 26.09 milliseconds for the policy path, 16.61 to
24.27 milliseconds for raw franz-go, and 70.89 to 73.26 milliseconds for
Sarama. Consume-transform-produce medians ranged from 2.00 to 7.11
milliseconds for one-record policy operations and 2.12 to 5.53 milliseconds
for raw franz-go; ten-record medians ranged from 7.14 to 9.47 milliseconds for
the policy path and 5.53 to 8.71 milliseconds for raw franz-go. Transaction
latency distributions spread as far as 76 percent. These healthy
single-broker results do not rank abort, fencing, timeout, unknown-outcome, or
rebalance behavior.

Complete replay-operation medians ranged from 7.855 to 30.53 milliseconds for
the policy path, 9.706 to 31.90 milliseconds for raw franz-go, 391.9 to 432.0
milliseconds for kafka-go, and 613.9 to 635.1 milliseconds for Sarama. The
kafka-go and Sarama measurements include their public reader shutdown
lifecycle. Policy distributions spread as far as 45 percent and raw franz-go
as far as 44 percent.

Complete inspection medians ranged from 3.472 to 5.330 milliseconds for the
policy path, 3.005 to 4.309 milliseconds for raw franz-go, 4.505 to 4.810
milliseconds for kafka-go, and 4.960 to 6.383 milliseconds for Sarama. One
policy distribution spans 170 percent. These shared-fixture results remain
descriptive evidence rather than stable production budgets or superiority
claims.

Post-restart inspection medians were 19.66 seconds for the policy path, 19.53
seconds for raw franz-go, 19.27 seconds for kafka-go, and 19.42 seconds for
Sarama. Median reconnect allocations were 10.87k, 11.33k, 39.06k, and 16.59k
respectively; median allocated bytes were 593.0 KiB, 606.2 KiB, 4.292 MiB, and
6.076 MiB. The distributions span 12 to 16 percent. Broker downtime, the
deliberate failed request, and Docker lifecycle work are outside these values;
client retry/backoff, connection initialization, four protocol operations, and
normalized state recovery are inside them.

Idle point samples reported two active and verified closed connections, three
goroutines, 30.03 KiB, and 167.5 heap objects for the policy; two connections,
three goroutines, 29.20 KiB, and 152 objects for raw franz-go; four
connections, five goroutines, 164.6 KiB, and 140 objects for kafka-go; and one
connection, three goroutines, 154.6 KiB, and 258.5 objects for Sarama. Direct
Go runtime user, GC, and scavenger counters reported no observable CPU in
these intervals. These process-level observations remain descriptive rather
than production budgets.

Allocations are reported but include client serialization and network request
handling. The policy path intentionally owns caller bytes before admission, so
its allocation delta from raw franz-go is part of the current public ownership
contract. Existing package microbenchmarks isolate individual validation,
failure-policy, replay-progress, worker, and inspection operations, but a
complete end-to-end policy-overhead decomposition remains outstanding.

## Remaining benchmark matrix

Release evidence still requires equivalent and reproducible captures for:

- mTLS, SASL, and other deployment-representative authentication costs; and
- a previous released package version after one exists.

Future runs must retain raw samples and environment fingerprints, report
variance, and avoid CI latency thresholds until a controlled runner can support
a justified budget.
