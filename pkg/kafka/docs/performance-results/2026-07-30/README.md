# Equivalent producer captures, 2026-07-30

This directory publishes one bounded local comparison run. It is evidence for
the exact workload and environment below, not a general client ranking.

## Workload

- benchmarks: `BenchmarkEquivalentSynchronousProduce`,
  `BenchmarkEquivalentSynchronousBatchProduce`, and
  `BenchmarkEquivalentAsynchronousProduce`
- samples: 20 per workload/client combination
- timed iterations: 50 acknowledged single records, complete batches, or
  bounded asynchronous windows per sample
- ranked clients: package policy, raw franz-go, IBM/Sarama
- key modes: keyed and explicitly unkeyed
- single-record payloads: 128 bytes, 1 KiB, and 64 KiB
- batch payloads: 128 bytes and 1 KiB across 10-record and 100-record batches
- asynchronous payloads: 128 bytes and 1 KiB across bounded 10-record and
  100-record outstanding windows
- compression: none and Snappy
- broker contract: one pre-created partition, idempotence, ordered synchronous
  or bounded asynchronous production, and all-ISR acknowledgements
- fixture: immutable Confluent Local 7.5.0 image; runtime reported `7.5.0-ccs`

Kafka-go v0.4.51 is present only in the separately executed correctness check.
It is not in the timing output because its producer cannot match the required
idempotence setting.

## Files

- [`environment-sync.txt`](environment-sync.txt) records the Go, host, Docker,
  broker, dependency, execution revision, and tracked harness-input identities
  for the refreshed single-record and batch captures.
- [`environment-async.txt`](environment-async.txt) records the same exact
  identity set for the asynchronous capture. Separate files prevent a later
  workload from replacing provenance for earlier raw results.
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
make analyze INPUT=raw-producer.txt > producer-benchstat.txt
make analyze INPUT=raw-producer-batch.txt > producer-batch-benchstat.txt
make analyze INPUT=raw-producer-async.txt > producer-async-benchstat.txt
```

The single, batch, and bounded asynchronous correctness checks passed together
before the captures and independently observed exact input-ordered records from
every client. The timed single-record command passed in 125.732 seconds; the
batch command passed in 151.422 seconds; and the asynchronous command passed in
367.1 seconds, including 353.308 seconds in the benchmark process.

## Artifact digests

```text
5138d5e160a52eb6ada65c9e0a04e20000041afb09c6b7d98806a30a5edc3d19  environment-sync.txt
c8ae78cb8c533c4700e75f10b2df04475b8d0c6c5db19ca86dd63768d730bfa5  environment-async.txt
204e442595be45e0c48b34d1118ede761e8febf4c2fa54278a3bfcfed68dc072  raw-producer.txt
d8f67026840c89649d360fb9ae3e70d020cbdc6ced5c85c9e6d7f603593004ca  producer-benchstat.txt
61122eac053d494be4a78f19fbc45badca1a59138a6b0de6073b061020ad65e5  raw-producer-batch.txt
a671b557caedce60d1f1962422c76ea8a0c2dd2518a4a2ddaa7be475ace1cc15  producer-batch-benchstat.txt
a9e2fb1d5e4a238282384b1e0f6696ec66f8ddc3e6fcf2b1cb83e6c0d84e98d6  raw-producer-async.txt
3aecdeb54b044d916395a5f3b79d73ef9c09e1a8c992ffe6432f24f28a0e10e4  producer-async-benchstat.txt
```

Several franz-based results have wide distributions on the shared local
fixture. Preserve that variance when interpreting this run; do not select the
best sample or infer production superiority.
