# Kafka client comparison benchmarks

This non-releasable module isolates comparison clients and container tooling
from the Kafka policy module. It records end-to-end broker measurements only
where the candidates can provide the same observable contract. It is not a
source of production support claims or a substitute for fault evidence.

## Pinned inputs

| Input | Version |
| --- | --- |
| `kafka` | current workspace module |
| `franz-go` | `v1.21.5` |
| `segmentio/kafka-go` | `v0.4.51` |
| `IBM/sarama` | `v1.60.1` |
| default broker fixture | Confluent Local `7.5.0` at immutable digest |
| testcontainers-go Kafka module | `v0.43.0` |

`go.sum` pins the module content checksums. Run `make environment` with every
capture to record the exact Go version, operating system, architecture,
workspace revision, Docker engine, client versions, broker image, and every
Go source, module, Makefile, and README harness input whether or not it is
staged yet. Generated benchmark and environment outputs are excluded.

## Equivalent producer workloads

`BenchmarkEquivalentSynchronousProduce` reuses one warmed producer and sends
one record at a time until the broker acknowledges all in-sync replicas.
`BenchmarkEquivalentSynchronousBatchProduce` submits 10 or 100 records through
each client's synchronous batch API and waits for the complete batch outcome.
`BenchmarkEquivalentAsynchronousProduce` admits a bounded window of 10 or 100
records through each client's asynchronous API and waits for every individual
delivery outcome before starting the next window. This keeps the maximum
application-owned outstanding work explicit while measuring asynchronous
admission, delivery callbacks, and result collection together.
`BenchmarkEquivalentMultiPartitionProduce` sends one 80-record synchronous
batch across eight pre-created partitions. The keyed mode selects ten keys per
partition with Kafka's Murmur2 mapping; the explicit mode assigns ten records
to each partition through every client's manual-partition API. Both modes wait
for the complete batch outcome.
Every ranked candidate has idempotence enabled, preserves order, disables topic
auto-creation, uses a pre-created topic with the workload's declared partition
count, retries at most ten times, and has bounded request waits, client channels,
and retry buffers. Client construction, metadata warm-up, topic creation,
fixture startup, and shutdown are outside the timer. Public record mapping,
client policy, serialization, compression, network transit, broker processing,
and delivery-result handling are inside it.

The single-record matrix covers keyed and explicitly accepted unkeyed records,
payloads of 128 bytes, 1 KiB, and 64 KiB, and no compression plus Snappy. The
batch matrix uses the same key and compression modes, 128-byte and 1 KiB
payloads, and 10-record plus 100-record batches. The one-partition fixture
deliberately removes partition-count and partitioner distribution as variables.
The asynchronous matrix uses the batch matrix's key, payload, compression, and
10/100-record dimensions but submits each record independently before awaiting
all results. The multi-partition matrix uses 128-byte and 1 KiB payloads with
no compression and Snappy. Automatic unkeyed multi-partition production is not
ranked: franz-go's adaptive KIP-794 partitioner and Sarama's random fallback do
not provide an equivalent distribution contract. The workloads do not
establish transaction, reconnect, TLS, or steady-state resource results; those
require separate evidence.

The policy library and raw franz-go use the same franz-go producer controls.
Sarama requires `Net.MaxOpenRequests=1` for idempotence; the one-partition
workload preserves ordering while still allowing asynchronous admission to fill
the next broker batch. Sarama's input channel, bridging retry length, and
bridging retry bytes are explicitly bounded. Sarama has no single setting
exactly equivalent to franz-go's total record-delivery deadline, so the
healthy-broker ranking does not compare timeout behavior. Cancellation behavior
also differs after admission; timed operations use a healthy broker and wait for
every result, so the ranking does not compare cancellation ambiguity.
Keyed multi-partition candidates all use Kafka's Murmur2 mapping. Explicit mode
uses the package policy, franz-go manual partitioner, or Sarama manual
partitioner directly; it does not infer placement from client defaults.

`kafka-go` v0.4.51 is pinned for the required comparison program, but is not
included in the durable-producer ranking: its public `Writer` supports
`RequireAll` acknowledgements but exposes no idempotent-producer mode. Comparing
that writer against the safe idempotent policy would violate the equivalent
settings requirement. The correctness test exercises it as an explicitly
non-idempotent, unranked capability control.

`TestEquivalentProducerOutcomes` and
`TestEquivalentProducerBatchOutcomes` verify the synchronous APIs.
`TestEquivalentAsynchronousProducerOutcomes` independently exercises bounded
asynchronous admission and every per-record delivery result. Each test uses a
separate real-broker read path to observe every exact key and payload once in
input order for all ranked clients and the unranked control.
`TestEquivalentMultiPartitionProducerOutcomes` separately reads every one of
eight partitions and proves exact per-partition order for the keyed and
explicit modes across all ranked clients.

## Equivalent consumer workloads

`BenchmarkEquivalentConsumerHandling` measures one stable consumer-group member
reading from one pre-created partition. Record mode handles and synchronously
commits one record per operation. Batch mode presents 10 or 100 contiguous
records to one application batch handler, then synchronously commits the last
offset.
Automatic commits are disabled. Handler success always precedes the commit.
The timed boundary includes fetch delivery, each client's public record
mapping, the handler call, and the commit; group join, input production,
fixture growth, client construction, and shutdown are outside it.

The consumer matrix ranks the package policy, raw franz-go, kafka-go, and
Sarama with 128-byte and 1 KiB payloads produced without compression or with
Snappy. Every sample receives a unique group at the earliest offset. The
harness reuses one topic per payload/compression pair and grows it in fixed
100-operation increments only to satisfy the records requested by a run,
avoiding a topic per benchmark sample. Fetch bytes, client queues, request
waits, sessions, and rebalance deadlines remain bounded.

The package policy, raw franz-go, and Sarama select cooperative-sticky
assignment. Kafka-go v0.4.51 exposes range and round-robin group balancers but
not cooperative-sticky; it uses range assignment. One member owning one
partition has the same assignment outcome, but the workload does not compare
rebalance protocols or cost. Sarama's blocking `ConsumerGroupSession.Commit`
does not return a commit error. Its healthy-broker samples are retained because
the correctness check independently verifies the exact broker offset, but the
timing evidence does not establish equivalent commit-failure reporting.
Read-committed isolation, parallel partitions, rebalances, retry, and shutdown
remain separate workloads.

`TestEquivalentConsumerOutcomes` uses independent group IDs to prove exact
record and batch handler order plus the final committed broker offset for all
four clients. `TestBenchmarkConsumerTopicReuse` proves fixture growth reuses
the same topic, and `TestBenchmarkConsumerOperationBytes` protects byte-rate
accounting for both keys and values.

`BenchmarkEquivalentCrossPartitionConsumerHandling` separately compares the
package policy with raw franz-go across eight pre-created partitions. One
operation drains exactly one record from every partition and preserves
ascending order within each partition. Kafka may return that operation through
one to eight bounded polls, so the benchmark performs one synchronous commit
after each non-empty poll and reports the observed `commits/op` instead of
assuming one fetch contains all partitions. Sequential mode admits one handler
at a time. Parallel mode admits at most eight handlers across independent
partitions while preserving sequential processing within each partition.
Every handler performs 256 SHA-256 rounds per record as fixed deterministic
application work. Input production and group setup remain outside the timer;
fetch delivery, public record mapping, handler work, and synchronous commits
are inside it.

This cross-partition workload ranks only the policy and raw franz-go because
both expose the same bounded `PollRecords` operation and commit boundary.
Kafka-go and Sarama consumer-group APIs do not expose an equivalent bounded
multi-partition poll-and-commit cycle. They remain included in the equivalent
single-partition record and batch workload; excluding them here avoids ranking
different settlement contracts.

`TestEquivalentCrossPartitionConsumerOutcomes` proves exact offsets `0`, `1`,
and `2` in each of eight partitions plus broker-verified committed offset `3`
for both ranked clients. `TestRunCrossPartitionHandlersConcurrency` uses
observable synchronization to prove the raw comparison runner admits bounded
cross-partition overlap; the policy module's own concurrency tests prove its
worker bound and per-partition serialization.
`TestBenchmarkCrossPartitionOperationBytes` protects byte-rate accounting for
all eight keys and values.

## Equivalent transactional workloads

`BenchmarkEquivalentTransactionalProduce` compares one stable transactional
producer per sample for the package policy, raw franz-go, and Sarama. One
operation begins a transaction, synchronously publishes either one or ten
keyed records one at a time through each candidate's public transaction
surface, and commits it. The matrix covers 128-byte and 1 KiB payloads with no
compression and Snappy. Client construction, producer-ID initialization, the
warm transaction, topic creation, fixture startup, and shutdown remain outside
the timer. Transaction begin, public record mapping and ownership policy,
per-record broker acknowledgement, and commit are inside it.

Every candidate uses a unique transactional ID, idempotence, all-ISR
acknowledgements, ordering-preserving in-flight settings, bounded retries,
bounded request and transaction timeouts, and one pre-created partition.
Sarama requires one open broker request for idempotence. Kafka-go is excluded
because its writer does not expose Kafka transactions. The workload measures
healthy committed transactions only; abort, fencing, timeout, and unknown
outcome behavior remain correctness and fault-injection concerns rather than
latency rankings.

`TestEquivalentTransactionalProducerOutcomes` commits two records and aborts a
third through every ranked client. Independent direct-partition readers prove
that read-committed isolation observes exactly the committed records while
read-uncommitted isolation also observes the aborted record.

`BenchmarkEquivalentConsumeTransformProduce` compares the package policy and
raw franz-go `GroupTransactSession` across one-record and ten-record source
polls. Each operation reads committed source records from one stable
cooperative-sticky group member, copies and deterministically transforms every
value, synchronously publishes one equally sized keyed output per source
record, and commits the output plus source offsets in one Kafka transaction.
Input production and group seeding are outside the timer. Poll delivery,
record mapping, transformation, synchronous output acknowledgement, source
offset addition, and transaction commit are inside it. The reported byte rate
counts logical input and output key/value bytes. `transactions/op` reports the
actual number of non-empty poll transactions because Kafka may split one
logical operation across bounded polls.

Kafka-go and Sarama are excluded from the consume-transform-produce ranking
because their public group APIs do not expose the same bounded
`GroupTransactSession` poll-and-commit boundary. Sarama remains ranked in the
producer-only transaction workload. The exclusion avoids comparing different
group-settlement and rebalance contracts.

`TestEquivalentConsumeTransformProduceOutcomes` independently proves exact
transformed output, atomic source-offset advancement, read-committed
invisibility after a forced abort, unchanged source offset after that abort,
and successful redelivery through both ranked clients. Read-uncommitted
inspection proves both the aborted output and the committed retry remain in the
Kafka log.

## Equivalent replay workload

`BenchmarkEquivalentReplay` compares complete direct-partition replay
operations for the package policy, raw franz-go, kafka-go, and Sarama. Every
operation constructs a client, validates the requested inclusive start and
exclusive end against current broker retention bounds, reads exactly the
requested offsets in ascending partition order, invokes one synchronous
borrowed-byte handler per record, and closes every client resource. No
candidate joins a consumer group or reads, commits, resets, or deletes group
offsets.

The matrix covers 10-record and 100-record exact ranges with 128-byte and 1 KiB
payloads produced without compression or with Snappy. Topic creation and input
production are outside the timer. Direct-client construction, two offset-bound
requests, fetch delivery, public record mapping, handler calls, and shutdown
are inside it. The package replay reader is intentionally single-use, so
excluding lifecycle cost would not measure its public operation. kafka-go
requires a separate public `Client` for the bound requests and `Reader` for
the direct partition; that public lifecycle is retained rather than replaced
with private internals.

`TestEquivalentReplayOutcomes` independently proves exact offsets, keys, and
values for the non-zero subrange `[1,3)` across all four clients. The workload
does not claim global order, group semantics, side-effect exactly-once
behavior, retention-gap recovery, or replay fault performance.

## Equivalent inspection workload

`BenchmarkEquivalentInspection` compares one stable read-only client per
sample for the package policy, raw franz-go, kafka-go, and Sarama. One
operation performs topic metadata, beginning-offset, end-offset, and topic
configuration requests, then returns a normalized state containing the topic
identity; leader and leader epoch; replica, ISR, and offline-replica sets;
offset bounds; and the same effective durability, retention, compaction,
segment, and unclean-election configuration fields.

The matrix covers pre-created one-partition and eight-partition topics.
Fixture startup, topic creation, client construction, warm-up, and shutdown
are outside the timer. All four protocol operations, response mapping,
validation, sorting, defensive copies, and configuration parsing are inside
it. Sarama shards offset requests by leader because its public client exposes
the protocol request at that boundary; the pinned single-node fixture needs
one request per beginning or end lookup. The workload does not compare
multi-topic partial results, group lag, health hysteresis, authorization
failures, or controller and leader failure.

`TestEquivalentInspectionOutcomes` proves exact agreement across all four
clients for three partitions and separately asserts sorted partition IDs,
available leaders, exact replica/ISR sets, no offline replicas, zero beginning
and end offsets, `min.insync.replicas=1`, and delete cleanup policy.

## Equivalent reconnect and idle-resource workloads

`BenchmarkEquivalentInspectionReconnect` compares a stable policy, raw
franz-go, kafka-go, and Sarama inspector after the same owned single-node
broker has become unreachable and restarted at the same endpoint. Before each
sample, the harness proves that the warmed client fails a complete inspection
under a two-second context while the broker is down. The timed and
allocation-counted boundary starts
after the broker is ready again and ends only after the same client returns the
exact pre-failure three-partition metadata, offset, and durability state.
Docker control, broker shutdown and startup, the deliberate failed request,
fixture construction, topic creation, client construction, warm-up, and
shutdown are outside the reported reconnect boundary.

The reconnect fixture reserves one loopback port and pins that host binding for
the complete container lifetime so a restart does not silently turn client
reconstruction against a new endpoint into a reconnect result. Reported
`reconnect-allocs/op` and `reconnect-bytes/op` are process allocation-counter
deltas across only the post-restart client operation. They include public
request mapping, client retry/backoff, connection initialization, four Kafka
protocol operations, response normalization, and harness contexts. They do not
include broker downtime, Docker work, or allocations performed concurrently
outside the measured process boundary.

`BenchmarkEquivalentInspectionIdleResources` constructs and warms one
three-partition inspector, then observes a fixed 500-millisecond interval with
no application requests. It reports the retained heap bytes and objects,
goroutine delta, active connections, verified closed connections, and Go
runtime user, GC, and scavenger CPU nanoseconds normalized to one wall-clock
second. Heap and goroutine values are process-level deltas from a
garbage-collected baseline; they can include shared runtime noise and are
descriptive rather than hard budgets. Active connection counts are obtained
from stable policy observations, franz-go hooks, kafka-go's public dial seam,
or Sarama's public broker state. Every sample closes its inspector and proves
the same number of observed active broker connections return to zero.

`TestEquivalentInspectionReconnectOutcomes` independently proves exact state
before and after a real broker restart for all four clients.
`TestEquivalentInspectionIdleResourceOutcomes` proves the measured clients
hold at least one warmed connection, return the same normalized topic state,
record nonnegative CPU time, and close every observed connection. These
single-node plaintext workloads do not establish multi-broker leader recovery,
TLS reconnect cost, process RSS, kernel socket memory, or deployment-specific
resource budgets.

## Broker selection

By default, the integration workload starts the pinned single-node Confluent
Local image through testcontainers and rejects a runtime version other than
`7.5.0-ccs`. Set `KAFKA_BENCH_BROKERS` to a comma-separated broker list to use
an already provisioned cluster. External brokers are never created,
reconfigured, or deleted, but the harness creates uniquely named one-partition
topics. Set `KAFKA_BENCH_BROKER_IDENTITY` to a bounded public description of
that cluster using only ASCII letters, digits, spaces, and `._:+()-`; do not
put credentials or secret-bearing URLs in either variable or captured output.

The default fixture is local, single-node, plaintext, and shares host CPU and
network resources. Results describe only that environment. They must not be
used to claim production throughput or superiority.

## Running

Use a new isolated `GOCACHE` for every command or independent gate. Store the
environment, raw samples, and analysis outside that temporary cache:

```sh
make test
make verify
make environment > environment-sync.txt
make capture OUTPUT=raw-producer.txt BENCH_PATTERN='^BenchmarkEquivalentSynchronousProduce$$' BENCH_COUNT=10 BENCH_TIME=10x
make capture OUTPUT=raw-producer-batch.txt BENCH_PATTERN='^BenchmarkEquivalentSynchronousBatchProduce$$' BENCH_COUNT=10 BENCH_TIME=10x
make environment > environment-async.txt
make capture OUTPUT=raw-producer-async.txt BENCH_PATTERN='^BenchmarkEquivalentAsynchronousProduce$$' BENCH_COUNT=10 BENCH_TIME=10x
make environment > environment-multi-partition.txt
make capture OUTPUT=raw-producer-multi-partition.txt BENCH_PATTERN='^BenchmarkEquivalentMultiPartitionProduce$$' BENCH_COUNT=10 BENCH_TIME=10x
make environment > environment-consumer.txt
make capture OUTPUT=raw-consumer.txt BENCH_PATTERN='^BenchmarkEquivalentConsumerHandling$$' BENCH_COUNT=20 BENCH_TIME=10x
make environment > environment-consumer-cross-partition.txt
make capture OUTPUT=raw-consumer-cross-partition.txt BENCH_PATTERN='^BenchmarkEquivalentCrossPartitionConsumerHandling$$' BENCH_COUNT=20 BENCH_TIME=10x
make environment > environment-transaction-producer.txt
make capture OUTPUT=raw-transaction-producer.txt BENCH_PATTERN='^BenchmarkEquivalentTransactionalProduce$$' BENCH_COUNT=20 BENCH_TIME=10x
make environment > environment-consume-transform-produce.txt
make capture OUTPUT=raw-consume-transform-produce.txt BENCH_PATTERN='^BenchmarkEquivalentConsumeTransformProduce$$' BENCH_COUNT=20 BENCH_TIME=10x
make environment > environment-replay.txt
make capture OUTPUT=raw-replay.txt BENCH_PATTERN='^BenchmarkEquivalentReplay$$' BENCH_COUNT=20 BENCH_TIME=10x
make environment > environment-inspection.txt
make capture OUTPUT=raw-inspection.txt BENCH_PATTERN='^BenchmarkEquivalentInspection$$' BENCH_COUNT=20 BENCH_TIME=10x
make environment > environment-resources.txt
make resource-capture OUTPUT=raw-resources.txt RESOURCE_COUNT=10 RESOURCE_TIME=1x
make analyze INPUT=raw-producer.txt > producer-benchstat.txt
make analyze INPUT=raw-producer-batch.txt > producer-batch-benchstat.txt
make analyze INPUT=raw-producer-async.txt > producer-async-benchstat.txt
make analyze INPUT=raw-producer-multi-partition.txt > producer-multi-partition-benchstat.txt
make analyze INPUT=raw-consumer.txt > consumer-benchstat.txt
make analyze INPUT=raw-consumer-cross-partition.txt > consumer-cross-partition-benchstat.txt
make analyze INPUT=raw-transaction-producer.txt > transaction-producer-benchstat.txt
make analyze INPUT=raw-consume-transform-produce.txt > consume-transform-produce-benchstat.txt
make analyze INPUT=raw-replay.txt > replay-benchstat.txt
make analyze INPUT=raw-inspection.txt > inspection-benchstat.txt
make analyze INPUT=raw-resources.txt > resources-benchstat.txt
```

Ten independent samples are the default. Publish the raw samples and benchstat
distributions with a workload-specific environment record; never replace an
earlier capture's input identity when the harness changes. Do not select only
the best result. The dedicated resource target fixes each expensive broker
restart or idle interval at one operation per sample; do not fold it into the
healthy-path benchmark matrix. Functional verification and timing remain
separate commands.
