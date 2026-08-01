# Guarantees and failure model

## Producer

The producer explicitly requests all in-sync replica acknowledgements and does
not disable franz-go idempotence. Calls use bounded retries, delivery timeout,
request timeout, dial timeout, buffering, batch size, and record size.

A delivery result with a nil error means Kafka acknowledged the record under
the configured all-ISR acknowledgement policy. It does not prove a downstream
consumer completed and does not make a database/Kafka dual write atomic.

Producer input bytes are copied before they enter franz-go. Synchronous batch
results preserve input order and report every known per-record failure. Since
franz-go completes records by partition independently, the package restores
results by the identity of each owned backend record rather than assuming
callback completion order is caller order. Missing, duplicate, nil, or unknown
backend results fail closed as ambiguous delivery evidence instead of being
silently assigned or discarded.
`PublishAsync` uses caller cancellation while admission is blocked, then
detaches the admitted record from later caller cancellation; its eventual
buffered result remains authoritative. A synchronous caller context remains
authoritative throughout delivery. Delivery errors use stable, redacted
operational categories while preserving error identity for programmatic
inspection.

Construction copies a required bounded topic allowlist. Every publish mode,
including transaction callbacks, rejects records outside that immutable set
before franz-go admission.

Non-transactional producers permit bounded cancellation of an in-flight
idempotent record. This preserves the package delivery bound after a broker
writes a record but its response is lost; franz-go's internal retries remain
idempotent, but a new application submission cannot reuse the canceled
producer sequence. Record delivery timeout and retry exhaustion are therefore
`ErrorAmbiguous`; delivery cancellation and deadline expiry are classified the
same way because the package cannot prove that Kafka rejected an admitted
record. These errors retain their underlying identity and must not be retried
blindly. A bounded dial failure before a request reaches Kafka remains a
retryable transport failure. A missing delivery result is also ambiguous
because no authoritative per-record outcome was supplied. Fatal sequence or
producer-ID state stops the producer instead of allowing franz-go's default
continue-after-data-loss behavior.

Retry backoff uses a 250 millisecond to 1 second default range,
exponential growth, and bounded per-client jitter. Failed-partition metadata
refresh uses the larger of the configured retry minimum and a reviewed 250
millisecond floor. `DeliveryTimeout` is a rough franz-go record bound evaluated
around requests. The package also gives every
non-transactional delivery a context deadline of `DeliveryTimeout +
RetryBackoffMax`, which is the policy-level maximum wait. `ShutdownTimeout`
must cover that combined interval.

Keyed production is the default ordering policy. Unkeyed records are accepted
only when configured explicitly; their partition is selected by the configured
adaptive franz-go policy. A record may instead select one exact non-negative
partition explicitly. Explicit selection overrides key hashing, fails through
the delivery result when the partition does not exist, and does not imply
cross-partition order.

`Drain` rejects new operations while resolving admitted records. `Shutdown`
fences new production before draining and closes only after a successful
bounded flush. An incomplete shutdown retains ownership for a retry or an
explicit `Abort`. `Close` applies the configured `ShutdownTimeout`, returns the
same incomplete-drain error, and never substitutes franz-go's record-dropping
direct close path. Drain, abort, and shutdown wait for operations that already
started to finish backend admission before acting on the buffer.
Every shutdown attempt that acquires producer lifecycle ownership emits one
payload-free observation after it completes. An incomplete attempt and a later
successful retry remain distinct; idempotent calls after completion emit
nothing.

The pinned single-broker compatibility fixture proves ordered batch and
asynchronous delivery metadata against the records subsequently read from
Kafka. It also proves that graceful shutdown drains an admitted asynchronous
record before closing and fences later production. A broker-enforced
client-ID byte-rate quota also proves that delivery can succeed while the
observer reports Kafka's positive post-response throttle interval. The
throttle is request-level metadata because a produce request can contain many
records; it is not attributed to an individual delivery result. The
three-broker Apache fixture additionally proves exact input attribution when
one topic accepts its batch record and another rejects an oversized batch, and
proves ambiguous non-transactional and transactional outcomes after matching
broker responses are lost.

Producer transaction begin, commit, and abort failures use redacted
`TransactionError` values. Fencing, authorization denial, and fatal producer
state are definitive failures rather than being mislabeled as unknown commit
outcomes. `OperationNotAttempted` and `TransactionAbortable` are reported as
abortable, known-not-committed results after the package attempts the required
bounded abort. Other unclassified commit or abort failures are ambiguous and
must be reconciled before the producer is reused.

`TransactionProcessor` consumes only committed source records and disables
automatic commits. One bounded poll is the settlement unit: every fetched
record must pass package limits, complete its handler, and resolve every
transactional output delivery before the processor asks Kafka to commit the
outputs and all source offsets atomically. Any validation, handler, panic,
processing-timeout, or delivery failure aborts the complete poll. Outputs
acknowledged inside an aborted transaction remain visible to
`read_uncommitted` inspection but not to `read_committed` consumers; source
records remain eligible for redelivery.

The processor does not partially settle successful partitions from a failed
transactional poll because franz-go's group transaction commits the complete
poll position. `MaxPollRecords` bounds that all-or-nothing unit. A rebalance
before transaction completion converts the commit to an abort and returns
`ErrTransactionNotCommitted`. Begin, commit, or abort lifecycle failure fences
the processor so it cannot silently poll past unsettled source records.
Unknown end outcomes retain `ErrTransactionOutcomeUnknown` and require
reconciliation before replacement. These guarantees apply only to Kafka
source offsets and Kafka output records. Application side effects outside
Kafka are neither atomic nor exactly once.

`MaxOutputRecords` and `MaxOutputBytes` bound all output attempts in one
transaction independently of franz-go's instantaneous buffer limits. Exceeding
either limit aborts the source poll even if the handler ignores the publish
error.
Consumer and transaction-processor shutdown observations follow the same
attempt model: waiting, group leave, close, and a stable incomplete outcome are
reported only after an attempt acquired lifecycle ownership. Invalid,
concurrent, observer-reentrant, and already-completed calls emit nothing.

## Consumer

Automatic commits are disabled. A poll is processed in fetch order, sequentially
within each partition. A partition stops at its first handler error, panic, or
timeout, and later fetched records in that partition are skipped. The highest
successfully processed record before that failure is committed, as are
successful prefixes from independent partitions. The first handler failure is
returned after the bounded commit attempt. If that commit also fails, the
returned error preserves both identities. Rebalances are released after each
poll. A waiting rebalance stops admission from that poll. The default policy
cancels every active partition handler with `ErrConsumerRebalance`; the
explicit drain policy permits only handlers already active to finish and
settle. Earlier safe prefixes can still commit before the rebalance gate is
released. Handler cancellation is cooperative, and the configured heartbeat,
handler, and commit deadlines must fit strictly inside the rebalance timeout.
A nil callback result does not settle a record when its handler context already
has a timeout, cancellation, or rebalance cause. A canceled runner does not
admit another record or batch callback from already-buffered fetch results.

`Consumer.Drain` fences new runners, cancels only an idle broker poll, and
allows every handler already admitted from the current bounded poll to finish
and submit its contiguous settlement. It does not leave the group or close the
client. A context failure returns `ErrConsumerDrainIncomplete` and retains the
fence for a retry. `Shutdown` uses the same boundary before dynamic group leave
and close; neither operation cancels an admitted handler.

Application callbacks default to sequential execution. An explicit
`MaxConcurrentHandlers` from 2 through 64 permits overlap only across
independent partitions. Each partition remains sequential, worker count never
exceeds the configured limit, and commit/error aggregation follows stable
poll-partition order rather than scheduler completion order. A shared handler
must be concurrency-safe when this policy is enabled.

Delivery is at least once. A crash after a durable side effect but before the
offset commit replays the record. `PollResult.Committed` counts processed records
covered by a wholly successful commit call, not the number of partition offsets
sent. Kafka may partially persist a multi-partition commit before returning an
error, so the counter remains zero after a failed commit and does not claim the
request was wholly persisted or wholly rejected. Side effects must be
idempotent.

`FailureHandler` does not add a queue-style nack or visibility timeout. Its
zero `FailureModeStop` returns a redacted error and leaves the failed source
record unsettled. Optional in-process retries are limited by attempt count,
selected stable categories, capped exponential backoff, the outer handler
deadline, and cancellation. Cancellation, including the rebalance-cancellation
cause, prevents a terminal target publication or delegated success from
settling the record.

A definite retry-topic or dead-letter publish result resolves the decorated
handler, after which the normal consumer submits its source offset commit. A
failed or panicking publisher does not resolve the handler. Publication and
source commit are separate Kafka effects: a crash or ambiguous commit after
publication can duplicate the target record. The package preserves source
coordinates and application headers so target consumers can deduplicate, but
it does not claim lossless deduplication for application side effects. Use
`TransactionProcessor` when target output and source offsets must commit in one
Kafka transaction.

Failure target records own copies of the original key, value, ordered headers,
and timestamp. Eleven appended schema, kind, target-version, source-coordinate,
attempt, and category headers must fit the configured record limits. Handler
error text is never copied. A record that cannot preserve all original data
and package metadata fails closed and remains unsettled. Publishing to the
runtime source topic is rejected to prevent an accidental self-loop.

`RunBatchOnce` changes the handler boundary, not the delivery guarantee. One
call receives records from exactly one topic partition. Only a nil batch result
makes its final record committable; any failure leaves the entire partition
batch unsettled. Independent successful partition batches may commit in the
same cycle. Application work performed before a failed batch return can be
repeated.

`BatchFailureHandler` preserves that whole-partition-batch settlement unit.
In-process retries repeat every record and can repeat partial application side
effects from a failed attempt. Retry-topic and dead-letter actions submit every
preserved source record through one bounded publication call with input-ordered
results. The call can span target partitions and Kafka requests and is not
atomic. Only an
exact set of definite successful delivery results resolves the source batch.
Any publisher error, missing or inconsistent result, or per-record delivery
failure leaves all source offsets unsettled. Available partial results remain
programmatically inspectable, and successful target records can be duplicated
when the source batch is redelivered.

New groups default to cooperative-sticky balancing. `BalanceEagerSticky` keeps
an eager group eager. Migrating an existing eager group requires one complete
rolling deployment with `BalanceEagerToCooperative`, followed by a second with
`BalanceCooperativeSticky`; the package does not claim a direct mixed rollout
is safe. When `InstanceID` is set, franz-go static-membership close semantics do
not send an ordinary leave-group request, allowing a bounded restart window but
leaving removal to an explicit Kafka administrative action. Instance identity
must be unique within the group. A duplicate live identity causes Kafka to
fence the older member. The package converts the observed fence into a
permanent lifecycle state returning `ErrConsumerFatal` and
`ErrConsumerInstanceFenced`, retains the broker cause without requiring callers
to import franz-go, and rejects every later runner before polling. It does not
automatically rejoin and fight the replacement. `Rack` only requests
preferred-replica fetching; broker topology determines the result.

Explicit partition pauses are bounded by `MaxPausedPartitions` and persist
until resumed. They affect future fetches, not records already buffered or
returned in the active poll, and they do not establish assignment ownership.
Broker-controlled assignment state is bounded by `MaxAssignedPartitions`.
Assigned, revoked, and lost callbacks advance a package-local epoch that fences
handler admission and settlement; it is not Kafka's broker generation ID.

Replay never joins a consumer group or commits group offsets. Its zero-value
side-effect policy permits planning but rejects handler execution. An explicit
checkpoint selects the next offset per configured partition and is returned
after every outcome; progress advances only after a successful handler call.
Readers are single-use so an advanced direct-consumer cursor cannot be mistaken
for a fresh replay.
Execution validates effective starts and exclusive ends against current broker
log-start and high-watermark offsets before invoking a handler. Exact
assignments then disable franz-go offset reset, so broker out-of-range errors,
missing offsets, handler timeout, cancellation, panic, and record-limit
failures stop with an incomplete range rather than silently moving forward.
`ProgressTimeout` bounds empty or skipped fetches that cannot advance any
checkpoint, including an exact range emptied by compaction.
Per-partition order is preserved, but no global partition order or exactly-once
application side effect is claimed. The local plan does not prove current
broker retention.

## Inspection and health

Inspection is read-only and requires explicit bounded topic or group targets.
Cluster inspection copies at most `MaxMetadataBrokers` validated brokers and
reports whether Kafka supplied a cluster ID and a controller present in that
broker set. Topic inspection copies at most `MaxMetadataPartitions` and
preserves replica preference order while returning sorted ISR and offline
replica sets, leader epochs, beginning offsets, exclusive end offsets, and the
effective `min.insync.replicas`, cleanup, retention, compaction, segment, and
unclean-election policy. Selected configuration is required, validated, and
bounded. Millisecond values preserve Kafka's signed 64-bit domain rather than
overflowing `time.Duration`; retention limits preserve `-1`, and retention
bytes are explicitly per partition.

Configuration inspection is diagnostic rather than a retention guarantee.
Deletion occurs at segment granularity and asynchronously, compaction depends
on cleaner state, beginning offsets remain the readable-range evidence, and
unclean-election configuration does not report election history. Tiered
storage local-retention overrides are not currently exposed.

Classic consumer-group inspection returns copied coordinator, group state,
protocol type and assignor, member identity, and current assignments alongside
committed-offset lag. Member IDs and assignments are sorted. Duplicate member
or instance IDs, overlapping partition ownership, non-consumer assignment
encodings, invalid parsed metadata, and excessive members or assignment entries
fail closed. Committed-offset requests require stable offsets, so KIP-447
brokers resolve pending transactional commits within the configured request
deadline rather than returning the pre-transaction offset.
The current implementation uses the classic `DescribeGroups` path and does not
claim KIP-848 consumer-protocol group inspection.

All inspector broker operations derive `RequestTimeout` from the caller context.
Missing, inconsistent, excessive, unauthorized, or unavailable required state
returns an error instead of a partial success. A caller therefore cannot infer
that omitted partitions, replicas, offsets, or durability configuration are
healthy. Multi-target typed partial results are not implemented yet.
Optional inspector observers receive only bounded aggregate counts and
health/readiness state. They never receive cluster IDs, broker hosts, target
names, group members, assignments, or lag coordinates. A conclusive readiness
probe emits a dependency-health event followed by the hysteresis decision.

`Inspector.DependencyHealth` is only current bounded broker connectivity, and
`Health` is its compatibility alias. `Readiness` keeps a previously ready
instance ready across fewer than three consecutive failures by default and
requires two consecutive successes for initial or recovered readiness. The
latest dependency error remains separately visible. `Liveness` reports only
whether the inspector is locally open; it is not complete process liveness.
None of these signals proves required topics, authorization, durability,
consumer progress, transaction health, or application correctness.
Inspector close is idempotent and returns `ErrObserverReentry` rather than
allowing an active inspector observer to re-enter lifecycle work.

The integration suite proves Zstandard production, same-key record order,
explicit partition delivery, per-partition contiguous settlement, successful
offset commits, redelivery after handler failure and failed dead-letter
publication, acknowledged retry-topic and dead-letter metadata followed by
source settlement, concurrent record and batch handling across independent
partitions with sequential order inside each partition, eager group membership,
partition pause/resume, a static member restart using the same instance ID, and
duplicate-live-instance fencing into a terminal package lifecycle state,
committed-versus-aborted transaction visibility, plus replay interruption,
external-checkpoint resume, out-of-range rejection, cluster/controller
visibility, and topic durability/offset inspection against Confluent Local
7.5.0 using franz-go v1.21.5. Topic inspection in that fixture includes
explicit non-default cleanup, retention, compaction, segment, and
unclean-election configuration. The same fixture proves a live classic static
member's copied identity and two-partition assignment.
The container image is pinned by repository digest and the fixture rejects a
runtime version other than `7.5.0-ccs`. This compatibility fixture does not
replace testing against an application's production broker version and
configuration.

## Context and memory

Handler deadlines are cooperative; a handler must honor context cancellation.
Consumed byte slices reference the current fetch. Use `ConsumedRecord.Retain`
before keeping a record beyond its handler call. Configuration and record
bounds prevent unbounded caller-controlled allocation inside this module.
Consumer fetch policy limits concurrent requests, response bytes, and bytes per
partition. Kafka may accept one batch above the partition fetch limit to make
progress, so `BrokerMaxReadBytes` is the separate hard encoded-response cap.
The package replaces franz-go's multi-gigabyte decompression ceiling with an
explicit per-batch limit and a client-wide active decoded-buffer budget.
Decoded storage is recycled only after observations, handlers, settlement, or
transaction completion no longer need the borrowed record. A rejected batch
never reaches a handler and never advances a source offset. Broker and topic
record limits remain part of the deployment safety boundary.
If a response contains complete batches before a rejected trailing batch,
franz-go may return that contiguous prefix and retry the rejected batch on a
later poll. The package may process and settle only the returned prefix; it
never commits across the rejected batch.

Active-buffer reclamation is supported for Kafka record batches (magic 2), the
format produced by the supported Kafka broker versions. Legacy compressed
message sets (magic 0 or 1) remain readable through franz-go and still obey the
hard encoded-response and decoded-batch limits, but franz-go does not attach
their decompression allocation to returned records. The package therefore
cannot reclaim that budget entry when processing completes. Repeated legacy
reads eventually fail closed with `ErrFetchDecompressedBufferFull`; operators
must rewrite legacy segments or replace the client. This format is unverified
and unsupported rather than silently claimed to have the modern lifecycle
guarantee.
