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

## Equivalent synchronous producer workload

`BenchmarkEquivalentSynchronousProduce` reuses one warmed producer and sends
one record at a time until the broker acknowledges all in-sync replicas. Every
ranked candidate has idempotence enabled, preserves order, uses one effective
in-flight request in this serial workload, disables topic auto-creation, uses
one pre-created partition, retries at most ten times, and has bounded request
and delivery waits. Client construction, metadata warm-up, topic creation, and
fixture startup are outside the timer. Record construction, client policy,
serialization, compression, network transit, broker processing, and delivery
result handling are inside it.

The matrix covers keyed and explicitly accepted unkeyed records, payloads of
128 bytes, 1 KiB, and 64 KiB, and no compression plus Snappy. The one-partition
fixture deliberately removes partition-count and partitioner distribution as
variables. It does not establish many-partition, asynchronous, batch,
transaction, reconnect, TLS, or steady-state resource results; those require
separate workloads.

The policy library and raw franz-go use the same franz-go producer controls.
Sarama requires `Net.MaxOpenRequests=1` for idempotence; because this workload
submits only one synchronous record at a time, that lower client ceiling does
not change the effective in-flight count. Sarama has no single setting exactly
equivalent to franz-go's total record-delivery deadline, so the healthy-broker
ranking does not compare timeout behavior.

`kafka-go` v0.4.51 is pinned for the required comparison program, but is not
included in the durable-producer ranking: its public `Writer` supports
`RequireAll` acknowledgements but exposes no idempotent-producer mode. Comparing
that writer against the safe idempotent policy would violate the equivalent
settings requirement. The correctness test exercises it as an explicitly
non-idempotent, unranked capability control. An equivalent consumer-group
workload will include it; until that workload is captured, the overall
competitor matrix remains incomplete.

`TestEquivalentProducerOutcomes` separately verifies against the real fixture
that every ranked client receives a successful acknowledgement and that a
separate read path observes each exact key and payload once.

## Broker selection

By default, the integration workload starts the pinned single-node Confluent
Local image through testcontainers. Set `KAFKA_BENCH_BROKERS` to a comma-separated
broker list to use an already provisioned cluster. External brokers are never
created, reconfigured, or deleted, but the harness creates uniquely named
one-partition topics. Set `KAFKA_BENCH_BROKER_IDENTITY` to a bounded public
description of that cluster; do not put credentials or secret-bearing URLs in
either variable or captured output.

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
make capture OUTPUT=raw-producer.txt BENCH_COUNT=10 BENCH_TIME=10x
make analyze INPUT=raw-producer.txt > producer-benchstat.txt
```

Ten independent samples are the default. Publish the raw samples and
benchstat distributions with the environment record; do not select only the
best result. Functional verification and timing remain separate commands.
