# Equivalent Kafka client captures, 2026-07-30

This directory publishes one bounded local comparison run. It is evidence for
the exact workload and environment below, not a general client ranking.

## Workload

- benchmarks: `BenchmarkEquivalentSynchronousProduce`,
  `BenchmarkEquivalentSynchronousBatchProduce`, and
  `BenchmarkEquivalentAsynchronousProduce`, plus
  `BenchmarkEquivalentMultiPartitionProduce` and
  `BenchmarkEquivalentConsumerHandling`
- samples: 20 per workload/client combination
- timed iterations: 50 acknowledged producer operations or 10 consumer
  fetch-handler-commit operations per sample
- ranked producer clients: package policy, raw franz-go, IBM/Sarama
- ranked consumer clients: package policy, raw franz-go, kafka-go, IBM/Sarama
- key modes: keyed and explicitly unkeyed
- single-record payloads: 128 bytes, 1 KiB, and 64 KiB
- batch payloads: 128 bytes and 1 KiB across 10-record and 100-record batches
- asynchronous payloads: 128 bytes and 1 KiB across bounded 10-record and
  100-record outstanding windows
- multi-partition payloads: 128 bytes and 1 KiB across one balanced 80-record
  Murmur2-keyed or explicitly assigned batch spanning eight partitions
- consumer payloads: 128 bytes and 1 KiB across one-record handling and
  10-record or 100-record partition batches
- compression: none and Snappy
- broker contract: one or eight pre-created partitions, idempotence, ordered
  synchronous or bounded asynchronous production, and all-ISR acknowledgements
- consumer contract: one stable group member and partition, earliest reset,
  automatic commits disabled, handler success before a synchronous commit, and
  an exact externally verified committed offset
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
make analyze INPUT=raw-producer.txt > producer-benchstat.txt
make analyze INPUT=raw-producer-batch.txt > producer-batch-benchstat.txt
make analyze INPUT=raw-producer-async.txt > producer-async-benchstat.txt
make analyze INPUT=raw-producer-multi-partition.txt > producer-multi-partition-benchstat.txt
make analyze INPUT=raw-consumer.txt > consumer-benchstat.txt
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
benchmark process.

## Artifact digests

```text
5138d5e160a52eb6ada65c9e0a04e20000041afb09c6b7d98806a30a5edc3d19  environment-sync.txt
c8ae78cb8c533c4700e75f10b2df04475b8d0c6c5db19ca86dd63768d730bfa5  environment-async.txt
8f32676be908c52309456267ce8f33155a929967e6754bb51528bcdebaf5b0f3  environment-multi-partition.txt
3cc508d020f8df08c2353ccacf0cf18dc07d39aa5c93983fc6bcf58581ff9734  environment-consumer.txt
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
```

Several producer franz-based results and consumer results have wide
distributions on the shared local fixture; one consumer distribution spans
320 percent. Preserve that variance when interpreting this run; do not select
the best sample or infer production superiority.
