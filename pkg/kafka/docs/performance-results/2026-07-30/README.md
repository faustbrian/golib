# Equivalent synchronous producer captures, 2026-07-30

This directory publishes one bounded local comparison run. It is evidence for
the exact workload and environment below, not a general client ranking.

## Workload

- benchmarks: `BenchmarkEquivalentSynchronousProduce` and
  `BenchmarkEquivalentSynchronousBatchProduce`
- samples: 20 per workload/client combination
- timed iterations: 50 acknowledged single records or complete batches per
  sample
- ranked clients: package policy, raw franz-go, IBM/Sarama
- key modes: keyed and explicitly unkeyed
- single-record payloads: 128 bytes, 1 KiB, and 64 KiB
- batch payloads: 128 bytes and 1 KiB across 10-record and 100-record batches
- compression: none and Snappy
- broker contract: one pre-created partition, idempotence, ordered synchronous
  production, and all-ISR acknowledgements
- fixture: immutable Confluent Local 7.5.0 image; runtime reported `7.5.0-ccs`

Kafka-go v0.4.51 is present only in the separately executed correctness check.
It is not in the timing output because its producer cannot match the required
idempotence setting.

## Files

- [`environment.txt`](environment.txt) records the Go, host, Docker, broker,
  dependency, workspace revision, and tracked harness-input identities.
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

## Commands

Each command ran with its own fresh isolated `GOCACHE` inherited by all child
processes:

```sh
make verify
make environment > environment.txt
make capture OUTPUT=raw-producer.txt BENCH_PATTERN='^BenchmarkEquivalentSynchronousProduce$$' BENCH_COUNT=20 BENCH_TIME=50x
make capture OUTPUT=raw-producer-batch.txt BENCH_PATTERN='^BenchmarkEquivalentSynchronousBatchProduce$$' BENCH_COUNT=20 BENCH_TIME=50x
make analyze INPUT=raw-producer.txt > producer-benchstat.txt
make analyze INPUT=raw-producer-batch.txt > producer-batch-benchstat.txt
```

The single and batch correctness checks passed together before the captures and
independently observed exact input-ordered records from every client. The timed
single-record command passed in 125.732 seconds; the batch command passed in
151.422 seconds.

## Artifact digests

```text
5138d5e160a52eb6ada65c9e0a04e20000041afb09c6b7d98806a30a5edc3d19  environment.txt
204e442595be45e0c48b34d1118ede761e8febf4c2fa54278a3bfcfed68dc072  raw-producer.txt
d8f67026840c89649d360fb9ae3e70d020cbdc6ced5c85c9e6d7f603593004ca  producer-benchstat.txt
61122eac053d494be4a78f19fbc45badca1a59138a6b0de6073b061020ad65e5  raw-producer-batch.txt
a671b557caedce60d1f1962422c76ea8a0c2dd2518a4a2ddaa7be475ace1cc15  producer-batch-benchstat.txt
```

Several franz-based results have wide distributions on the shared local
fixture. Preserve that variance when interpreting this run; do not select the
best sample or infer production superiority.
