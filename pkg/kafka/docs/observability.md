# Observability hooks

The root module exposes a vendor-neutral `ObserverPolicy`. The current surface
reports producer delivery plus consumer poll, record-handler, batch-handler,
offset-commit, bounded in-process retry scheduling, assignment, revocation,
ownership-loss, blocked-rebalance, group management error, Kafka transaction
begin/commit/abort, broker connection, broker request, broker throttle, broker
disconnect, and replay plan, record, run, and shutdown completion. Producer,
consumer, and transaction-processor
shutdown attempts plus inspector cluster, topic, consumer-group,
dependency-health, readiness, shutdown, and broker events use the same
contract. The franz-go hook bridge is private;
observations do not expose franz-go hooks, records, requests, responses,
clients, network connections, broker endpoints, or raw group errors.

## Configuration and execution

`ProducerConfig.Observers`, `ConsumerConfig.Observers`,
`TransactionProcessorConfig.Observers`, `ReplayConfig.Observers`, and
`InspectorConfig.Observers` accept 1 to 16 ordered `ObserverFunc` callbacks.
The callback slice is copied during client construction. A
non-empty policy requires an explicit
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
The transaction processor uses the same private broker hook and can emit broker
events before `NewTransactionProcessor` returns.
The inspector emits operation observations after each admitted read-only call.
One `Readiness` call emits the underlying dependency-health observation and,
when that probe conclusively updates hysteresis, a separate readiness
observation. Nil, caller-canceled, closed, or observer-reentrant readiness
calls do not invent a readiness decision.
Replay partition workers can invoke record observers concurrently across
independent partitions, while broker events can overlap planning, execution,
and shutdown. Records within one replay partition remain sequential.

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
- `ObservationConsumeRetryScheduled` after one failed record or whole-batch
  handler attempt is classified for another bounded in-process attempt and
  before its cancellation-aware backoff begins;
- `ObservationConsumeAssigned` after a validated assignment is applied;
- `ObservationConsumeRevoked` after a validated revocation is applied;
- `ObservationConsumeLost` after fatal ownership loss clears the assignment;
- `ObservationConsumeBlocked` after an active poll is told that a rebalance
  callback is waiting;
- `ObservationConsumeRebalanceWait` when that callback's local wait reaches
  poll-gate release, callback-context cancellation, or the configured rebalance
  timeout;
- `ObservationConsumeGroupError` after an error ends a group-management
  session;
- `ObservationTransactionBegin` after a transaction begin attempt;
- `ObservationTransactionCommit` after a transaction commit attempt;
- `ObservationTransactionAbort` after transaction cleanup attempts an abort;
- `ObservationReplayPlan` after `PlanAgainstBroker` validates or rejects the
  complete explicit range set;
- `ObservationReplayRecord` after one requested record is processed, skipped,
  or failed;
- `ObservationReplayRun` after one single-use replay execution completes or
  fails with its exact resumable progress;
- `ObservationReplayShutdown` after an admitted bounded shutdown completes or
  remains incomplete;
- `ObservationInspectorCluster` after one cluster metadata query;
- `ObservationInspectorTopics` after one explicit bounded topic query;
- `ObservationInspectorConsumerGroups` after one explicit bounded
  consumer-group lag
  query;
- `ObservationDependencyHealth` after one admitted connectivity probe;
- `ObservationReadiness` after one conclusive readiness-hysteresis update;
- `ObservationInspectorShutdown` after the inspector closes its client;
- `ObservationProducerShutdown` after an admitted producer shutdown completes
  or remains incomplete;
- `ObservationConsumerShutdown` after an admitted consumer shutdown completes
  or remains incomplete;
- `ObservationTransactionProcessorShutdown` after an admitted
  consume-transform-produce shutdown completes or remains incomplete;
- `ObservationBrokerConnect` after a connection initialization attempt,
  including API-version negotiation and configured SASL; its bounded
  `AuthenticationMethod` identifies the configured flow without credentials;
- `ObservationBrokerRequest` after one Kafka protocol request fails during
  write or completes its response read;
- `ObservationBrokerThrottle` when a broker reports a throttle interval; or
- `ObservationBrokerDisconnect` when a broker connection closes.

Every observation contains its copied client ID, start time, elapsed duration,
success flag, and stable failure category. Consumer events also contain the
copied group ID. Successful events use `ErrorUnknown` because no failure
category applies.

`Observation.Validate` applies the root-owned bounds, settlement-count
relationships, success/category relationship, Kafka coordinate bounds, and
event-specific record cardinality. Optional adapters should call it before
exporting a public observation rather than reimplementing these invariants.

| Kind | Count and coordinate meaning |
| --- | --- |
| Produce record/async | `RecordCount=1`; successful delivery has partition, offset, and broker timestamp |
| Produce batch | `RecordCount` is the bounded input count; coordinates are omitted |
| Consume record | `RecordCount=1`; `ProcessedCount=1` only after handler success; validated source partition and offset are present |
| Consume batch | `RecordCount` is the partition-batch size; `ProcessedCount` equals it only after handler success; offset is the batch's last source offset |
| Consume commit | `RecordCount` and `ProcessedCount` are the contiguous records represented by the commit; `PartitionCount` is the number of submitted partition offsets; `CommittedCount` is zero on commit failure |
| Consume poll | `RecordCount`, `ProcessedCount`, and `CommittedCount` match the returned `PollResult` while within policy bounds; `PartitionCount` counts validated fetched topic-partitions |
| Consume retry scheduled | `RecordCount` is one record or the complete partition batch; partition and last source offset identify the retry unit; processing and commit counts are zero because the later attempt has not run |
| Consume assigned/revoked | `PartitionCount` is the validated callback partition count; invalid metadata fails the event and oversized counts are clipped to `MaxAssignedPartitions` and marked truncated |
| Consume lost | `PartitionCount` is bounded callback metadata; the event always fails with `ErrorFenced` because ownership is already lost, while malformed metadata cannot prevent assignment clearing |
| Consume blocked | Reports only a signal received while a bounded poll owns franz-go's rebalance gate; it does not claim that a broker rebalance completed |
| Consume group error | Reports only the stable redacted category for the error that ended the group-management session |
| Transaction begin/commit/abort | Reports one completed local phase with no record or payload counts; producer events contain no group ID, while consume-transform-produce events contain its copied source group ID |
| Replay plan | `PartitionCount` is the configured range count and `ReplayRemaining` is the exact validated remaining offset count; a broker-bound failure reports a zero returned plan and stable category |
| Replay record | `RecordCount=1`; exactly one of `ReplayProcessed`, `ReplaySkipped`, or `ReplayFailed` is one; processed and skipped outcomes succeed, failed outcomes fail, processed records set `ProcessedCount=1`, and validated source topic, partition, offset, timestamp, and conservative bytes are present |
| Replay run | `PartitionCount` is the configured range count; `ReplayProcessed`, `ReplaySkipped`, `ReplayFailed`, and `ReplayRemaining` exactly match the returned result and its resumable ranges; success requires zero failed and remaining records |
| Replay shutdown | Reports bounded reader shutdown without record coordinates or progress counts |
| Inspector cluster | `BrokerCount` is the validated returned broker count; failure exports zero brokers |
| Inspector topics | `TopicCount` is the bounded requested target count and `PartitionCount` is the validated returned aggregate; failure exports no partitions |
| Inspector consumer groups | `GroupCount` is the bounded requested target count; `GroupMemberCount` and `PartitionCount` are validated returned aggregates and are zero on failure |
| Dependency health | `DependencyHealthy` exactly matches the completed probe outcome |
| Readiness | `DependencyHealthy`, `Ready`, `ConsecutiveFailures`, and `ConsecutiveSuccesses` are the conclusive post-probe state; operation success matches dependency health, not the stateful `Ready` decision |
| Inspector shutdown | Reports one successful idempotent close transition; repeated closes emit nothing |
| Producer/consumer/transaction-processor shutdown | Reports each attempt that acquires lifecycle ownership, including incomplete attempts and a later successful retry; invalid, observer-reentrant, concurrent, and already-completed calls emit nothing |
| Broker connect | `Duration` covers dial, API-version negotiation, and configured SASL initialization; `AuthenticationMethod` is the configured bounded method, including explicit `none`; a negative upstream duration is clipped and marked truncated |
| Broker request | `APIKey` is Kafka's numeric protocol API key; `RequestBytes` and `ResponseBytes` exclude TLS framing; `QueueDuration` includes franz-go queue and throttle waiting; `Duration` covers that wait through response completion |
| Broker throttle | `ThrottleDuration` is Kafka's reported interval; `ThrottledAfterResponse` distinguishes client-side post-response delay from broker-side pre-response delay. The event is request-level and deliberately omits topic, partition, and record coordinates because franz-go's throttle hook does not identify the request and one produce response can cover many records. |
| Broker disconnect | Reports the connection close without inventing a cause because franz-go does not supply one to this hook |

`InspectTopics` and `InspectConsumerGroups` invoke the existing fail-closed
operation once per independently isolated target. They therefore emit one
existing topic or consumer-group observation per target, potentially
concurrently and without an additional aggregate event. Target names remain
absent from observations.

`BrokerID` is present only when franz-go supplies a non-negative Kafka node ID.
Seed connections can be reported with `BrokerKnown=false`. Invalid negative
byte counts and durations are clipped to zero; duration overflow saturates;
all such cases set `Truncated`. Connection errors and request failures use only
the package's stable redacted `ErrorCategory`. A successful broker connection
proves that the reported configured SASL method completed initialization.
franz-go does not provide a separate successful-authentication hook, so the
package does not invent a second authentication event or distinct
authentication latency.
The pinned single-broker fixture applies a client-ID producer-byte quota and
proves a positive post-response throttle event alongside successful delivery.

Assignment, revocation, loss, and blocked-rebalance durations cover only the
local package state transition before observer dispatch.
`ObservationConsumeRebalanceWait` starts when the package enters franz-go's
blocked callback.
A successful duration ends immediately after the package calls
`AllowRebalance` for that poll. A failed duration ends when the franz-go client
context is canceled or the configured `RebalanceTimeout` expires; failure does
not claim the poll gate was released. Repeated blocked signals for the same
poll are deduplicated. This interval can include handler drain or cancellation,
observer dispatch, and offset settlement. It does not represent group join,
`JoinGroup`/`SyncGroup` request time, all cooperative phases, or complete Kafka
rebalance duration; broker-request observations expose individual protocol
request latency separately. Poll completion retains the callback until this
wait observation returns; an observer that ignores its context can therefore
block poll completion as described above. Group-management error hooks do not expose a
duration from franz-go, so their duration is zero.
Transaction durations cover only the corresponding local begin or bounded end
call and exclude observer execution. A known abort-required commit failure is
`ErrorRetryable`; authorization, fencing, and fatal outcomes retain those
categories; an unknown commit or abort outcome is `ErrorAmbiguous`. Application
callback failure is not copied into the abort observation and a successful
abort remains a successful lifecycle event.
Replay plan duration covers broker start/end-offset validation. Record duration
covers validation and the bounded handler call but excludes observer execution.
Run duration covers broker validation, polling, handlers, and progress failure
through the returned result. Shutdown duration covers waiting for an active
replay and closing the direct client. Replay handler errors preserve a valid
application-provided `Category`; invalid or panicking category methods are
contained as `ErrorPermanent`.
Producer shutdown duration covers admission fencing, in-flight admission
draining, flushing, and close. Consumer and transaction-processor shutdown
duration covers waiting for the active runner, the dynamic-member group leave,
and close. Incomplete attempts retain their stable cancellation, timeout, or
other redacted error category; a successful retry is a separate event.
Consume-retry duration starts immediately before the failed handler attempt and
ends after its error is classified and selected for retry. It excludes observer
dispatch, the subsequent backoff, and the later attempt. The event means
"scheduled", not "executed": cancellation during backoff can prevent that later
attempt. It does not report Kafka redelivery after a poll, rebalance, restart,
or process failure.

`Producer.Diagnostic` and `TransactionProcessor.Diagnostic` are pull-based
local snapshots, not observer events. They provide payload-free lifecycle,
fatal-category, and buffered-output gauges without adding Kafka identity
cardinality. Poll them only at an application-owned bounded cadence; do not
derive record-delivery, commit-outcome, broker-health, or coordinator-health
claims from their values.

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

Observers must not call the client that invoked them. Producer, consumer,
transaction-processor, replay, and inspector operations using the callback
context fail with `ErrObserverReentry`. Their `Shutdown` and `Close` methods,
plus context-free mutating consumer operations, also fail with that error while
a callback is active, even when the callback replaces its context. Per-record
and whole-batch failure decorators also reject the callback context, preventing
an observer from recursively scheduling another retry event. This conservative
fence can reject a concurrent lifecycle call from another goroutine while an
observer is running. Replacing the callback context for any other operation
violates the contract. The package holds no client lifecycle lock while
application observer code runs.

## Current boundary

The root observer model currently covers producer delivery, nontransactional
consumer processing, bounded in-process retry scheduling, commits, group
lifecycle, producer and
consume-transform-produce transaction lifecycle, replay planning, record
outcomes, aggregate progress and shutdown, inspector read-only operations,
dependency health, readiness, and shutdown, plus producer, consumer,
transaction-processor, replay, and inspector broker activity. Producer,
consumer, and transaction-processor shutdown attempts are also covered.
Kafka redelivery and end-to-end broker rebalance timing remain unimplemented.
Authentication state is reported honestly as part of broker connection
initialization because that is the lifecycle boundary supplied by franz-go.

The standard-library [`kafka/adapters/golog`](../adapters/golog) package
translates every current stable root observation into one fixed `log/slog`
record. Its fields are bounded scalar metadata. Client IDs, topics, and
consumer groups are denied unless exactly present in copied allowlists of at
most 128 identities each. Adapter-generated fields never contain payloads,
keys, headers, credentials, broker endpoints, application errors, or panic
values. Attributes already attached to the caller's logger remain
application-owned. A slog handler panic becomes the stable
`golog.ErrLoggerPanic`; slog handler errors cannot be surfaced because
`slog.Logger` intentionally does not return them. Handler blocking is governed
by the root observer's cooperative deadline and must remain bounded.

The independently versioned
[`kafka/adapters/gotelemetry`](../adapters/gotelemetry) module translates every
current stable root observation. It emits the reviewed OpenTelemetry messaging
semantic conventions 1.43.0 for send, poll, process, and commit operations plus
adapter-owned Kafka lifecycle, broker request, queue, and throttle metrics.
Client IDs, topics, and consumer groups are denied as attributes unless they
are exactly present in copied bounded allowlists. Its completion observer does
not inject or extract record headers. A separate immutable
`TraceContextPropagation` policy copies only W3C `traceparent` and `tracestate`
between explicitly supplied records and contexts, with Kafka message-limit
validation, producer-record ownership, fail-closed duplicate fields, and no
baggage or global propagator. It does not publish, consume, settle, create
spans, or alter Kafka settlement. A pinned Apache Kafka 4.3.1 integration test
proves the injected headers survive root-producer publication and root-consumer
fetch and extract as the same remote span context before settlement.
OpenTelemetry remains absent from
the root module.
