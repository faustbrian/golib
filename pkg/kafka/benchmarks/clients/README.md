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
workspace revision, Docker engine, client versions, and broker image.

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
Every ranked candidate has idempotence enabled, preserves order, disables topic
auto-creation, uses one pre-created partition, retries at most ten times, and
has bounded request waits, client channels, and retry buffers. Client
construction, metadata warm-up, topic creation, fixture startup, and shutdown
are outside the timer. Public record mapping, client policy, serialization,
compression, network transit, broker processing, and delivery-result handling
are inside it.

The single-record matrix covers keyed and explicitly accepted unkeyed records,
payloads of 128 bytes, 1 KiB, and 64 KiB, and no compression plus Snappy. The
batch matrix uses the same key and compression modes, 128-byte and 1 KiB
payloads, and 10-record plus 100-record batches. The one-partition fixture
deliberately removes partition-count and partitioner distribution as variables.
The asynchronous matrix uses the batch matrix's key, payload, compression, and
10/100-record dimensions but submits each record independently before awaiting
all results. It does not establish many-partition, transaction, reconnect, TLS,
or steady-state resource results; those require separate workloads.

The policy library and raw franz-go use the same franz-go producer controls.
Sarama requires `Net.MaxOpenRequests=1` for idempotence; the one-partition
workload preserves ordering while still allowing asynchronous admission to fill
the next broker batch. Sarama's input channel, bridging retry length, and
bridging retry bytes are explicitly bounded. Sarama has no single setting
exactly equivalent to franz-go's total record-delivery deadline, so the
healthy-broker ranking does not compare timeout behavior. Cancellation behavior
also differs after admission; timed operations use a healthy broker and wait for
every result, so the ranking does not compare cancellation ambiguity.

`kafka-go` v0.4.51 is pinned for the required comparison program, but is not
included in the durable-producer ranking: its public `Writer` supports
`RequireAll` acknowledgements but exposes no idempotent-producer mode. Comparing
that writer against the safe idempotent policy would violate the equivalent
settings requirement. The correctness test exercises it as an explicitly
non-idempotent, unranked capability control. An equivalent consumer-group
workload will include it; until that workload is captured, the overall
competitor matrix remains incomplete.

`TestEquivalentProducerOutcomes` and
`TestEquivalentProducerBatchOutcomes` verify the synchronous APIs.
`TestEquivalentAsynchronousProducerOutcomes` independently exercises bounded
asynchronous admission and every per-record delivery result. Each test uses a
separate real-broker read path to observe every exact key and payload once in
input order for all ranked clients and the unranked control.

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
make environment > environment.txt
make capture OUTPUT=raw-producer.txt BENCH_PATTERN='^BenchmarkEquivalentSynchronousProduce$$' BENCH_COUNT=10 BENCH_TIME=10x
make capture OUTPUT=raw-producer-batch.txt BENCH_PATTERN='^BenchmarkEquivalentSynchronousBatchProduce$$' BENCH_COUNT=10 BENCH_TIME=10x
make capture OUTPUT=raw-producer-async.txt BENCH_PATTERN='^BenchmarkEquivalentAsynchronousProduce$$' BENCH_COUNT=10 BENCH_TIME=10x
make analyze INPUT=raw-producer.txt > producer-benchstat.txt
make analyze INPUT=raw-producer-batch.txt > producer-batch-benchstat.txt
make analyze INPUT=raw-producer-async.txt > producer-async-benchstat.txt
```

Ten independent samples are the default. Publish the raw samples and
benchstat distributions with the environment record; do not select only the
best result. Functional verification and timing remain separate commands.
