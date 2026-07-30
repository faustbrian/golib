# Equivalent Kafka replay and inspection captures, 2026-07-31

This directory publishes one bounded local comparison run. It is evidence for
the exact workload and environment below, not a general client ranking.

## Workloads

`BenchmarkEquivalentReplay` compares the package policy, raw franz-go,
kafka-go, and IBM/Sarama across:

- 10-record and 100-record exact direct-partition ranges;
- 128-byte and 1 KiB payloads;
- records produced without compression and with Snappy; and
- 20 independent samples with one complete replay operation per sample.

Each timed operation constructs the public client resources, requests the
broker beginning and end offsets, validates the inclusive start and exclusive
end, reads every requested offset in ascending order, invokes a synchronous
borrowed-byte handler, and closes all resources. Topic creation and input
production remain outside the timer. No candidate joins a consumer group or
reads, commits, resets, or deletes group offsets. The capture contains 640
samples, 640 complete replay operations, and 35,200 handled records.

`BenchmarkEquivalentInspection` compares one stable client per sample for the
same four clients across one-partition and eight-partition topics. Each timed
operation performs topic metadata, beginning-offset, end-offset, and topic
configuration requests and normalizes the same topic identity, leader epoch,
replica/ISR/offline state, offsets, and durability, retention, compaction,
segment, and unclean-election configuration. The capture contains 160 samples
of ten operations, for 1,600 complete inspections and 7,200 normalized
partition states.

The fixture is the immutable Confluent Local 7.5.0 image and reported
`7.5.0-ccs` at runtime. The host was Darwin arm64 on an Apple M4 Max with Go
1.26.5 and Docker Desktop engine 29.6.2. Exact module versions, the execution
revision, and every harness input hash are recorded in the environment files.

## Correctness boundary

Before capture, a race-enabled real-broker check passed for both workloads.
Replay independently proved exact offsets, keys, and values for `[1,3)`.
Inspection proved exact four-client agreement for three sorted partitions,
including leader epochs, replicas, ISR, offline replicas, beginning and end
offsets, `min.insync.replicas`, cleanup policy, and every other configuration
field included in the timed contract.

These healthy single-node results do not prove replay behavior under retention
gaps, compaction, truncation, cancellation, or side-effect failure. They do not
prove inspection behavior under authorization denial, partial multi-topic
failure, controller loss, or leader failover. Separate package and fault
fixtures own those claims.

## Files

- [`environment-replay.txt`](environment-replay.txt) binds replay results to
  the exact Go, host, Docker, broker, dependency, execution revision, and
  harness inputs.
- [`raw-replay.txt`](raw-replay.txt) contains all 640 unmodified replay
  samples and the runtime broker assertion.
- [`replay-benchstat.txt`](replay-benchstat.txt) preserves replay latency,
  throughput, record-count, byte, and allocation distributions.
- [`environment-inspection.txt`](environment-inspection.txt) binds inspection
  results to the same complete input identity.
- [`raw-inspection.txt`](raw-inspection.txt) contains all 160 unmodified
  inspection samples and the runtime broker assertion.
- [`inspection-benchstat.txt`](inspection-benchstat.txt) preserves inspection
  latency, partition-count, byte, and allocation distributions.

## Commands

Every command ran with its own fresh isolated `GOCACHE`, inherited by all
children:

```sh
go test -race -tags=integration -run '^TestEquivalent(Replay|Inspection)Outcomes$' -count=1 -timeout=10m ./...
make environment > environment-replay.txt
make capture OUTPUT=raw-replay.txt BENCH_PATTERN='^BenchmarkEquivalentReplay$$' BENCH_COUNT=20 BENCH_TIME=1x
make analyze INPUT=raw-replay.txt > replay-benchstat.txt
make environment > environment-inspection.txt
make capture OUTPUT=raw-inspection.txt BENCH_PATTERN='^BenchmarkEquivalentInspection$$' BENCH_COUNT=20 BENCH_TIME=10x
make analyze INPUT=raw-inspection.txt > inspection-benchstat.txt
```

The replay process passed in 368.004 seconds. Every sample reported exactly
10 or 100 records per operation. The inspection process passed in 29.847
seconds. Every sample reported exactly one or eight partitions per operation.

## Interpretation

Replay medians for the policy ranged from 7.855 to 30.53 milliseconds and raw
franz-go from 9.706 to 31.90 milliseconds. kafka-go medians ranged from 391.9
to 432.0 milliseconds and Sarama from 613.9 to 635.1 milliseconds because the
contract includes each public direct reader's complete construction and
shutdown lifecycle. Policy latency distributions spread as far as 45 percent
and raw franz-go as far as 44 percent.

Inspection medians ranged from 3.472 to 5.330 milliseconds for the policy,
3.005 to 4.309 milliseconds for raw franz-go, 4.505 to 4.810 milliseconds for
kafka-go, and 4.960 to 6.383 milliseconds for Sarama. One policy distribution
spans 170 percent on the shared local fixture.

These distributions describe the exact noisy local environment. They are not
stable budgets and do not establish client superiority. The policy path's
validation, sorting, and defensive copies are part of its public contract.

## Artifact digests

```text
6f6e278629292842b85890b7b86c210691414752bb399bf5023c34be48fe5758  environment-inspection.txt
6f6e278629292842b85890b7b86c210691414752bb399bf5023c34be48fe5758  environment-replay.txt
8cbb9ada06dfb8033b5a0d688e4207a786c4a96cf9f017019941e07fefde512d  inspection-benchstat.txt
03b07fff85a9bf377c52a348866515c9e09339ac10da3718c06eb1c8418ffcb6  raw-inspection.txt
f642451b6deefc063b619e361d9dd76b5ea96f416163ff5b29617d9ddfc03003  raw-replay.txt
0f94670dbba3d0ada39e01e2b44d6684b0fb5ba5b113773d8832ddba9748130c  replay-benchstat.txt
```
