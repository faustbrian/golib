# Equivalent Kafka client captures, 2026-07-30

This directory publishes one bounded local comparison run. It is evidence for
the exact workload and environment below, not a general client ranking.

## Workload

- benchmarks: `BenchmarkEquivalentSynchronousProduce`,
  `BenchmarkEquivalentSynchronousBatchProduce`, and
  `BenchmarkEquivalentAsynchronousProduce`, plus
  `BenchmarkEquivalentMultiPartitionProduce` and
  `BenchmarkEquivalentConsumerHandling`, plus
  `BenchmarkEquivalentCrossPartitionConsumerHandling`,
  `BenchmarkEquivalentTransactionalProduce`, and
  `BenchmarkEquivalentConsumeTransformProduce`
- samples: 20 per workload/client combination
- timed iterations: 50 acknowledged producer operations or 10 consumer
  fetch-handler-commit operations per sample
- ranked producer clients: package policy, raw franz-go, IBM/Sarama
- ranked consumer clients: package policy, raw franz-go, kafka-go, IBM/Sarama
- ranked cross-partition consumer clients: package policy and raw franz-go
- ranked transactional producer clients: package policy, raw franz-go,
  IBM/Sarama
- ranked consume-transform-produce clients: package policy and raw franz-go
- key modes: keyed and explicitly unkeyed
- single-record payloads: 128 bytes, 1 KiB, and 64 KiB
- batch payloads: 128 bytes and 1 KiB across 10-record and 100-record batches
- asynchronous payloads: 128 bytes and 1 KiB across bounded 10-record and
  100-record outstanding windows
- multi-partition payloads: 128 bytes and 1 KiB across one balanced 80-record
  Murmur2-keyed or explicitly assigned batch spanning eight partitions
- consumer payloads: 128 bytes and 1 KiB across one-record handling and
  10-record or 100-record partition batches
- cross-partition consumer payloads: 128 bytes and 1 KiB with one record in
  each of eight partitions and 256 SHA-256 handler rounds per record
- transaction payloads: 128 bytes and 1 KiB across one-record and ten-record
  transactions
- compression: none and Snappy
- broker contract: one or eight pre-created partitions, idempotence, ordered
  synchronous or bounded asynchronous production, and all-ISR acknowledgements
- consumer contract: one stable group member and partition, earliest reset,
  automatic commits disabled, handler success before a synchronous commit, and
  an exact externally verified committed offset
- cross-partition consumer contract: one stable group member owns eight
  partitions; each operation handles one record per partition sequentially or
  with concurrency bounded at eight, preserves per-partition order, disables
  automatic commits, and synchronously commits every non-empty poll
- transactional producer contract: one unique transactional ID per client,
  idempotence, all-ISR acknowledgements, one pre-created partition, synchronous
  per-record acknowledgement, and one commit per operation
- consume-transform-produce contract: one read-committed group member and
  unique transactional ID atomically commit transformed outputs and source
  offsets; input production remains outside the timer
- fixture: immutable Confluent Local 7.5.0 image; runtime reported `7.5.0-ccs`

Kafka-go v0.4.51's producer is present only in the separately executed producer
correctness check. It is not in the producer timing output because it cannot
match the required idempotence setting. Its manually committed consumer is in
the consumer timing output. Kafka-go uses range assignment while the other
consumer candidates use cooperative-sticky; one member and one partition have
the same assignment outcome, so rebalance behavior remains outside the
workload. Sarama's synchronous consumer commit does not return an error. The
healthy-broker capture independently verifies its exact final broker offset but
does not compare commit-failure reporting.

The cross-partition capture excludes kafka-go and Sarama because their public
consumer-group APIs do not expose the same bounded multi-partition
poll-and-commit cycle as the package policy and raw franz-go. One logical
operation may require one to eight polls and commits; the capture reports the
observed count. All samples in this run completed with one commit per
eight-record operation. The exclusion preserves settlement equivalence and
does not imply unsupported-client behavior.

The producer transaction capture excludes kafka-go because its writer does not
expose Kafka transactions. The consume-transform-produce capture also excludes
Sarama because neither its public group API nor kafka-go's exposes the same
bounded `GroupTransactSession` poll-and-commit boundary. Sarama remains ranked
in the producer-only transaction workload. The healthy-broker timings do not
rank abort, fencing, timeout, unknown-outcome, or rebalance behavior.

## Files

- [`environment-sync.txt`](environment-sync.txt) records the Go, host, Docker,
  broker, dependency, execution revision, and harness-input identities
  for the refreshed single-record and batch captures.
- [`environment-async.txt`](environment-async.txt) records the same exact
  identity set for the asynchronous capture. Separate files prevent a later
  workload from replacing provenance for earlier raw results.
- [`environment-multi-partition.txt`](environment-multi-partition.txt) binds
  the eight-partition capture to its own execution revision and harness inputs.
- [`environment-consumer.txt`](environment-consumer.txt) binds the consumer
  capture to the consumer harness and its exact execution environment.
- [`environment-consumer-cross-partition.txt`](environment-consumer-cross-partition.txt)
  binds the sequential and bounded-parallel eight-partition consumer capture
  to its exact harness and execution environment.
- [`environment-transaction-producer.txt`](environment-transaction-producer.txt)
  binds the producer-only transaction capture to its exact harness and
  execution environment.
- [`environment-consume-transform-produce.txt`](environment-consume-transform-produce.txt)
  binds the consume-transform-produce capture to its exact harness and
  execution environment.
- [`raw-producer.txt`](raw-producer.txt) contains all 720 unmodified
  single-record benchmark samples and the exact runtime broker assertion.
- [`producer-benchstat.txt`](producer-benchstat.txt) contains the benchstat
  single-record medians and distribution spreads for time, throughput, bytes,
  and allocations.
- [`raw-producer-batch.txt`](raw-producer-batch.txt) contains all 960
  unmodified batch benchmark samples and the exact runtime broker assertion.
- [`producer-batch-benchstat.txt`](producer-batch-benchstat.txt) contains the
  batch medians and distribution spreads, including the exact records per
  operation.
- [`raw-producer-async.txt`](raw-producer-async.txt) contains all 960 unmodified
  bounded asynchronous benchmark samples and the exact runtime broker
  assertion.
- [`producer-async-benchstat.txt`](producer-async-benchstat.txt) contains the
  asynchronous-window medians and distribution spreads, including the exact
  records per operation.
- [`raw-producer-multi-partition.txt`](raw-producer-multi-partition.txt)
  contains all 480 unmodified eight-partition benchmark samples.
- [`producer-multi-partition-benchstat.txt`](producer-multi-partition-benchstat.txt)
  contains its medians and distributions, including exact record and partition
  counts per operation.
- [`raw-consumer.txt`](raw-consumer.txt) contains all 960 consumer benchmark
  samples and the exact runtime broker assertion.
- [`consumer-benchstat.txt`](consumer-benchstat.txt) contains record and batch
  medians and distributions for time, throughput, bytes, and allocations.
- [`raw-consumer-cross-partition.txt`](raw-consumer-cross-partition.txt)
  contains all 320 unmodified sequential and bounded-parallel eight-partition
  consumer benchmark samples.
- [`consumer-cross-partition-benchstat.txt`](consumer-cross-partition-benchstat.txt)
  contains its latency, throughput, commit-count, byte, and allocation
  distributions.
- [`raw-transaction-producer.txt`](raw-transaction-producer.txt) contains all
  480 producer transaction samples and exact record and transaction counts.
- [`transaction-producer-benchstat.txt`](transaction-producer-benchstat.txt)
  contains the producer transaction medians and distributions.
- [`raw-consume-transform-produce.txt`](raw-consume-transform-produce.txt)
  contains all 320 atomic read-process-write samples and exact source, output,
  and transaction counts.
- [`consume-transform-produce-benchstat.txt`](consume-transform-produce-benchstat.txt)
  contains the consume-transform-produce medians and distributions.

## Commands

Each command ran with its own fresh isolated `GOCACHE` inherited by all child
processes:

```sh
make verify
make environment > environment-sync.txt
make capture OUTPUT=raw-producer.txt BENCH_PATTERN='^BenchmarkEquivalentSynchronousProduce$$' BENCH_COUNT=20 BENCH_TIME=50x
make capture OUTPUT=raw-producer-batch.txt BENCH_PATTERN='^BenchmarkEquivalentSynchronousBatchProduce$$' BENCH_COUNT=20 BENCH_TIME=50x
make environment > environment-async.txt
make capture OUTPUT=raw-producer-async.txt BENCH_PATTERN='^BenchmarkEquivalentAsynchronousProduce$$' BENCH_COUNT=20 BENCH_TIME=50x
make environment > environment-multi-partition.txt
make capture OUTPUT=raw-producer-multi-partition.txt BENCH_PATTERN='^BenchmarkEquivalentMultiPartitionProduce$$' BENCH_COUNT=20 BENCH_TIME=50x
make environment > environment-consumer.txt
make capture OUTPUT=raw-consumer.txt BENCH_PATTERN='^BenchmarkEquivalentConsumerHandling$$' BENCH_COUNT=20 BENCH_TIME=10x
make environment > environment-consumer-cross-partition.txt
make capture OUTPUT=raw-consumer-cross-partition.txt BENCH_PATTERN='^BenchmarkEquivalentCrossPartitionConsumerHandling$$' BENCH_COUNT=20 BENCH_TIME=10x
make environment > environment-transaction-producer.txt
make capture OUTPUT=raw-transaction-producer.txt BENCH_PATTERN='^BenchmarkEquivalentTransactionalProduce$$' BENCH_COUNT=20 BENCH_TIME=10x
make environment > environment-consume-transform-produce.txt
make capture OUTPUT=raw-consume-transform-produce.txt BENCH_PATTERN='^BenchmarkEquivalentConsumeTransformProduce$$' BENCH_COUNT=20 BENCH_TIME=10x
make analyze INPUT=raw-producer.txt > producer-benchstat.txt
make analyze INPUT=raw-producer-batch.txt > producer-batch-benchstat.txt
make analyze INPUT=raw-producer-async.txt > producer-async-benchstat.txt
make analyze INPUT=raw-producer-multi-partition.txt > producer-multi-partition-benchstat.txt
make analyze INPUT=raw-consumer.txt > consumer-benchstat.txt
make analyze INPUT=raw-consumer-cross-partition.txt > consumer-cross-partition-benchstat.txt
make analyze INPUT=raw-transaction-producer.txt > transaction-producer-benchstat.txt
make analyze INPUT=raw-consume-transform-produce.txt > consume-transform-produce-benchstat.txt
```

The producer correctness checks passed together before their captures. An
independent read path observed exact input order for the single-partition
checks; the eight-partition check verified that order separately within every
partition. The consumer correctness check proved exact record and batch
handler order plus the final broker offset for all four clients. The timed
single-record command passed in 125.732 seconds; the batch command passed in
151.422 seconds; the asynchronous command passed in 367.1 seconds, including
353.308 seconds in the benchmark process; and the multi-partition command
passed in 152.7 seconds, including 129.598 seconds in the benchmark process.
The consumer command passed in 190 seconds, including 174.493 seconds in the
benchmark process. The cross-partition consumer command passed with 320
samples and 3,200 timed operations covering 25,600 records; its benchmark
process completed in 47.519 seconds. The producer transaction command passed
with 480 samples and 4,800 timed committed transactions covering 26,400
records; its benchmark process completed in 156.956 seconds. The
consume-transform-produce command passed with 320 samples and 3,200 timed
atomic operations covering 17,600 source and 17,600 output records; its
benchmark process completed in 56.464 seconds. Every transactional sample
reported exactly one Kafka transaction per logical operation.

## Artifact digests

```text
5138d5e160a52eb6ada65c9e0a04e20000041afb09c6b7d98806a30a5edc3d19  environment-sync.txt
c8ae78cb8c533c4700e75f10b2df04475b8d0c6c5db19ca86dd63768d730bfa5  environment-async.txt
8f32676be908c52309456267ce8f33155a929967e6754bb51528bcdebaf5b0f3  environment-multi-partition.txt
3cc508d020f8df08c2353ccacf0cf18dc07d39aa5c93983fc6bcf58581ff9734  environment-consumer.txt
8ccb8aa095093272acef9a7b22ffcec682039bf6f3614a189e05b69de0ee749d  environment-consumer-cross-partition.txt
204e442595be45e0c48b34d1118ede761e8febf4c2fa54278a3bfcfed68dc072  raw-producer.txt
d8f67026840c89649d360fb9ae3e70d020cbdc6ced5c85c9e6d7f603593004ca  producer-benchstat.txt
61122eac053d494be4a78f19fbc45badca1a59138a6b0de6073b061020ad65e5  raw-producer-batch.txt
a671b557caedce60d1f1962422c76ea8a0c2dd2518a4a2ddaa7be475ace1cc15  producer-batch-benchstat.txt
a9e2fb1d5e4a238282384b1e0f6696ec66f8ddc3e6fcf2b1cb83e6c0d84e98d6  raw-producer-async.txt
3aecdeb54b044d916395a5f3b79d73ef9c09e1a8c992ffe6432f24f28a0e10e4  producer-async-benchstat.txt
385204d566ebb2d1afe8884dc6e581299d018cd16fb8a3d2da21cafb38ad2004  raw-producer-multi-partition.txt
7c74bd48a2fcd8411007294b7f453c0bbbceb78747aaeeb25687effdd10fa4d3  producer-multi-partition-benchstat.txt
eb995d8f44a412accc6ce8f08ab1d768e63d697aa9c68e4728849e775fe211f7  raw-consumer.txt
140e2734f35d36324193cb21140a612d2ff94f141df34a5ed4dbe1f8ab2daaf6  consumer-benchstat.txt
7086acbe1091f3ca1b3bb56649032145edf95328590988a0770b499c5210d73b  raw-consumer-cross-partition.txt
2df9e51162064ebfc082f56ebc32972e511f502672f29f7bdc6f508f69c30daa  consumer-cross-partition-benchstat.txt
f249510e86986350c83d9d006b1932d03dd4a764ed5433285d465b02d07ad5b6  environment-transaction-producer.txt
f2a860a66a685a9fd69c62fd2dd2982a4dd32c7b0c8e446aed0832a1f969c5bb  raw-transaction-producer.txt
664387d92bee9cdd016d104c833b595753faea8c438dbff750a63ea44827c71a  transaction-producer-benchstat.txt
f249510e86986350c83d9d006b1932d03dd4a764ed5433285d465b02d07ad5b6  environment-consume-transform-produce.txt
f77a6d7e12dbc673df2e3fbfead4dc678d6b4c1f34ffb1e050e263c61d807312  raw-consume-transform-produce.txt
6ea2ccc5b75dd10b8cc06af919e001911f987445f20a951f496660b624b134a7  consume-transform-produce-benchstat.txt
```

Several producer franz-based results and consumer results have wide
distributions on the shared local fixture; one consumer distribution spans
320 percent, one cross-partition distribution spans 66 percent, and
transaction latency distributions span as far as 76 percent. Preserve that
variance when interpreting this run; do not select the best sample or infer
production superiority.
