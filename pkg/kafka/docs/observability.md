# Observability hooks

The root module exposes a vendor-neutral `ObserverPolicy`. The current surface
reports producer delivery plus consumer poll, record-handler, batch-handler,
and offset-commit completion. It does not expose franz-go hooks, records,
requests, responses, or clients.

## Configuration and execution

`ProducerConfig.Observers` and `ConsumerConfig.Observers` accept 1 to 16
ordered `ObserverFunc` callbacks. The callback slice is copied during client
construction. A non-empty policy requires an explicit
`ObservationFailureFunc`; observation failures never change producer delivery,
handler, commit, or poll results.

The default callback budget is 100 milliseconds. Explicit values must be from
1 millisecond through 5 seconds. All observers and failure callbacks for one
event execute synchronously in registration order and share that single
budget. Producer operations and consumer partition workers can invoke the same
observer concurrently, so callbacks must be concurrency-safe.

When an observer fails, its failure handler runs immediately before the next
registered observer.

Go cannot forcibly stop an arbitrary callback. The package supplies a derived
deadline and detects a cooperative late return, but a callback that ignores its
context can block the calling operation indefinitely. For asynchronous
production, franz-go completes its promise synchronously; a blocked observer
therefore also delays the delivery channel and can delay flush or shutdown.
Consumer handler and commit observations run before the poll releases
franz-go's rebalance gate so borrowed record metadata remains valid. A blocked
consumer observer therefore extends poll processing and can delay a rebalance.
Observers must perform only bounded in-process work and hand off to their own
explicitly bounded infrastructure when export cannot complete immediately.

The callback context retains caller values for correlation but is detached
from caller cancellation and receives the observer-policy deadline. This lets
an asynchronous final outcome remain observable after the caller stops
waiting. The context is callback-scoped and must not be retained.

## Event contract

`Observation.Kind` is one of:

- `ObservationProduceRecord` after `PublishRecord` resolves;
- `ObservationProduceBatch` after the complete input-ordered batch resolves;
- `ObservationProduceAsync` immediately before the final delivery is made
  available on the result channel;
- `ObservationConsumeRecord` after one record-processing attempt;
- `ObservationConsumeBatch` after one partition-batch processing attempt;
- `ObservationConsumeCommit` after one contiguous offset-commit attempt; or
- `ObservationConsumePoll` after the complete bounded poll cycle and before
  its rebalance gate is released.

Every observation contains its copied client ID, start time, elapsed duration,
success flag, and stable failure category. Consumer events also contain the
copied group ID. Successful events use `ErrorUnknown` because no failure
category applies.

| Kind | Count and coordinate meaning |
| --- | --- |
| Produce record/async | `RecordCount=1`; successful delivery has partition, offset, and broker timestamp |
| Produce batch | `RecordCount` is the bounded input count; coordinates are omitted |
| Consume record | `RecordCount=1`; `ProcessedCount=1` only after handler success; validated source partition and offset are present |
| Consume batch | `RecordCount` is the partition-batch size; `ProcessedCount` equals it only after handler success; offset is the batch's last source offset |
| Consume commit | `RecordCount` and `ProcessedCount` are the contiguous records represented by the commit; `PartitionCount` is the number of submitted partition offsets; `CommittedCount` is zero on commit failure |
| Consume poll | `RecordCount`, `ProcessedCount`, and `CommittedCount` match the returned `PollResult` while within policy bounds; `PartitionCount` counts validated fetched topic-partitions |

`RecordBytes` is a conservative policy-size estimate rather than Kafka's
encoded wire size. Topic is copied only for validated single-topic metadata;
mixed-topic operations omit it so an adapter cannot accidentally fan one
operation into an unbounded topic list. Invalid fetched metadata also omits
topic, coordinates, partition count, and bytes rather than copying
broker-controlled diagnostic data.
Consumer errors exposing a valid `Category() ErrorCategory` preserve that
classification. An invalid or panicking category implementation is contained
and reported as `ErrorPermanent`; panic values are discarded.
If a backend violates `MaxPollRecords`, the consumer returns
`ErrTooManyFetchedRecords`; the observation clips `RecordCount` to that
configured maximum and sets `Truncated` instead of exporting an unbounded
count. `PollResult.Polled` retains the actual rejected count for direct caller
handling.

Observations never contain keys, values, headers, credentials, broker URLs,
application error text, or franz-go values. Adapter cardinality remains an
adapter and deployment decision. Topics and consumer groups must not become
metric dimensions without explicit bounded allowlists; keys and arbitrary
headers must never be attributes by default.

## Failures and reentrancy

An observer-returned error is sent to the configured failure handler as an
`ObservationFailure`. Its normal formatting is redacted. `Cause` deliberately
returns the application-owned error for local classification and must not be
exported without application redaction. Observer panic values are discarded
and represented only by `ErrObserverPanic`. A panic in the failure handler is
contained and discarded because recursively reporting reporter failure would
be unbounded.

Observers must not call the client that invoked them. Producer and consumer
operations using the callback context fail with `ErrObserverReentry`.
Producer and consumer `Close`, plus context-free mutating consumer operations,
also fail with that error while a callback is active. This conservative fence
can reject a concurrent context-free call from another goroutine while an
observer is running. Replacing the callback context to bypass the fence
violates the contract and can deadlock lifecycle work. The package holds no
producer or consumer lifecycle lock while application observer code runs.

## Current boundary

The root observer model currently covers producer delivery and nontransactional
consumer processing and commits. Broker connection, authentication, request,
throttle, retry, rebalance, transaction-lifecycle, replay, inspection, health,
and shutdown events remain unimplemented. Transaction-processor consumer
events are also not yet emitted. The planned
`kafka/adapters/gotelemetry` nested module must translate only stable root
observations and pin a reviewed OpenTelemetry messaging semantic-convention
version; OpenTelemetry will not become a root dependency.
