# Delivery hardening evidence

This module owns the mapping between event-sourcing deliveries and the
first-party queue seam. It does not own backend transactions, worker lifecycle,
dead-letter storage, or application effects. Evidence therefore distinguishes
adapter outcomes from backend and consumer responsibilities.

## Wire compatibility

The only supported wire version is
`golib.event-sourcing.queue.v1`. Two exact golden payloads freeze:

- every field populated in replay mode, including sorted metadata and every
  optional identity; and
- live mode with every optional field absent.

Successful decode is canonical: re-encoding must reproduce the exact input
bytes. The decoder rejects every unsupported version rather than applying a
fallback. There is no supported prior version because v1 is the first released
format.

The fuzz corpus deliberately seeds:

- every truncation offset of the minimal canonical envelope;
- duplicate and unknown fields;
- invalid UTF-8 and trailing bytes;
- negative, exponent-overflow, and out-of-range numbers;
- non-UTC, non-microsecond, and malformed timestamps;
- metadata presence and hostile strings;
- invalid base64 and envelope-size boundaries; and
- unsupported versions.

One dispatcher fuzz target normalizes every retry and timeout field plus
enqueue time, retry policy, handler type, tags, trace identity, producer
version, correlation, and trace context into valid policy. It requires
construction and dispatch to succeed, then proves the queue receives the exact
owned policy and a decodable delivery after caller storage changes. A separate
invalid-policy target injects underflow, overflow, non-finite factors,
inconsistent backoff bounds, invalid timeout and enqueue time, and oversized
metadata and requires `ErrInvalidJobOption`. A payload-limit target exercises
valid encoded deliveries immediately below, at, and above their configured
codec boundary.

## Publication outcomes

The adapter knows only whether a queue call returned success:

| Observation | Acceptance | Safe conclusion |
| --- | --- | --- |
| Cancellation, replay policy, encode, or wrapper limit stops before `Queue`. | Not attempted | This adapter did not publish the stopping delivery. |
| `Queue` returns nil. | Succeeded call | The backend reported success; durability still follows backend policy. |
| `Queue` returns timeout, cancellation, disconnect, shutdown, or any other error. | Unknown | The backend may have accepted the delivery before returning the error. |
| `Queue` panics. | Unknown | The panic is contained, but acceptance cannot be reconstructed. |

Cancellation after admission cannot interrupt an in-progress queue call because
the compatible producer interface has no context parameter. A focused test
holds that call open, cancels the dispatch context, and proves dispatch waits
for the queue outcome.

Retrying an unknown outcome is intentionally allowed but can publish identical
canonical bytes twice. The consumer must deduplicate by `message_id`.

## Process-death and duplicate windows

Digest-pinned Valkey Streams subprocess tests terminate the worker process with
`os.Exit`; they do not use graceful queue release.

| Death or failure window | Broker result | Application result |
| --- | --- | --- |
| Before the consumer performs its effect and before settlement. | The pending delivery is reclaimed. | The recovery worker performs the first effect. |
| After the effect but before handler return or settlement. | The pending delivery is reclaimed. | The effect is attempted again; a durable `message_id` record prevents duplication. |
| After handler success but before acknowledgement persists. | The pending delivery is reclaimed. | Treat this as the same duplicate window as post-effect/pre-settlement. |
| After acknowledgement persists. | That Valkey record is not redelivered to the group. | Other duplicate sources, including ambiguous publication, still require idempotency. |
| Dead-letter append fails before source acknowledgement. | The source remains pending and is reclaimed; a later append can succeed. | Do not treat a failed dead-letter operation as terminal delivery loss. |

The process-death proof uses a task-owned durable effect marker. Separate
subprocess modes exit inside the consumer and immediately after
`TaskHandler.Handle` returns but before queue settlement. Recovery observes the
same message ID and does not repeat the effect.

## Backend and ordering boundary

Supported evidence covers the in-memory Ring contract and Valkey Streams 9.1.0.
Valkey ordering is tested with interleaved aggregate streams through one Valkey
stream, one group, and one consumer. Message ID, aggregate type and ID, stream
version, and partition survive in enqueue order.

This does not claim global order under multiple streams, consumers, retries,
reclaim, dead-letter replay, cluster topology, or another backend. Other
first-party queue implementations are interface-compatible candidates, not
backend-interoperability claims for this module.

## Concurrency and ownership

Race evidence shares one codec, dispatcher, and handler across concurrent
calls. The queue retains queued messages and job options after dispatch.
Assertions prove:

- every `Bytes` call returns independent storage;
- each dispatch owns retry, tag, correlation, and trace maps;
- task storage can change after handling without changing the decoded
  delivery; and
- concurrent callbacks do not retain shared mutable codec state.

The adapter starts no goroutines and has no close operation. A supported Ring
test races shared dispatcher calls with admission close and bounded release,
then proves every accepted delivery is handled and every rejected call remains
explicitly ambiguous. Valkey publisher shutdown and worker process-death tests
exercise the durable backend's sequential lifecycle and recovery boundaries.

## Diagnostics

Normal, detailed, Go-syntax, string, and quoted formatting of public adapter
errors contains only the stable category. Payloads, metadata, backend errors,
panic values, and broker credentials remain absent. Wrapped causes remain
available to `errors.Is` and `errors.As` for deliberate classification and
must not be logged separately by applications.

## Benchmarks

Codec encode and decode, dispatcher overhead, and handler overhead run without a
broker. The durable benchmark compares raw Valkey publication with
codec-plus-dispatcher publication using the same digest-pinned Valkey image,
append-only persistence, payload, stream limits, command timeout, and client
settings.

Run:

```sh
go test -run '^$' -bench 'Benchmark(Codec|Dispatcher|TaskHandler)' -benchmem
go test -tags=integration -run '^$' -bench BenchmarkValkeyDurablePublication -benchmem
```

The repository benchmark evidence records Go version, operating system,
architecture, CPU, operation latency, bytes, and allocations. Broker results
include local Docker and network scheduling and must not be interpreted as a
portable service-level objective. Compare repeated samples with `benchstat`
before claiming a regression or improvement.

The 2026-08-11 hardening sample used Go 1.26.5 on darwin/arm64, an Apple M4
Max, five samples, 100 operations per broker-free sample, and 20 operations per
durable sample. The table reports the median latency and the complete observed
range; allocations are the median:

| Workload | Median | Observed range | Bytes/op | Allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Codec encode | 1.729 us/op | 1.455-4.190 us/op | 1,048 | 2 |
| Codec decode | 5.467 us/op | 3.461-7.042 us/op | 2,632 | 30 |
| Dispatcher to an in-process retaining sink | 1.860 us/op | 1.717-3.732 us/op | 3,005 | 12 |
| Handler decode plus no-op consumer | 3.881 us/op | 3.349-6.818 us/op | 2,632 | 30 |
| Raw durable Valkey publication | 2.982 ms/op | 1.113-3.556 ms/op | 28,237 | 189 |
| Codec plus dispatcher durable Valkey publication | 4.115 ms/op | 1.977-9.679 ms/op | 30,472 | 198 |

The durable variance is intentionally published rather than hidden. These
samples ran on a developer workstation with concurrent load, so they establish
separate workloads and allocation visibility, not a release threshold or a
statistically significant performance difference.
