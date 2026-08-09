# Kafka producer policy-overhead capture, 2026-08-10

This directory publishes one bounded local no-I/O capture. It isolates the
root producer policy from Kafka broker, network, serialization, compression,
retry, and storage latency. It is not a franz-go throughput comparison or a
production regression budget.

## Workloads

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

## Results

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

## Environment and reproducibility

- execution base revision: `530dbc1d6576d7b5e05f8593144785538faf53d3`;
- root production-source digest:
  `bee301d85cdc8ac99eb3055a9485cbec515263eaf24b22f08b8d19d7f51a05ea`;
- benchmark source digest:
  `2c0e668b38e1dcd6974bfcbc2b20e833ae6d695b76f8f92914eb48addf4160f0`;
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
```

`raw-policy-overhead.txt` contains the raw Go benchmark output.
`policy-overhead-benchstat.txt` contains `go tool benchstat` analysis.

SHA-256:

```text
34ac4a371c46273f4e2fdbb13fb50ceced6d9fdf18bf35f70a39714869cd93fb  raw-policy-overhead.txt
f3da584435d6d54d8ac6a9f1fe92584ba58a4991dd0ed510908bcf6e9e13e79c  policy-overhead-benchstat.txt
```

## Remaining boundary

This closes the root producer single, batch, asynchronous API, transaction,
and observer no-I/O decomposition. Existing microbenchmarks separately cover
consumer failure policy, partition workers, replay progress, inspection, and
adapters. A common no-I/O end-to-end decomposition for consumer-group polling
and consume-transform-produce remains outstanding.
