# Equivalent Kafka replay, inspection, rebalance, and resource captures, 2026-07-31

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

`BenchmarkEquivalentConsumerRebalance` compares the package policy, raw
franz-go, and Sarama under one cooperative-sticky two-partition contract. One
stable member remains in the group. Each operation constructs a second public
client, joins it, handles and commits one record, and waits for broker
inspection to prove a stable one-partition-per-member assignment. It then
closes that client and waits until the first member stably owns both partitions
again. Ten samples per client report the join-through-commit-and-stability
boundary, the close-through-stability boundary, and their total. Kafka-go is
excluded because v0.4.51 does not expose cooperative-sticky assignment.

`BenchmarkEquivalentInspectionReconnect` compares one warmed stable inspector
for each client after the same owned broker is stopped, an inspection fails
under a two-second context, and the broker restarts at the exact same endpoint.
The measured boundary contains only the post-restart operation through exact
recovery of the pre-failure three-partition state. Ten samples per client
record reconnect latency, allocations, and allocated bytes. The 40 samples
were checkpointed as eight five-sample shards so an interruption cannot discard
completed candidates.

`BenchmarkEquivalentInspectionIdleResources` compares one warmed stable
three-partition inspector over a fixed 500-millisecond interval with no
application request. Ten samples per client record garbage-collected heap and
object deltas, goroutine deltas, active and verified closed connections, and
Go runtime user, GC, and scavenger CPU. Every sample proves shutdown returns
all observed broker connections to zero. The capture contains 40 idle samples.

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
Reconnect correctness proved the same normalized state before and after a real
broker restart for every client. Idle-resource correctness proved each warmed
client held broker connections and returned every observed connection to zero;
the focused race run passed both resource contracts.
Rebalance correctness independently proved the initial one-member
two-partition assignment, the stable two-member one-partition-each assignment,
exact handling through the joining member, and restoration after it left. Its
focused race run passed before capture.

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
- [`environment-rebalance.txt`](environment-rebalance.txt) binds the
  cooperative-sticky results to the exact execution revision and harness
  inputs.
- [`raw-rebalance.txt`](raw-rebalance.txt) contains all 30 unmodified
  rebalance samples and the runtime broker assertion.
- [`rebalance-benchstat.txt`](rebalance-benchstat.txt) preserves join, leave,
  total, and partition-count distributions.
- [`environment-resources.txt`](environment-resources.txt) binds reconnect and
  idle results to their execution revision and complete harness input identity.
- `raw-resources-reconnect-{policy,franz,kafka-go,sarama}-{01,02}.txt`
  preserves ten unmodified restart samples per client in durable five-sample
  checkpoints.
- [`raw-resources-idle.txt`](raw-resources-idle.txt) preserves all 40
  unmodified idle-resource samples.
- [`resources-reconnect-benchstat.txt`](resources-reconnect-benchstat.txt) and
  [`resources-idle-benchstat.txt`](resources-idle-benchstat.txt) preserve the
  combined resource distributions.

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
go test -race -tags=integration -run '^TestEquivalentConsumerRebalanceOutcomes$' -count=1 -timeout=15m ./...
make environment > environment-rebalance.txt
make rebalance-capture OUTPUT=raw-rebalance.txt REBALANCE_PATTERN='^BenchmarkEquivalentConsumerRebalance$$' REBALANCE_COUNT=10 REBALANCE_TIME=1x
go tool benchstat raw-rebalance.txt > rebalance-benchstat.txt
go test -race -tags=integration -run '^TestEquivalentInspection(Reconnect|IdleResource)Outcomes$' -count=1 -timeout=25m ./...
make environment > environment-resources.txt
make resource-capture OUTPUT=raw-resources-reconnect-policy-01.txt RESOURCE_PATTERN='^BenchmarkEquivalentInspectionReconnect$$/^golib-policy$$' RESOURCE_COUNT=5 RESOURCE_TIME=1x
make resource-capture OUTPUT=raw-resources-reconnect-policy-02.txt RESOURCE_PATTERN='^BenchmarkEquivalentInspectionReconnect$$/^golib-policy$$' RESOURCE_COUNT=5 RESOURCE_TIME=1x
make resource-capture OUTPUT=raw-resources-reconnect-franz-01.txt RESOURCE_PATTERN='^BenchmarkEquivalentInspectionReconnect$$/^raw-franz-go$$' RESOURCE_COUNT=5 RESOURCE_TIME=1x
make resource-capture OUTPUT=raw-resources-reconnect-franz-02.txt RESOURCE_PATTERN='^BenchmarkEquivalentInspectionReconnect$$/^raw-franz-go$$' RESOURCE_COUNT=5 RESOURCE_TIME=1x
make resource-capture OUTPUT=raw-resources-reconnect-kafka-go-01.txt RESOURCE_PATTERN='^BenchmarkEquivalentInspectionReconnect$$/^kafka-go$$' RESOURCE_COUNT=5 RESOURCE_TIME=1x
make resource-capture OUTPUT=raw-resources-reconnect-kafka-go-02.txt RESOURCE_PATTERN='^BenchmarkEquivalentInspectionReconnect$$/^kafka-go$$' RESOURCE_COUNT=5 RESOURCE_TIME=1x
make resource-capture OUTPUT=raw-resources-reconnect-sarama-01.txt RESOURCE_PATTERN='^BenchmarkEquivalentInspectionReconnect$$/^sarama$$' RESOURCE_COUNT=5 RESOURCE_TIME=1x
make resource-capture OUTPUT=raw-resources-reconnect-sarama-02.txt RESOURCE_PATTERN='^BenchmarkEquivalentInspectionReconnect$$/^sarama$$' RESOURCE_COUNT=5 RESOURCE_TIME=1x
make resource-capture OUTPUT=raw-resources-idle.txt RESOURCE_PATTERN='^BenchmarkEquivalentInspectionIdleResources$$' RESOURCE_COUNT=10 RESOURCE_TIME=1x
go tool benchstat -col '' raw-resources-reconnect-*.txt > resources-reconnect-benchstat.txt
go tool benchstat raw-resources-idle.txt > resources-idle-benchstat.txt
```

The replay process passed in 368.004 seconds. Every sample reported exactly
10 or 100 records per operation. The inspection process passed in 29.847
seconds. Every sample reported exactly one or eight partitions per operation.
The rebalance process passed in 76.446 seconds. All 30 samples reported two
partitions and completed both exact stable-assignment boundaries.
The eight reconnect checkpoint processes passed in 1,175.999 aggregate
seconds, and every sample recovered all three normalized partitions. The idle
capture passed in 35.859 seconds, and every sample proved that all connections
active at the measured point returned to zero.

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

Complete cooperative-sticky rebalance medians were 1.516 seconds for the policy
path, 1.509 seconds for raw franz-go, and 2.189 seconds for Sarama. The
construction-through-commit-and-stability medians were 1.010, 1.004, and 1.132
seconds respectively. Close-through-one-member-stability medians were 506.1
milliseconds, 507.7 milliseconds, and 1.053 seconds. Bounded broker group
inspection is included because it establishes the exact stable boundary;
topic creation, input production, first-member setup, and final shutdown are
excluded.

Reconnect medians were 19.66 seconds for the policy, 19.53 seconds for raw
franz-go, 19.27 seconds for kafka-go, and 19.42 seconds for Sarama, with
12-to-16-percent distributions. Median reconnect allocations were 10.87k for
the policy, 11.33k for raw franz-go, 39.06k for kafka-go, and 16.59k for
Sarama. Median allocated bytes were 593.0 KiB, 606.2 KiB, 4.292 MiB, and
6.076 MiB respectively. These values include each client's retry/backoff and
four complete inspection protocol operations after broker readiness; broker
downtime and Docker lifecycle work are excluded.

The idle point-in-time medians were two active and verified closed
connections, three goroutines, 30.03 KiB, and 167.5 heap objects for the
policy; two connections, three goroutines, 29.20 KiB, and 152 objects for raw
franz-go; four connections, five goroutines, 164.6 KiB, and 140 objects for
kafka-go; and one connection, three goroutines, 154.6 KiB, and 258.5 objects
for Sarama. The direct Go runtime user, GC, and scavenger counters reported
zero CPU nanoseconds per second for every 500-millisecond sample, which means
those classes accumulated no observable CPU at the metric's resolution; it is
not a claim of physically zero process, system, or kernel CPU.

These distributions describe the exact noisy local environment. They are not
stable budgets and do not establish client superiority. The policy path's
validation, sorting, and defensive copies are part of its public contract.

## Artifact digests

```text
6f6e278629292842b85890b7b86c210691414752bb399bf5023c34be48fe5758  environment-inspection.txt
c8529c02f7827bb86c08be6b5ef0203c2d1716000fcd504d915b870c8ea3cef4  environment-rebalance.txt
6f6e278629292842b85890b7b86c210691414752bb399bf5023c34be48fe5758  environment-replay.txt
8cbb9ada06dfb8033b5a0d688e4207a786c4a96cf9f017019941e07fefde512d  inspection-benchstat.txt
03b07fff85a9bf377c52a348866515c9e09339ac10da3718c06eb1c8418ffcb6  raw-inspection.txt
9ff2e47d6783c350add4e033c05930509ac427bd81509f63b41c1731d87b627a  raw-rebalance.txt
f642451b6deefc063b619e361d9dd76b5ea96f416163ff5b29617d9ddfc03003  raw-replay.txt
2988afa954690f55e744d0ee32737dd42b9b42d04eebcedee4c78f2a3ce44eed  rebalance-benchstat.txt
0f94670dbba3d0ada39e01e2b44d6684b0fb5ba5b113773d8832ddba9748130c  replay-benchstat.txt
70416879767ca17391118f82b445b640d338b6d10b025b7d1a1e8a29329b6905  environment-resources.txt
6c37a943131f6521337381220cb698338414f99b9311267c9ae662f92221007f  raw-resources-idle.txt
99474d469dcbf4430db93ed825c21bd1590459d49791394df67cc9f6d875ae00  raw-resources-reconnect-franz-01.txt
a88f44e19621073ba66a8229558782ed1f9648b9e43257f8959a87b95ab30d84  raw-resources-reconnect-franz-02.txt
351fdfa66831489d7a56f900e75cfb8a5e33867dd2826a07a0bfe85ce3466753  raw-resources-reconnect-kafka-go-01.txt
3569e9f28e03bdff18090b3f413bb7946c9000e195bac56e971eb4ecc07b63da  raw-resources-reconnect-kafka-go-02.txt
db837d578cc6585cf2a3bde4b8001d8d9e7f770cb814898d52dd64e48d7b6d1a  raw-resources-reconnect-policy-01.txt
e6cb81e3faf8106b8ec60f5a9f9e89ae9715232bec735f768e23c8c7e0da1042  raw-resources-reconnect-policy-02.txt
a8506bae84224b3ddfa5a5da48fe660e5e6fbb327564f20ab40e28974fd0107f  raw-resources-reconnect-sarama-01.txt
08a2c4109e244741dff19a6ddac55fdf0faba582e5acd343ae7fe880891c95f1  raw-resources-reconnect-sarama-02.txt
85698cba80260432dd358ffa915da2d195a5819b2fecc77a0ed1dff6303d36d8  resources-idle-benchstat.txt
ff9e4f80c29f594eca3cca8efd35e4774f0f7b092662e3b072f087918fdc1124  resources-reconnect-benchstat.txt
```
