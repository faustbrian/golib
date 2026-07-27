# API

Use `NewProducer`, `NewConsumer`, `NewTransactionProcessor`,
`NewReplayReader`, and `NewInspector` as the composition roots. Every
constructor validates identities and bounded resource policy before franz-go
is configured.

All five client roles accept `ProtocolPolicy`. Its zero value preserves
per-connection `ApiVersions` negotiation. `MinimumVersion` applies an owned
request-version downgrade floor without exposing franz-go version types. It is
not a broker release check or compatibility claim. See the
[configuration reference](configuration.md).

Broker addresses and Kafka client, group, transactional, instance, and rack
identifiers must be valid UTF-8 without control characters or surrounding
whitespace. Identifiers are bounded to 255 bytes. Empty required client and
group IDs retain their required-value errors, oversized IDs retain their size
errors, and other malformed client or group IDs return `ErrInvalidClientID` or
`ErrInvalidGroupID`.

Topic names are validated consistently at every producer, consumer, replay,
and inspection boundary against Kafka's broker naming rules: 1 to 249 ASCII
bytes containing only alphanumerics, `.`, `_`, or `-`, excluding `.` and `..`.
Invalid names fail before client admission or a broker request.
Producer construction additionally requires 1 to 64 unique
`ProducerConfig.AllowedTopics`. The constructor copies the slice, and every
single, batch, asynchronous, and transactional publish rejects a topic outside
the resulting immutable allowlist with `ErrTopicNotAllowed`.

Use `ProducerConfig.Validate` when a composition root must validate producer
policy before constructing a client. It applies the same validation and
defaulting policy as `NewProducer` without allocating a client or dialing
brokers.
`ConsumerConfig.Validate` provides the equivalent allocation-free check for a
consumer-group policy.

`Producer.Publish` is the compatibility synchronous error-only method.
`Producer.PublishRecord` returns one `DeliveryResult`; `PublishBatch` returns
input-ordered results including partial failures; and `PublishAsync` returns a
buffered one-result channel after bounded admission. `ProducerRecord` input
bytes are copied before admission. `ConsumedRecord.Retain` makes an owned copy
of borrowed fetch bytes for retention beyond a handler call.

`ProducerConfig.KeyPolicy` defaults to `KeyRequired`; callers must select
`UnkeyedAllowed` explicitly when unkeyed partition selection is intended.
`ProducerRecord.Partition` defaults to automatic selection. Use
`ExplicitPartition` for an exact non-negative partition; invalid modes and
partition numbers fail before producer admission. An explicit partition
overrides key-based selection but does not remove the configured key
requirement.
Producer admission is bounded independently by record count and total buffered
bytes; a Kafka batch also has its own smaller byte and record limits.
`Producer.Drain` preserves admitted records, `Abort` explicitly discards
buffered records, and `Shutdown` performs a bounded drain before close.
`ProducerConfig.ShutdownTimeout` defaults to `DeliveryTimeout`, cannot be
shorter than delivery, and bounds the error-returning `Close` convenience
method. Shutdown first fences new work and waits for already-started calls to
finish backend admission so a concurrent flush cannot miss them.
Every broker delivery failure is a redacted `DeliveryError`. Its stable
category distinguishes retryable, authorization, fenced, oversized, timeout,
canceled, shutdown, fatal producer-state, permanent, and ambiguous outcomes.
`errors.Is` and `errors.As` retain the underlying identity for deliberate
inspection; application retry is a separate policy decision.
The franz-go backend is configured to stop after detecting idempotent-producer
data loss; a fatal delivery requires producer replacement or an explicit
application recovery decision.

`ProducerConfig.Observers` configures ordered synchronous completion events for
single, batch, and asynchronous production plus broker connection, Kafka
request, throttle, disconnect, and shutdown activity without exposing franz-go
hooks.
`Observation` contains copied payload-free metadata, numeric Kafka broker and
API-key coordinates, bounded duration/byte metadata, and a stable failure
category. `Observation.Validate` lets optional adapters reject malformed public
values against the same bounded metadata, settlement-count, failure-category,
and event-cardinality policy. Observer errors and panics are contained and
reported through the required `ObservationFailureFunc`; they never replace the
delivery result.
Inspector observations add only bounded broker, topic, consumer-group, member,
and partition aggregates plus dependency-health and readiness hysteresis
state. They never copy broker hosts, cluster IDs, inspected target names,
member identities, assignments, or lag coordinates.
`adapters/golog` emits fixed standard-library `log/slog` records from this
contract. Its copied client, topic, and group allowlists deny every identity by
default. `adapters/gotelemetry` supplies the independently versioned
OpenTelemetry mapping.
Callbacks share one cooperative deadline, can run concurrently across producer
and franz-go broker operations, and cannot re-enter the invoking producer. See the
[observability guide](observability.md).

`Producer.RunTransaction` returns redacted `TransactionError` values for Kafka
transaction begin, commit, and abort failures. `Category` distinguishes
authorization, fencing, fatal producer state, retryable abort-required failure,
and ambiguous outcome. `Abortable` means Kafka definitively rejected the
commit and required an abort; `OutcomeKnown` is false when reconciliation is
required before reuse. `errors.Is` and `errors.As` preserve the safe
programmatic cause chain without rendering it.

`TransactionProcessor` is the Kafka-only consume-transform-produce surface.
`TransactionProcessorConfig` separates connection, consumer-group, output,
record-limit, observer, and shutdown concerns. Its observer policy reports
copied begin, commit, abort, shutdown, group-management-error, and broker events
without exposing franz-go values or transaction payload counts. `RunOnce` polls at most
`Group.MaxPollRecords`, begins one transaction, calls the
`TransactionHandler` sequentially for every fetched record, and commits only
after all handlers and all synchronous output deliveries succeed.
`TransactionPollResult.Published` counts acknowledged records inside the open
transaction; they are durable to `read_committed` consumers only when
`Committed` is true. A false commit result returns
`ErrTransactionNotCommitted`, leaves the poll unsettled, and is safe to retry.
Begin, end, or abort failures fence further runs with
`ErrTransactionProcessorFatal`; the application must close the processor,
reconcile ambiguous outcomes where reported, and construct a new instance.
Each handler receives a callback-lifetime `Transaction`; retaining it does not
extend its lifetime. `Run` and `RunOnce` are mutually exclusive. `Shutdown`
fences new runs, waits within the caller context, preserves static membership,
and can be retried after an incomplete shutdown. Transaction publishes own
copies of record bytes. Concurrent publishes are safe, but their admission
order is scheduler-dependent; callers that require source-derived output order
must publish synchronously in that order.
Producer and processor transaction observers classify unknown commit or abort
outcomes as `ErrorAmbiguous`; their failures and panics never replace the
transaction result. Producer and processor shutdown attempts emit one event
after each acquired lifecycle attempt, including an incomplete attempt and a
later successful retry. Transaction observers cannot re-enter the invoking
client.

`Consumer.RunOnce` returns one bounded poll result. Processing is sequential
within a partition. `ConsumerConfig.MaxConcurrentHandlers` defaults to one and
can permit up to 64 concurrent callbacks across independent partitions. The
same handler value must be concurrency-safe when the value exceeds one.
Cross-partition callback order is scheduler-dependent; commit construction and
the first returned handler error retain stable poll-partition order. After one
partition fails, its later fetched records are skipped while independent
partitions continue; only each partition's contiguous successful prefix is
submitted for commit. `Consumer.Run` exits cleanly when its context is canceled.
A canceled runner admits no new callback from buffered fetch results, and a
context cause observed after a callback prevents settlement even if it returns
nil.
`ConsumerConfig.MaxConcurrentFetches`, `FetchMaxBytes`, and
`FetchMaxPartitionBytes` jointly bound compressed fetch buffering. The
per-partition limit follows Kafka's progress rule: one larger record batch may
still be returned.
`MaxPollRecords` is enforced again at the package boundary; a backend response
above it fails with `ErrTooManyFetchedRecords` before any handler runs.
`ConsumerConfig.Limits` defaults to `DefaultMessageLimits` and bounds fetched
topic, key, value, header count, header keys, individual header values, and
aggregate header bytes before the package copies header metadata or runs a
handler.
`ConsumerConfig.BalancePolicy` selects cooperative-sticky, eager-sticky, or the
ordered eager-to-cooperative migration pair without exposing franz-go
balancers. Optional validated `InstanceID` and `Rack` values select static
membership and rack-aware fetching respectively.
`ConsumerConfig.RebalanceHandler` defaults to `RebalanceCancelHandler`. A
blocked rebalance cancels every active handler with `ErrConsumerRebalance` and
stops every worker from admitting another callback from that poll. The explicit
`RebalanceDrainHandler` alternative lets only already-active handlers finish
and settles successful results. Both policies commit safe earlier prefixes
before releasing franz-go's poll gate. Handler cancellation is cooperative.
`Consumer.Run` and `Consumer.RunOnce` are mutually exclusive. `Shutdown`
atomically fences new runs, waits for an active runner, explicitly leaves a
dynamic group membership, and then closes the client. A deadline or leave
failure returns `ErrConsumerShutdownIncomplete` and leaves shutdown retriable.
Static membership deliberately skips the leave request. `Close` applies the
configured `ConsumerConfig.ShutdownTimeout` and returns its shutdown error.
`Run`, `RunOnce`, `RunBatchOnce`, and `Shutdown` reject a nil context with
`ErrContextRequired` before polling or changing lifecycle state.
`Consumer.PausePartitions` and `ResumePartitions` accept owned
`TopicPartition` values only for configured subscriptions. Each request and
the accumulated pause set are bounded by `MaxPausedPartitions`.
`Consumer.Assignment` returns a sorted copy of currently tracked partitions
plus a package-local assignment epoch. The epoch is a settlement fence and
diagnostic sequence, not Kafka's broker generation ID. Assignment callback
metadata is bounded by `MaxAssignedPartitions`; invalid or oversized metadata
fails closed before another handler is invoked.
`Consumer.RunBatchOnce` groups one bounded poll by topic partition and invokes
`BatchHandler` once per non-empty partition batch, using the same bounded
cross-partition concurrency policy. A nil result settles the entire batch; an
error settles none of it. Successful independent partition batches remain
committable. `ConsumedBatch.Retain` copies the batch slice and every record byte
for use after the handler returns.

`ConsumerConfig.Observers` uses the same copied, ordered `ObserverPolicy` as
the producer. Consumer events report each record or partition-batch processing
attempt, each offset-commit attempt, the final bounded poll result, assignment,
revocation, ownership loss, blocked rebalances, group-management errors, and
broker connection, Kafka request, throttle, and disconnect activity.
Validated single-topic metadata can include source coordinates and conservative
record bytes; mixed-topic or invalid metadata is omitted. Observer failures do
not change handler, commit, or poll outcomes. Consumer observers run before the
poll releases its rebalance gate when they report processing; lifecycle
observers run after package assignment and rebalance locks are released. They
cannot re-enter mutating or lifecycle operations on that consumer.

`NewFailureHandler` decorates the per-record `Handler` contract without
changing `Consumer` or exposing franz-go. `FailureRetryPolicy` bounds selected
error categories to 1 through 32 total attempts with capped,
cancellation-aware exponential backoff. `FailureModeStop` is the zero terminal
mode. `FailureModeRetryTopic` and `FailureModeDeadLetter` require a
`FailurePublisher`, an explicit valid `FailureTarget.Topic`, a non-zero target
version, and a bounded publish timeout. `Producer` implements the narrow
publisher interface. `FailureModeDelegate` invokes one synchronous
application-owned `FailureDelegate`; nil explicitly resolves the source
handler and an error keeps it unsettled.

`HandlerFailure` exposes borrowed source metadata, the attempt, stable
`ErrorCategory`, and a programmatic cause; `Retain` deep-copies record bytes.
`FailureHandlingError` renders only its stable stage and category while
preserving original identities through `errors.Is` and `errors.As`.
Classifier, publisher, and delegate panics are contained. Successful retry or
dead-letter publication returns handler success so the normal consumer can
commit the source offset afterward. The two effects are not atomic and can
duplicate target records. Use `TransactionProcessor` for Kafka-transactional
source-offset and output settlement.
`ReplayReader.Plan` returns an owned local dry-run plan after applying
`ReplayCheckpoint`. `PlanAgainstBroker` additionally validates effective starts
and exclusive ends against broker log-start and high-watermark offsets under
`PlanningTimeout`. It polls no records, invokes no handler, changes no group
offset, and does not consume the reader's single replay execution. Planning is
fenced by replay and shutdown lifecycle state. An error returns a zero plan so
an unvalidated local plan cannot be mistaken for a broker-validated result.
`Replay` requires
`ReplaySideEffectsAllowed`, uses exact no-reset offsets after repeating the
same validation before handler admission. Unavailable bounds and out-of-range
requests have distinct errors. Replay also returns `ErrReplayStalled` if
`ProgressTimeout` elapses without advancing any range. It returns 64-bit
aggregate plus per-range progress on every success or failure.
`ReplayConfig.MaxConcurrentFetches` independently bounds broker fetch requests.
`MaxConcurrentHandlers` defaults to one and permits 1 through 64 fixed workers.
Values above one process one sequential batch per partition concurrently. All
partition batches returned by one bounded poll are admitted together; after a
partition failure, already admitted independent partitions finish and their
successful next offsets remain in the result checkpoint. Errors are joined in
stable first-seen partition order. Cancellation reaches every active callback
and prevents a queued partition from invoking a new callback. A backend result
above `MaxPollRecords` fails with `ErrTooManyFetchedRecords` before grouping or
handler admission.
`ReplayConfig.Validate` applies the constructor's complete normalization and
validation without allocating a client. `ReplayConfig.Observers` adds copied,
ordered plan, per-record, aggregate-run, shutdown, and broker observations.
Aggregate observations preserve exact signed-64-bit processed, skipped, failed,
and remaining counts; record observations expose validated Kafka coordinates
without record data. Same-reader observer reentry fails with
`ErrObserverReentry`.
`ReplayResult.Checkpoint` returns owned next offsets for a new reader. A reader
permits one execution; concurrent and repeated calls have distinct lifecycle
errors. `Shutdown` fences new work and is
retriable after a bounded incomplete wait; `Close` returns the configured
bounded shutdown result.
`Inspector.PlanReplayByTimestamp` maps one millisecond-aligned record-time
window and an explicit bounded partition set to a sorted owned
`ReplayTimestampPlan`. It uses exact-partition broker requests for log starts,
high watermarks, and both timestamp boundaries. `ReplayRanges` returns owned
non-empty `ReplayRange` values suitable for `ReplayConfig`. Missing, malformed,
out-of-bounds, or retention-ambiguous responses fail closed and return a zero
plan. Planning performs no fetch, handler invocation, group join, or offset
mutation, and Kafka timestamps do not imply cross-partition ordering.
`Inspector.Cluster` returns bounded copied cluster identity, controller
visibility, and sorted broker metadata. `Inspector.Topics` returns bounded
replica, ISR, offline-replica, leader-epoch, beginning/end offset, and effective
`min.insync.replicas`, cleanup, retention, compaction, segment, and
unclean-election state. Kafka duration values remain raw milliseconds;
retention limits preserve the `-1` unlimited sentinel and retention bytes are
per partition. `Inspector.ConsumerGroupLag` returns bounded classic-group
coordinator, state, protocol, member identity, copied assignment,
committed-offset, and lag inspection. Members are sorted by member ID and their
assignments by topic and partition. Topic and group methods require explicit
target lists, and every operation derives `InspectorConfig.RequestTimeout`.
`InspectorConfig.Observers` reports inspection, dependency, readiness,
shutdown, and broker activity through the shared stable observation contract.

`DependencyHealth` is current bounded connectivity; `Health` is its
compatibility alias. `Readiness` applies configured consecutive-failure and
recovery thresholds. Its `ReadinessState.Ready` field is the composition
decision, while the returned error retains the latest dependency failure for
diagnostics. Nil or canceled probe contexts do not mutate readiness. Initial
readiness requires the recovery threshold, and a ready inspector remains ready
until the failure threshold is reached. `Liveness` reports only whether the
inspector remains locally open, never Kafka availability. `Close` returns
`ErrObserverReentry` for same-inspector observer reentry and otherwise closes
idempotently, makes liveness and readiness false immediately, and fences later
operations with `ErrInspectorClosed`.

`ProducerConfig.CompressionPreferences` is an ordered, constructor-copied list
of `CompressionCodec` values. An empty list defaults to Snappy followed by no
compression. Duplicate codecs and ineffective orders are rejected, and
`CompressionNone` may appear only last.

The canonical machine-checked exported API is in `api/baseline.txt`.
