# Guarantees and failure model

## Producer

The producer explicitly requests all in-sync replica acknowledgements and does
not disable franz-go idempotence. Calls use bounded retries, delivery timeout,
request timeout, dial timeout, buffering, batch size, and record size.

A delivery result with a nil error means Kafka acknowledged the record under
the configured all-ISR acknowledgement policy. It does not prove a downstream
consumer completed and does not make a database/Kafka dual write atomic.

Producer input bytes are copied before they enter franz-go. Synchronous batch
results preserve input order and report every known per-record failure. A
caller canceling its wait cannot retract a record already admitted to the
idempotent producer; the eventual result remains authoritative. Delivery
errors use stable, redacted operational categories while preserving error
identity for programmatic inspection.

Construction copies a required bounded topic allowlist. Every publish mode,
including transaction callbacks, rejects records outside that immutable set
before franz-go admission.

A delivery timeout or exhausted franz-go retry budget is classified by the
condition that ended the attempt; neither classification automatically retries
the application operation. With the required idempotent producer, franz-go
only returns its record-delivery timeout when failing the record is safe. A
bounded dial failure caused by a broker refusing a connection during restart
is a retryable transport failure; the package still does not retry the
application operation after franz-go returns that final delivery result. A
missing delivery result is different: the package classifies it as ambiguous
because no authoritative per-record outcome was supplied. Fatal sequence or
producer-ID state stops the producer instead of allowing franz-go's default
continue-after-data-loss behavior. Callers must replace or explicitly recover
the producer rather than retrying blindly.

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

New groups default to cooperative-sticky balancing. `BalanceEagerSticky` keeps
an eager group eager. Migrating an existing eager group requires one complete
rolling deployment with `BalanceEagerToCooperative`, followed by a second with
`BalanceCooperativeSticky`; the package does not claim a direct mixed rollout
is safe. When `InstanceID` is set, franz-go static-membership close semantics do
not send an ordinary leave-group request, allowing a bounded restart window but
leaving removal to an explicit Kafka administrative action. Instance identity
must be unique within the group or the broker may fence a member. `Rack` only
requests preferred-replica fetching; broker topology determines the result.

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
fail closed.
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
committed-versus-aborted transaction visibility, plus replay interruption,
external-checkpoint resume, out-of-range rejection, cluster/controller
visibility, and topic durability/offset inspection against Confluent Local
7.5.0 using franz-go v1.21.5. Topic inspection in that fixture includes
explicit non-default cleanup, retention, compaction, segment, and
unclean-election configuration. The same fixture proves a live classic static
member's copied identity and two-partition assignment.
The container image is pinned by repository digest. This compatibility fixture
does not replace testing against an application's production broker version
and configuration.

## Context and memory

Handler deadlines are cooperative; a handler must honor context cancellation.
Consumed byte slices reference the current fetch. Use `ConsumedRecord.Retain`
before keeping a record beyond its handler call. Configuration and record
bounds prevent unbounded caller-controlled allocation inside this module.
Consumer fetch policy limits concurrent requests, response bytes, and bytes per
partition. These are compressed-fetch controls rather than a strict heap cap:
franz-go may accept one batch above the partition limit to make progress, and
decompression can expand the retained bytes. Broker record limits remain part
of the deployment safety boundary.
