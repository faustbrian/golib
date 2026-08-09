# Kafka policy-overhead capture, 2026-08-10

This directory publishes bounded local no-I/O producer, consumer-group, and
consume-transform-produce captures. They isolate root policy from Kafka broker,
network, group coordination, serialization, compression, retry, and storage
latency. They are not franz-go throughput comparisons or production regression
budgets.

## Producer workloads

`BenchmarkProducerPolicyOverhead` runs every candidate through the same
synchronous in-memory transport seam and reports three boundaries:

- `transport-floor` calls that seam directly with an already owned
  `kgo.Record`;
- `policy` calls the public root policy, including validation, topic
  allowlisting, defensive byte ownership, franz-go record mapping, lifecycle
  fencing, bounded delivery contexts, and result normalization; and
- `policy-observed` adds one successful synchronous root observer.

The matrix contains:

- single-record production with 128-byte, 1 KiB, and 64 KiB values;
- 10-record and 100-record synchronous batches with 128-byte and 1 KiB values;
- 10-record and 100-record asynchronous API windows with 128-byte and
  1 KiB values; and
- one-record and 10-record Kafka producer transactions with 1 KiB values.

Every record has a non-empty key, two ordered headers, one allowed topic, and
an event timestamp. The transport returns one successful partition-zero,
offset-one delivery per admitted record. Benchmark assertions reject missing,
failed, or inconsistent deliveries.

The transport floor is deliberately smaller than raw franz-go: it measures
only the common no-I/O callback boundary. It excludes franz-go buffering,
serialization, partitioning, request construction, and network work. The
separate real-broker client harness remains authoritative for equivalent
client comparisons.

## Producer results

Ten 100-millisecond samples produced 390 benchmark result samples. Median
policy time was 454 nanoseconds for a 128-byte single record, 536 nanoseconds
for a 1 KiB single record, and 5.89 microseconds for a 64 KiB single record.
The corresponding observed medians were 803 nanoseconds, 1.19 microseconds,
and 6.44 microseconds.

Policy batch medians ranged from 2.54 microseconds for ten 128-byte records to
43.5 microseconds for one hundred 1 KiB records. Policy asynchronous API-window
medians ranged from 6.05 to 71.6 microseconds. One-record and ten-record policy
transaction medians were 1.00 and 7.78 microseconds. Allocation counts were
stable across all ten samples and retain the package's required caller-byte
ownership cost.

Latency distributions spread as far as 97 percent on the shared development
host. In a few noisy batch cases the observed median was lower than the
unobserved median. That is measurement variance, not evidence that observation
improves performance. These latency results are descriptive only; no CI
threshold or superiority claim follows from them.

## Consumer and consume-transform-produce workloads

`BenchmarkConsumerPolicyOverhead` measures a complete successful poll,
handler, contiguous settlement, commit, and rebalance-release cycle through
the same static no-I/O fetched records and backend. It covers:

- record handling for one, ten, and one hundred 1 KiB records in one partition;
- sequential and four-worker handling of one hundred records across four
  independent partitions;
- partition-batch handling for ten and one hundred 1 KiB records; and
- sequential and four-worker batch handling across four partitions.

`BenchmarkTransactionProcessorPolicyOverhead` measures complete successful
one-, ten-, and one-hundred-record source polls. Each 1 KiB source record
publishes one 1 KiB output before the source offsets and outputs commit through
the no-I/O Kafka transaction boundary.

Both matrices report the direct `transport-floor`, root `policy`, and
`policy-observed` boundaries. The observer case adds one successful synchronous
root observer. Benchmark assertions require the exact polled, processed,
published, settled, and committed outcomes.

## Consumer and consume-transform-produce results

Ten 100-millisecond samples produced 360 benchmark result samples. Record-mode
policy medians were 1.07 microseconds for one record, 8.43 microseconds for ten
records, and 56.8 microseconds for one hundred records in one partition. One
hundred records across four partitions measured 54.6 microseconds sequentially
and 73.9 microseconds with four partition workers. Corresponding observed
medians ranged from 2.22 to 132 microseconds.

Batch-mode policy medians were 1.96 microseconds for ten records and 9.56
microseconds for one hundred records in one partition. One hundred records
across four partitions measured 12.0 microseconds sequentially and 19.2
microseconds with four partition workers. Corresponding observed medians ranged
from 4.50 to 47.1 microseconds.

Consume-transform-produce policy medians were 1.42, 8.64, and 75.6 microseconds
for one, ten, and one hundred source/output pairs. Corresponding observed
medians were 2.07, 8.86, and 77.4 microseconds. Allocation counts were stable
across all ten samples for every workload.

Latency distributions spread as far as 354 percent on the shared development
host. These latency results are descriptive only. The extreme transport-floor
throughput values describe reused in-memory records, not Kafka, franz-go, or
application throughput, and no superiority or CI-budget claim follows from
them.

## Environment and reproducibility

- producer execution base revision:
  `530dbc1d6576d7b5e05f8593144785538faf53d3`;
- consumer and consume-transform-produce execution base revision:
  `896f4bedc2191fbb28eb81118a976b23326995d2`;
- root production-source digest:
  `bee301d85cdc8ac99eb3055a9485cbec515263eaf24b22f08b8d19d7f51a05ea`;
- producer benchmark source digest:
  `2c0e668b38e1dcd6974bfcbc2b20e833ae6d695b76f8f92914eb48addf4160f0`;
- consumer and consume-transform-produce benchmark source digest:
  `64878dde2f14ed629789593e27ff59989f128d511f387defed1880998389e1c4`;
- Go 1.26.5 on Darwin arm64, macOS 27.0 build `26A5388g`;
- Apple M4 Max; and
- franz-go v1.21.5.

The benchmark source was uncommitted at the execution base revision, so the
source digests, not the Git revision alone, bind the measured inputs. No
container, broker, Docker engine, authentication provider, or external
service participated.

The capture used a fresh disposable Go build cache:

```sh
agent_gocache=$(mktemp -d "${TMPDIR:-/tmp}/golib-gocache.XXXXXX")
trap 'find "$agent_gocache" -depth -delete 2>/dev/null || true' EXIT HUP INT TERM
export GOCACHE="$agent_gocache"

go test -run '^$' \
  -bench '^BenchmarkProducerPolicyOverhead$' \
  -benchmem -benchtime=100ms -count=10

go test -run '^$' \
  -bench '^BenchmarkConsumerPolicyOverhead/record/' \
  -benchmem -benchtime=100ms -count=10

go test -run '^$' \
  -bench '^BenchmarkConsumerPolicyOverhead/batch/' \
  -benchmem -benchtime=100ms -count=10

go test -run '^$' \
  -bench '^BenchmarkTransactionProcessorPolicyOverhead/' \
  -benchmem -benchtime=100ms -count=10
```

`raw-policy-overhead.txt` contains the raw Go benchmark output.
`policy-overhead-benchstat.txt` contains `go tool benchstat` analysis.
`raw-consumer-transaction-policy-overhead.txt` contains the combined raw
consumer and consume-transform-produce output.
`consumer-transaction-policy-overhead-benchstat.txt` contains its
`go tool benchstat` analysis.

SHA-256:

```text
34ac4a371c46273f4e2fdbb13fb50ceced6d9fdf18bf35f70a39714869cd93fb  raw-policy-overhead.txt
f3da584435d6d54d8ac6a9f1fe92584ba58a4991dd0ed510908bcf6e9e13e79c  policy-overhead-benchstat.txt
1c2362b98881c6060244a3172ed3c1843503e183cb8ae1944732560d04f66467  raw-consumer-transaction-policy-overhead.txt
24c0fd548dd57c865c65782eec928191ac561b4f0343521eacacf3834684a230  consumer-transaction-policy-overhead-benchstat.txt
```

## Remaining boundary

This closes the common root no-I/O decomposition for producer single, batch,
asynchronous API, and transaction operations; consumer-group record and batch
polling with sequential and cross-partition parallel handling; Kafka
consume-transform-produce; and optional root observation. Existing
microbenchmarks separately cover consumer failure policy, replay progress,
inspection, and adapters. Release comparisons still require external identity
provider costs and a previous package release after one exists.
