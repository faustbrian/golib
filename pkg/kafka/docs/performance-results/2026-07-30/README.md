# Equivalent synchronous producer capture, 2026-07-30

This directory publishes one bounded local comparison run. It is evidence for
the exact workload and environment below, not a general client ranking.

## Workload

- benchmark: `BenchmarkEquivalentSynchronousProduce`
- samples: 20 per workload/client combination
- timed iterations: 50 acknowledged records per sample
- ranked clients: package policy, raw franz-go, IBM/Sarama
- key modes: keyed and explicitly unkeyed
- payloads: 128 bytes, 1 KiB, and 64 KiB
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
- [`raw-producer.txt`](raw-producer.txt) contains all 720 unmodified Go
  benchmark samples and the exact runtime broker assertion.
- [`producer-benchstat.txt`](producer-benchstat.txt) contains the benchstat
  medians and distribution spreads for time, throughput, bytes, and
  allocations.

## Commands

Each command ran with its own fresh isolated `GOCACHE` inherited by all child
processes:

```sh
make verify
make environment > environment.txt
make benchmark BENCH_COUNT=20 BENCH_TIME=50x > raw-producer.txt
make analyze INPUT=raw-producer.txt > producer-benchstat.txt
```

The correctness check passed before the capture and independently observed the
exact record produced by every client. The timed command passed in 117.782
seconds.

## Artifact digests

```text
1d1862c1e2561bd350bd05eeac7acb3915a813a0e09bfd3b4982e54b5bf42778  environment.txt
b4bc657d41462b665d334647a3ac44691bad1da10b62e3d00d89c2373f1e7e24  raw-producer.txt
3ca23eebdef8bb3809508dc4d6b93612c4ddedcbccea92b22796239134da9c9e  producer-benchstat.txt
```

Several franz-based results have wide distributions on the shared local
fixture. Preserve that variance when interpreting this run; do not select the
best sample or infer production superiority.
