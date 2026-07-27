# Observability hooks

The root module exposes a vendor-neutral `ObserverPolicy`. The current surface
reports producer delivery plus consumer poll, record-handler, batch-handler,
offset-commit, assignment, revocation, ownership-loss, blocked-rebalance, group
management error, broker connection, broker request, broker throttle, and
broker disconnect completion. The franz-go hook bridge is private;
observations do not expose franz-go hooks, records, requests, responses,
clients, network connections, broker endpoints, or raw group errors.

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
Broker events execute on franz-go's internal connection and request goroutines
and can therefore overlap every producer or consumer operation. A consumer can
start topic-metadata work inside franz-go client construction, so broker
callbacks may run after configuration validation but before `NewConsumer`
returns. They must not depend on assignment of the constructor result.

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
Assignment, revocation, and loss observers run after package ownership state is
updated and after its lock is released. The blocked-rebalance observer runs
after active handler cancellation has been signaled. franz-go invokes that
blocked callback in its own goroutine, and lifecycle observations can overlap
handler, broker, and other observer callbacks.
Observers must perform only bounded in-process work and hand off to their own
explicitly bounded infrastructure when export cannot complete immediately.

Policy-operation callback contexts retain caller values for correlation but are
detached from caller cancellation and receive the observer-policy deadline.
This lets an asynchronous final outcome remain observable after the caller
stops waiting. Broker callbacks derive from a background context because a
connection can serve many operations and outlive their contexts; they therefore
carry no caller values. Consumer-group lifecycle callbacks also derive from a
background context because they belong to a group session rather than one
application operation; franz-go callback context values are not exposed.
Every callback context is callback-scoped and must not be retained.

## Event contract

`Observation.Kind` is one of:

- `ObservationProduceRecord` after `PublishRecord` resolves;
- `ObservationProduceBatch` after the complete input-ordered batch resolves;
- `ObservationProduceAsync` immediately before the final delivery is made
  available on the result channel;
- `ObservationConsumeRecord` after one record-processing attempt;
- `ObservationConsumeBatch` after one partition-batch processing attempt;
- `ObservationConsumeCommit` after one contiguous offset-commit attempt;
- `ObservationConsumePoll` after the complete bounded poll cycle and before
  its rebalance gate is released;
- `ObservationConsumeAssigned` after a validated assignment is applied;
- `ObservationConsumeRevoked` after a validated revocation is applied;
- `ObservationConsumeLost` after fatal ownership loss clears the assignment;
- `ObservationConsumeBlocked` after an active poll is told that a rebalance
  callback is waiting;
- `ObservationConsumeGroupError` after an error ends a group-management
  session;
- `ObservationBrokerConnect` after a connection initialization attempt,
  including API-version negotiation and configured SASL;
- `ObservationBrokerRequest` after one Kafka protocol request fails during
  write or completes its response read;
- `ObservationBrokerThrottle` when a broker reports a throttle interval; or
- `ObservationBrokerDisconnect` when a broker connection closes.

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
| Consume assigned/revoked | `PartitionCount` is the validated callback partition count; invalid metadata fails the event and oversized counts are clipped to `MaxAssignedPartitions` and marked truncated |
| Consume lost | `PartitionCount` is bounded callback metadata; the event always fails with `ErrorFenced` because ownership is already lost, while malformed metadata cannot prevent assignment clearing |
| Consume blocked | Reports only a signal received while a bounded poll owns franz-go's rebalance gate; it does not claim that a broker rebalance completed |
| Consume group error | Reports only the stable redacted category for the error that ended the group-management session |
| Broker connect | `Duration` covers dial, API-version negotiation, and configured SASL initialization; a negative upstream duration is clipped and marked truncated |
| Broker request | `APIKey` is Kafka's numeric protocol API key; `RequestBytes` and `ResponseBytes` exclude TLS framing; `QueueDuration` includes franz-go queue and throttle waiting; `Duration` covers that wait through response completion |
| Broker throttle | `ThrottleDuration` is Kafka's reported interval; `ThrottledAfterResponse` distinguishes client-side post-response delay from broker-side pre-response delay |
| Broker disconnect | Reports the connection close without inventing a cause because franz-go does not supply one to this hook |

`BrokerID` is present only when franz-go supplies a non-negative Kafka node ID.
Seed connections can be reported with `BrokerKnown=false`. Invalid negative
byte counts and durations are clipped to zero; duration overflow saturates;
all such cases set `Truncated`. Connection errors and request failures use only
the package's stable redacted `ErrorCategory`. A successful broker connection
proves that configured SASL initialization completed, but franz-go does not
provide a separate successful-authentication hook, so the package does not
invent an authentication event or distinct authentication latency.

Assignment, revocation, loss, and blocked-rebalance durations cover only the
local package state transition before observer dispatch. They do not represent
Kafka rebalance duration, group join time, callback wait time inside franz-go,
or observer execution time. Group-management error hooks do not expose a
duration from franz-go, so their duration is zero.

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

The root observer model currently covers producer delivery, nontransactional
consumer processing, commits, group lifecycle, and producer/consumer broker
connection, request, throttle, and disconnect activity. Standalone
authentication, retry, complete broker rebalance timing, transaction-lifecycle,
replay, inspection, health, and shutdown events remain unimplemented.
Transaction-processor consumer and broker events are also not yet emitted. The
planned `kafka/adapters/gotelemetry` nested module must translate only stable
root observations and pin a reviewed OpenTelemetry messaging
semantic-convention version; OpenTelemetry will not become a root dependency.
