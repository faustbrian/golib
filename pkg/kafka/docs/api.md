# API

Use `NewProducer`, `NewConsumer`, `NewTransactionProcessor`,
`NewReplayReader`, and `NewInspector` as the composition roots. Every
constructor validates identities and bounded resource policy before franz-go
is configured.

The public `kafkatest` test-support package provides broker-backed producer,
consumer, transaction, replay, and inspector conformance suites plus
deterministic authentication-provider and observer-policy suites. Its
`BrokerHarness` exposes only Kafka broker addresses, security policy, isolated
topic creation, bounded direct-partition reads, and committed-offset lookup; it
does not expose franz-go clients, records, options, or administrative response
types. See the [conformance guide](conformance.md).

All five client roles accept `ProtocolPolicy`. Its zero value preserves
per-connection `ApiVersions` negotiation. `MinimumVersion` applies an owned
request-version downgrade floor without exposing franz-go version types. It is
not a broker release check or compatibility claim. See the
[configuration reference](configuration.md).

All client roles also accept the same `ClientSecurity` contract. Static
`TLS.RootCAs` and a rotating `TrustAnchorProvider` are mutually exclusive. A
provider supplies the complete bounded DER-encoded root set for each new TLS
connection, must be concurrency-safe, and shares `CredentialTimeout` with the
authentication and client-certificate providers. Existing connections retain
their negotiated trust state; use overlap-first rotation and force bounded
reconnection before retiring an old root. See the [security guide](security.md)
for ownership, redaction, failure, and rollout semantics.
`OAuthBearerProvider` is the explicit external-token acquisition seam. The
package bounds each invocation and validates and copies its result; the
provider owns endpoint TLS, HTTP limits, token caching, refresh policy, and
identity-provider-specific fields. A new Kafka SASL session invokes the
provider again, including broker-enforced reauthentication.

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
input-ordered results including partial failures, normalizing franz-go's
cross-partition callback-completion order by the owned record identity; and
`PublishAsync` returns a buffered one-result channel after bounded admission.
Missing results retain `ErrDeliveryResultMissing`; duplicate, nil, or unknown
results retain `ErrDeliveryResultInvalid`. Both are ambiguous because the
backend did not provide one trustworthy result for every admitted input.
`ProducerRecord` input bytes are copied before admission.
`ConsumedRecord.Retain` makes an owned copy of borrowed fetch bytes for
retention beyond a handler call.
`ProducerRecord.Validate` applies an explicit validated `MessageLimits` policy
without allocating or transferring caller-owned bytes, allowing adapters and
composition roots to fail before ownership changes.

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
`RetryBackoffMin` and `RetryBackoffMax` bound exponential per-client retry
jitter. Failed-partition metadata refresh uses the larger of the retry minimum
and a 250-millisecond safety floor. `ProducerConfig.ShutdownTimeout` defaults to
`DeliveryTimeout + RetryBackoffMax`, cannot be shorter than that combined
bound, and bounds the error-returning `Close` convenience method. Shutdown
first fences new work and waits for already-started calls to finish backend
admission so a concurrent flush cannot miss them.
Every broker delivery failure is a redacted `DeliveryError`. Its stable
category distinguishes retryable, authorization, fenced, oversized, timeout,
canceled, shutdown, fatal producer-state, permanent, and ambiguous outcomes.
OAUTHBEARER broker error challenges retain
`kerr.SaslAuthenticationFailed` identity without retaining the challenge body,
so delivery classification remains `ErrorAuthorization` without disclosing
broker-returned OAuth details.
`errors.Is` and `errors.As` retain the underlying identity for deliberate
inspection; application retry is a separate policy decision.
Non-transactional production permits franz-go to stop an in-flight idempotent
record when a synchronous caller or the package delivery bound expires.
Cancellation, delivery timeout, or retry exhaustion after admission is
conservatively `ErrorAmbiguous`: the broker may have accepted the record even
though the acknowledgement was lost. `PublishAsync` uses caller cancellation
only while admission is blocked; after it returns, the owned record is detached
from later caller cancellation and continues under the package delivery bound.
The producer performs no application retry, and callers must reconcile before
submitting the record again because a new submission can duplicate it.
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
`Transaction.Publish` is additionally bounded by the earlier of its context
and `DeliveryTimeout + RetryBackoffMax`. If that bound expires after franz-go
may have sent the record, the package cannot safely cancel only the record
without invalidating its transactional sequence state. It instead cancels and
closes the owned client, returns an `ErrorAmbiguous` `DeliveryError` joined with
`ErrProducerFatal`, skips commit and abort on the closed client, and rejects all
later operations. A replacement using the same transactional ID fences and
recovers the broker-side open transaction.

`TransactionProcessor` is the Kafka-only consume-transform-produce surface.
`TransactionProcessorConfig` separates connection, consumer-group, output,
record-limit, observer, and shutdown concerns. Its observer policy reports
copied begin, commit, abort, shutdown, group-management-error, and broker events
without exposing franz-go values or transaction payload counts. `RunOnce` polls at most
`Group.MaxPollRecords`, begins one transaction, calls the
`TransactionHandler` sequentially for every fetched record, and commits only
after all handlers and all synchronous output deliveries succeed.
Source poll and group-join failures are returned as redacted `ConsumerError`
values with `ConsumerOperationPoll`; retry decisions use their stable category
rather than franz-go or Kafka protocol error types.
`TransactionPollResult.Published` counts acknowledged records inside the open
transaction; they are durable to `read_committed` consumers only when
`Committed` is true. A false commit result returns
`ErrTransactionNotCommitted`, leaves the poll unsettled, and is safe to retry.
Begin, end, or abort failures fence further runs with
`ErrTransactionProcessorFatal`; the application must close the processor,
reconcile ambiguous outcomes where reported, and construct a new instance.
An admitted output whose delivery bound expires follows the same terminal
client-close rule: the current poll is not settled or committed, the output is
not visible at `read_committed`, and later runs fail before polling.
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

`Producer.Diagnostic` returns copied scalar lifecycle state plus franz-go's
current buffered producer record and byte counts. `Accepting` reflects the
package admission gate; `TransactionActive` reflects local `RunTransaction`
ownership; `FatalCategory` is redacted and is `ErrorUnknown` when `Fatal` is
false. The lifecycle fields are one lock-consistent snapshot, while buffered
counts are a separate concurrent sample and may change immediately. The method
performs no Kafka I/O and neither exposes nor invokes methods on the retained
fatal error.
`Producer.Health` is the separate broker-connectivity probe and always derives
the configured `RequestTimeout`; a shorter caller deadline still wins.

`TransactionProcessor.Diagnostic` applies the same contract to `Run` and
`RunOnce`, a locally begun transaction attempt, shutdown ownership, fatal state,
forced client termination, and buffered transactional output. `Closing` ends
when `Closed` becomes true. Neither diagnostic describes broker transaction
coordinator state, resolves an ambiguous outcome, or proves delivery.

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
Consumer-group poll, offset-commit, and dynamic-member leave failures return a
redacted `ConsumerError`. `Operation` distinguishes poll, commit, and leave;
`Category` provides the stable package classification; and `Retryable` reports
whether a later bounded attempt may succeed without changing input or
configuration. The original cause remains available through `errors.Is` and
`errors.As`, but its potentially sensitive text is not rendered. `RunOnce` and
`RunBatchOnce` return a retryable poll error to the caller rather than hiding an
unbounded retry loop. `Run` returns the first such exhausted internal retry
cycle. Handler errors remain application errors and are not converted to
`ConsumerError`.
`ConsumerConfig.MaxConcurrentFetches`, `FetchMaxBytes`, and
`FetchMaxPartitionBytes` jointly bound compressed fetch buffering.
`FetchMinBytes` controls broker-side response batching from one byte through
the aggregate maximum; `FetchMaxWait` keeps that batching delay bounded. The
per-partition limit follows Kafka's progress rule: one larger record batch may
still be returned. `BrokerMaxReadBytes` separately rejects an encoded response
above its hard limit before franz-go allocates the response body.
`MaxDecompressedBatchBytes` rejects an individual batch that expands beyond
policy, while `MaxBufferedDecompressedBytes` bounds decoded compressed-batch
memory retained across active and prefetched responses. An overflow preserves
`ErrFetchBatchTooLarge` or `ErrFetchDecompressedBufferFull`; consumer errors
classify both as `ErrorOversized`. The rejected batch reaches no handler and
advances no source offset. If a response contains earlier complete batches,
franz-go may return that contiguous prefix and retry the rejected trailing
batch on a later poll; the package may handle and settle only that prefix.
The reclaimable active-buffer lifecycle applies to Kafka record batches
(magic 2). Legacy compressed message sets retain the response and per-batch
limits but are unsupported because franz-go does not attach their decoded
allocation to returned records for recycling.
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
If Kafka fences a duplicate static `InstanceID`, the affected consumer enters a
terminal state. The operation observing the fence and all later runner calls
return both `ErrConsumerFatal` and `ErrConsumerInstanceFenced`; the stable
package sentinel avoids a franz-go dependency while the underlying broker
cause remains available in the error chain. Later calls fail before polling.
Shutdown remains available, but recovery requires a new consumer with a
corrected deployment-unique identity.
`ConsumerConfig.RebalanceHandler` defaults to `RebalanceCancelHandler`. A
blocked rebalance cancels every active handler with `ErrConsumerRebalance` and
stops every worker from admitting another callback from that poll. The explicit
`RebalanceDrainHandler` alternative lets only already-active handlers finish
and settles successful results. Both policies commit safe earlier prefixes
before releasing franz-go's poll gate. Handler cancellation is cooperative.
`Consumer.Run`, `RunOnce`, and `RunBatchOnce` are mutually exclusive. `Drain`
atomically fences new work, interrupts an idle broker poll, lets already
admitted handlers and contiguous settlement finish, and returns without
leaving the group or closing the client. A deadline returns
`ErrConsumerDrainIncomplete` and leaves the drain fenced and retriable.
`Shutdown` applies the same boundary before explicitly leaving a dynamic group
membership and closing the client. A deadline or leave failure returns
`ErrConsumerShutdownIncomplete` and leaves shutdown retriable.
Static membership deliberately skips the leave request. `Close` applies the
configured `ConsumerConfig.ShutdownTimeout` and returns its shutdown error.
`Run`, `RunOnce`, `RunBatchOnce`, `Drain`, and `Shutdown` reject a nil context
with `ErrContextRequired` before polling or changing lifecycle state.
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

`NewBatchFailureHandler` decorates that whole-batch contract. The constructor
validates copied bounds and callback compatibility without allocating a Kafka
client. `HandleBatch` validates one non-empty, ordered, single-partition batch
before retaining any bytes and gives every retry an isolated copy. Stop and
failed delegate outcomes leave the complete batch unsettled. A nil delegate
result resolves the complete batch.

Retry-topic and dead-letter modes preserve each source record and append its
source coordinates, attempt, category, target version, batch index, and batch
count. `BatchFailurePublisher` returns exactly one input-ordered
`DeliveryResult` per record; `Producer` implements the interface. The decorator
returns success only when the publisher returns no error and every result is a
definite success. Missing, extra, mismatched, failed, or ambiguous results
return `ErrFailurePublish`. `FailureHandlingError.DeliveryResults` returns an
owned copy of the available outcomes for reconciliation without exposing
record payloads through the error string.

`ConsumerConfig.Observers` uses the same copied, ordered `ObserverPolicy` as
the producer. Consumer events report each record or partition-batch processing
attempt, each offset-commit attempt, the final bounded poll result, assignment,
revocation, ownership loss, blocked rebalances, group-management errors, and
broker connection, Kafka request, throttle, and disconnect activity.
Broker-connect observations identify the configured bounded
`AuthenticationMethod`; success proves that franz-go completed API-version
negotiation and that authentication flow, while failures retain only the stable
redacted category.
Validated single-topic metadata can include source coordinates and conservative
record bytes; mixed-topic or invalid metadata is omitted. Observer failures do
not change handler, commit, or poll outcomes. Consumer observers run before the
poll releases its rebalance gate when they report processing; lifecycle
observers run after package assignment and rebalance locks are released. They
cannot re-enter mutating or lifecycle operations on that consumer.

`NewFailureHandler` decorates the per-record `Handler` contract without
changing `Consumer` or exposing franz-go. Before retaining bytes or invoking
the wrapped handler, it validates the source topic, partition, offset,
timestamp type, leader epoch, and complete record material against its copied
`Limits`; rejection returns `ErrFailureRecordInvalid` and the underlying
record-limit identity where one applies. `FailureRetryPolicy` bounds selected
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
Replay accepts the dedicated `ReplayHandler` contract rather than a
consumer-group `Handler`. Each borrowed `ReplayRecord` carries the requested
`ReplayRange` and the checkpoint-derived `EffectiveStartOffset`, so the
application can preserve replay provenance with its side effects.
`ReplayRecord.Retain` deep-copies the embedded consumed-record bytes.
`ReplayConfig.MaxConcurrentFetches` independently bounds broker fetch requests.
Replay applies the same encoded response, decoded batch, and active decoded
buffer limits as consumer groups. A decompression failure returns its stable
sentinel before replay handler admission and leaves every range checkpoint
unchanged.
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
per partition. Local tiered-storage retention additionally preserves Kafka's
`-2` inheritance sentinel and is accompanied by the effective remote-storage
and remote-copy-disable flags. Visibility fields distinguish returned false or
sentinel values from version-dependent broker omission.
`Inspector.ConsumerGroupLag` returns bounded classic-group
coordinator, state, protocol, member identity, copied assignment,
committed-offset, and lag inspection. Members are sorted by member ID and their
assignments by topic and partition. It requests KIP-447 stable committed
offsets so pending transactional commits resolve within the request deadline
on supported brokers. Topic and group methods require explicit target lists,
and every operation derives `InspectorConfig.RequestTimeout`.
`Inspector.ConsumerProtocolGroupLag` is the separate KIP-848 path. It returns
group and assignment epochs, server assignor, member epochs and types,
subscriptions, optional static-instance and rack identity, current and target
assignments, stable committed offsets, log bounds, and lag. It preserves
current and target assignments as distinct reconciliation states and does not
reinterpret a classic group.
`Inspector.InspectTopics`, `Inspector.InspectConsumerGroups`, and
`Inspector.InspectConsumerProtocolGroups` execute one independent request per
target under that shared deadline and preserve the caller's input order.
`InspectorConfig.MaxConcurrentInspections`, defaulting to four, bounds those
requests. Each result retains the target-specific error and its stable
`ErrorCategory`; any failed target makes the aggregate error
`ErrInspectionTargetsFailed` without discarding successful states. `Topics`,
`ConsumerGroupLag`, and `ConsumerProtocolGroupLag` remain fail closed across
their complete target lists. Kafka's successful `Dead` description for an
unknown classic group is state, not an error.
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

`kafkaservice.NewProducer` retains an explicit concrete producer. Its optional
startup and readiness callbacks use service-owned bounded contexts. A non-nil
shutdown callback transfers flush and close ownership; an omitted callback
keeps the resource shared. `Producer.Publish` requires correlation values in
context, clones the record, creates a child hop, rejects application-supplied
correlation fields, replaces configured trace fields, and gives the concrete
publish callback the child context. Configured `MessageLimits`, defaulting to
`DefaultMessageLimits`, are checked before the clone and after metadata
injection. Stop rejects new publishes and joins admitted callbacks. Failed
shutdown remains retryable; concurrent callers
share one attempt, and the first successful attempt makes later shutdown
idempotent. Startup, readiness, publish, handler, run, and shutdown callback
panics become secret-safe `CallbackPanicError` values. Panic values are not
retained, startup panic cleanup follows the normal transferred-resource path,
and shutdown panics leave cleanup retryable.

`kafkaservice.NewHandler` creates a fresh request ID for every record delivery.
Trusted metadata preserves correlation and converts the producer request ID to
causation; the default starts a new local workflow. Malformed correlation is
replaced by default and can be rejected explicitly. A configured caller-owned
OpenTelemetry propagator extracts trace headers independently of correlation
trust from a deep copy, so propagator mutation cannot alter the borrowed
application record. `kafkaservice.NewConsumer` composes that handler with an
explicit run callback and returns a service plan whose task stops intake and
joins handlers before its component closes the consumer. Kafka retry,
settlement, topic, partition, and dead-letter policy remains in this package
and the application.

`ProducerConfig.CompressionPreferences` is an ordered, constructor-copied list
of `CompressionCodec` values. An empty list defaults to Snappy followed by no
compression. Duplicate codecs and ineffective orders are rejected, and
`CompressionNone` may appear only last.

The canonical machine-checked exported API is in `api/baseline.txt`.
