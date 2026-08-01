# Consumer groups and rebalances

The package consumer is Kafka-specific and at least once. It joins one explicit
consumer group, subscribes to explicit topics, disables automatic commits, and
blocks franz-go rebalances while one bounded poll is handled and settled. It is
not a queue-style worker with nack or visibility-timeout semantics.

## Assignment ownership

The consumer installs fast internal assigned, revoked, and lost callbacks.
Cooperative assignments merge newly added partitions, revocations remove only
the partitions Kafka reports, and a fatal loss clears all ownership. Every
callback advances a package-local assignment epoch, including empty lifecycle
callbacks. That epoch is a local settlement fence and diagnostic sequence; it
is not Kafka's broker generation or member ID.

`Assignment` returns a sorted copied snapshot and any fail-closed assignment
validation error. `MaxAssignedPartitions` defaults to 1,024 and is configurable
from 1 through 65,536. Unsubscribed topics, negative or duplicate partitions,
and assignment growth beyond the limit clear tracked ownership and stop the
runner before another handler. A fatal loss followed by a new assignment is
the recovery boundary.

`BlockRebalanceOnPoll` serializes these callbacks with the active bounded poll.
The runner additionally verifies the same local epoch before handling and
before committing. If ownership changes across that boundary, it returns
`ErrConsumerOwnershipLost` and does not submit stale offsets. Broker generation
loss during a commit can still surface as a commit error with an ambiguous
per-partition outcome and can cause redelivery.

The consumer also installs franz-go's blocked-callback signal. Once a
rebalance is waiting, the runner admits no later record from the poll.
`RebalanceCancelHandler`, the zero-value policy, cancels every active handler
context with `ErrConsumerRebalance`; those records are not settled even if a
handler returns another error, and both error identities are retained.
`RebalanceDrainHandler` lets only handlers already active finish and settles
their successful results before releasing the rebalance. No worker admits
another callback after the signal. Earlier successful contiguous prefixes
remain committable because franz-go still holds the poll's rebalance gate until
the commit attempt completes.

The pinned Confluent Local 7.5.0 fixture exercises both policies with eager
members and a handler already in flight. Under `RebalanceCancelHandler`, the
joining member causes the active handler to return `ErrConsumerRebalance`, the
source offset remains unsettled, and a replacement member receives the record
again. Under `RebalanceDrainHandler`, the blocked-rebalance observation occurs
while the first handler remains active; releasing that handler lets it finish
and commit offset 1 before the poll gate opens, and the joining member then
receives only the subsequently published record at offset 1.

A pinned three-broker Apache Kafka 4.3.1 fixture extends this boundary across
two co-located partition leaders and two operating-system processes. A bounded
minimum fetch admits both partitions into one poll. Cancel mode observes the
blocked rebalance, cancels both active handlers, commits neither partition, and
redelivers both offset-zero records to the replacement member. Drain mode
observes the same blocked boundary, releases both active handlers, commits
offset one for each partition before ownership transfer, and lets the
replacement begin at offset one on both partitions.

The same pinned three-broker runtime now proves two additional ownership
boundaries. During a cooperative join, the original member drains and commits
both in-flight partitions before Kafka revokes exactly one; the retained and
replacement members then independently process offset one on their disjoint
partitions. When an administrator removes an active static member while its
handler is in flight, the blocked drain can finish its application work but
Kafka rejects the stale-generation offset commit with `UNKNOWN_MEMBER_ID`.
The package reports the one-partition ownership-loss callback, clears the
assignment, leaves the offset unsettled, and a replacement receives offset
zero. This is at-least-once behavior: the completed application work can be
duplicated after broker-forced loss.

## Configuration

`ConsumerConfig` requires brokers, client ID, group ID, topics, and an earliest
or latest reset policy. Construction validates all policy before franz-go
allocates the client. Fetch concurrency, minimum, aggregate, and per-partition
bytes,
the hard encoded broker response, each decoded record batch, active decoded
buffers, poll records, fetch wait, session, rebalance, heartbeat, handler,
commit, and dial durations are bounded.
The package rejects a backend poll above `MaxPollRecords` with
`ErrTooManyFetchedRecords` before invoking a handler, even though franz-go is
also configured with that limit.
`ConsumerConfig.Validate` applies the same defaults and checks without
allocating a client. Optional `ConsumerConfig.Observers` report payload-free
record, partition-batch, commit, complete poll, assignment, revocation,
ownership-loss, blocked-rebalance, and group-management-error outcomes. They
execute synchronously, can run concurrently across partition workers and
franz-go callback goroutines, and cannot re-enter consumer mutation or
lifecycle operations. Processing observers run before the poll releases its
rebalance gate; lifecycle observers run only after package assignment or
rebalance state locks are released. See the
[observability guide](observability.md).
The heartbeat, handler, and commit deadlines together must be strictly less
than the rebalance timeout. This preserves time for franz-go to detect the
rebalance, finish or cancel all active handlers, attempt the contiguous
commit, and release the poll gate.
`Limits` defaults to `DefaultMessageLimits`. Subscribed topics must fit its
topic bound, and each fetched record must fit every key, value, header count,
header key, individual header value, and aggregate header bound before the
package copies header metadata or runs a handler.
Compressed batches are decoded through the package's bounded decoder before
this record validation. `ErrFetchBatchTooLarge` identifies one expanded batch;
`ErrFetchDecompressedBufferFull` identifies aggregate active decoded storage.
Both are non-retryable `ErrorOversized` poll outcomes for the current policy,
admit no record from the rejected batch, and do not commit its source offset.
When a response contains earlier complete batches, franz-go may return that
contiguous prefix and retry the rejected trailing batch on a later poll; only
the successfully handled prefix can be committed. Records are
recycled only after processing, observations, and settlement complete; retain
bytes explicitly when they must outlive the callback.
This reclaimable active-buffer lifecycle applies to Kafka record batches
(magic 2). Legacy compressed message sets are unsupported; see the
[compatibility matrix](compatibility.md).

## Consumer infrastructure failures

Poll or group-session, offset-commit, and dynamic-member leave failures are
returned as redacted `ConsumerError` values. `Operation` identifies `poll`,
`commit`, or `leave`; `Category` distinguishes retryable, authorization,
fencing, timeout, cancellation, shutdown, and permanent failures; and
`Retryable` permits an explicit later bounded attempt. The original cause is
retained for deliberate `errors.Is` and `errors.As` checks but is never included
in `ConsumerError.Error`.

`RunOnce` and `RunBatchOnce` return a retryable failure after franz-go exhausts
one internal retry cycle. The application chooses whether and how often to call
again within its own context; the package does not hide an infinite loop.
`Run` returns that failure instead of silently restarting the group forever.
Handler failures remain unchanged application errors so retry and dead-letter
decorators can preserve their identity. A static-member fence additionally
returns `ErrConsumerFatal` and `ErrConsumerInstanceFenced` and permanently
fences the consumer instance.

Client and group IDs must be valid UTF-8 without whitespace padding or control
characters and are limited to 255 bytes. `ShutdownTimeout` defaults to 30
seconds and is bounded from 100 milliseconds through 15 minutes.

`InstanceID` and `Rack` are optional UTF-8 identifiers of at most 255 bytes.
Whitespace padding, control characters, invalid UTF-8, and oversized values
fail construction.
Instance IDs must be deployment-unique within a group. Rack values must match
the broker deployment's rack naming convention.

## Balance policy and rolling deployment

`BalanceCooperativeSticky` is the zero value and safe default for a new group.
It incrementally revokes moved partitions rather than requiring every member to
revoke every assignment. This follows
[KIP-429](https://cwiki.apache.org/confluence/display/KAFKA/KIP-429%3A%2BKafka%2BConsumer%2BIncremental%2BRebalance%2BProtocol).

Do not introduce a cooperative-only member directly into an existing eager
group. Use two complete rolling deployments:

1. configure every member with `BalanceEagerToCooperative`, which advertises
   eager sticky first and cooperative sticky second;
2. after every old eager-only member has gone, configure every member with
   `BalanceCooperativeSticky`.

`BalanceEagerSticky` keeps a group on eager rebalancing when compatibility
requires full revocation. Reversing an established cooperative group to eager
is not presented as a safe rolling operation.

The pinned three-broker Apache Kafka 4.3.1 fixture executes this migration with
three distinct operating-system child processes. An eager-only member and a
mixed eager-to-cooperative member negotiate `sticky`; after the eager-only
process shuts down, the migration member retains both partitions. Introducing
a cooperative-only process then negotiates `cooperative-sticky`, and that
process retains both partitions after the migration member shuts down. Every
stable transition verifies the exact client identities and that partitions 0
and 1 are each owned exactly once. This proves protocol negotiation and
stable assignment results for the exercised rollout; it does not prove
application handler behavior during every possible rebalance timing.

## Static membership and rack awareness

Setting `InstanceID` opts into Kafka static membership. A normal client close
does not explicitly leave that static member, so a restart using the same ID
can rejoin within the session timeout without forcing an immediate rebalance.
Removing a static member is an explicit Kafka administrative operation. A
duplicate live ID fences the older member. Once the package observes
`FENCED_INSTANCE_ID`, the consumer enters a terminal state: the active call
returns both `ErrConsumerFatal` and the stable
`ErrConsumerInstanceFenced` policy sentinel while retaining the broker cause,
and every later `Run`, `RunOnce`, or `RunBatchOnce` call fails before polling
or invoking a handler. Callers do not need franz-go error types to classify
this state. Automatic rejoin would contend with the replacement and is
therefore not attempted. Shut the fenced consumer down, correct the
deployment-unique instance identity, and construct a new consumer.
See [KIP-345](https://cwiki.apache.org/confluence/display/KAFKA/KIP-345%3A%2BIntroduce%2Bstatic%2Bmembership%2Bprotocol%2Bto%2Breduce%2Bconsumer%2Brebalances)
for the broker protocol and administrative model.

Setting `Rack` asks compatible brokers to prefer an eligible replica in that
rack. It does not create rack metadata, place replicas, or guarantee a local
replica. Infrastructure owns broker rack configuration and replica placement.
The pinned three-broker Apache Kafka 4.3.1 fixture assigns one rack per broker
and enables Kafka's rack-aware replica selector. A separate consumer process
first establishes routing to a non-leader replica in its rack on an empty
topic, permits only one fetch in flight, and then handles and commits a source
record after that fetch completes on the exact follower. This proves locality
only for the exercised replicated topic and healthy in-sync follower; Kafka
can fall back when no eligible local replica is available.
See [KIP-392](https://cwiki.apache.org/confluence/display/KAFKA/KIP-392%3A%20Allow%20consumers%20to%20fetch%20from%20closest%20replica).

## Processing, settlement, and redelivery

Records remain sequential within a partition. A handler success and unchanged
assignment ownership are required before settlement. At the first failure in
one partition, later records from that partition in the current poll are
skipped. Its successful prefix and successful independent partitions are
committed together. A failed commit has an ambiguous per-partition broker
outcome, leaves `PollResult.Committed` at
zero, and may redeliver records whose side effects already completed.

`MaxConcurrentHandlers` defaults to one and can explicitly permit 1 through 64
simultaneous callbacks across independent topic partitions. The runner creates
no more workers than the smaller of that limit and the partitions in the
bounded poll. A worker owns one partition at a time and invokes its records in
fetched order; two callbacks for the same partition never overlap.
Cross-partition start and completion order is scheduler-dependent.
When several partitions fail, the returned error is the first failure in the
stable partition order of the poll. Successful independent partition results
remain committable.

A fetched record outside `Limits` follows the same partition-local failure
path without invoking the handler. Its error identifies the rejected field,
later records in that partition are skipped, and valid independent partitions
may still advance.

`RunBatchOnce` provides a deliberately different settlement contract. It
groups the bounded poll by topic partition, preserves record order within each
partition, and invokes `BatchHandler` once per non-empty partition batch. A nil
handler result makes the complete batch committable at its final record. An
error, panic, timeout, ownership loss, or rebalance cancellation makes none of
that partition batch committable; successful independent batches can still
advance. The package does not split a failed batch into a guessed successful
prefix and does not claim atomic application side effects or cross-partition
settlement.

The `ConsumedBatch.Records` slice is owned for the handler call, while record
bytes have the same borrowed lifetime as per-record handling. `Retain` returns
an owned slice with deeply copied record bytes.

`NewBatchFailureHandler` keeps failure decisions at this same settlement unit.
It validates and retains the complete source batch before the first handler
attempt, gives each bounded retry an isolated copy, and never interprets an
application error as a partial success. Stop, exhausted retry, failed delegate,
or failed publication leaves the entire partition batch unsettled. A nil
delegate result or a definite successful publication of every target record
resolves the complete batch and allows normal settlement at its final offset.

Retry-topic and dead-letter modes use one bounded target publication call with
input-ordered results and source metadata on every record. Target keys can
place those records on different partitions and franz-go can use multiple
Kafka requests; the call is not atomic. If only some target records are
acknowledged,
`FailureHandlingError.DeliveryResults` exposes those outcomes while the source
batch remains unsettled. Retrying after redelivery can duplicate acknowledged
target records. This explicit duplicate window is preferable to committing a
source prefix the application never identified as durably successful.

Handlers must be idempotent and honor cancellation and their context deadline.
When `MaxConcurrentHandlers` exceeds one, the same handler value can be called
concurrently and must synchronize its own shared state.
Go context cancellation is cooperative: the package does not run application
callbacks in disposable goroutines and cannot forcibly stop a handler that
ignores its context. Such a handler can still exhaust the broker rebalance
timeout and lose ownership. A context cause observed after callback return
overrides a nil handler result and prevents settlement. A canceled runner
admits no new callback even if the backend returns already-buffered records.
Retain a consumed record before storing its bytes beyond the handler call.

### Retry, retry topics, and dead letters

Kafka has no per-record nack or visibility timeout. `NewFailureHandler`
decorates the per-record handler with explicit stop, bounded category-selected
in-process retry, versioned retry-topic, versioned dead-letter, or
application-delegated policy. The default terminal mode stops and returns a
redacted error without settling the record.

A definite retry or dead-letter publish result turns that decorated handler
call into a success. The consumer then submits its normal contiguous source
commit. Those are separate effects: a crash or ambiguous commit between them
can publish the target record more than once. Failed publication leaves the
source unsettled. Use `TransactionProcessor` for a Kafka-only atomic
source-offset and output transaction. Complete configuration, metadata, and
failure-window guidance is in the
[retry and dead-letter guide](retry-dead-letter.md).

`NewFailureHandler` does not apply to `RunBatchOnce`; a failed partition batch
does not identify a safe individual record to settle or reroute. Use
`NewBatchFailureHandler` only when retrying, rerouting, or delegating the
complete batch is the intended settlement decision.

### Pause and resume

`PausePartitions` stops future fetches only for explicit `TopicPartition`
values whose topics are in the immutable subscription. `ResumePartitions`
removes those explicit pauses; resuming a partition that is not paused is a
no-op. Topic names, non-negative partition numbers, duplicates, request count,
and accumulated pause count are validated before changing backend state.
`MaxPausedPartitions` defaults to 256 and is bounded from 1 through 1,024.

Pausing does not cancel a handler, retract records returned by the current
poll, or discard records already buffered by franz-go. Pauses persist across
rebalances until resumed, but a `TopicPartition` does not prove assignment or
generation ownership. Use `Assignment` for a bounded diagnostic snapshot, not
as authority for an external commit. `PausedPartitions` returns a sorted copied
snapshot and remains diagnostic after close. Pause and resume reject calls once
shutdown begins.

## Runner, drain, and shutdown lifecycle

One consumer permits one active `Run`, `RunOnce`, or `RunBatchOnce` call. A
concurrent runner fails with `ErrConsumerBusy`. Within that one runner,
callbacks may overlap only across partitions and only up to
`MaxConcurrentHandlers`.
`Drain` fences new runners and pause/resume mutations, interrupts an idle
broker poll, and waits for an active poll to finish handling and contiguous
settlement. It does not cancel an admitted handler, leave the group, or close
the client. A successful drain removes the fence so another runner can start.
A deadline or cancellation returns both `ErrConsumerDrainIncomplete` and the
context cause; the fence remains until the active runner stops and a later
`Drain` succeeds. Concurrent drains, and shutdown started during a drain,
return `ErrConsumerDrainActive` without changing lifecycle ownership. A fatal
consumer state reached while waiting remains present in the drain result.

`Shutdown` requests the same drain boundary, waits for the active runner, and
closes the client without calling franz-go `Close` while a poll or handler is
active. Applications may still cancel the runner context directly when they
want handler cancellation rather than draining.

The pinned broker fixture starts a continuously polling dynamic member on an
empty assigned partition. `Drain` stops that idle runner, preserves the active
assignment, and permits a later runner to consume and commit the next record.
A separate assigned idle member exits through `Shutdown` without external
runner cancellation and rejects reuse after the group leave and close.

For dynamic members, shutdown performs a context-bounded group leave before
closing the client. Static members intentionally skip that leave so a restart
with the same instance ID retains Kafka's static-membership window. A shutdown
deadline, cancellation, or failed leave returns
`ErrConsumerShutdownIncomplete`, keeps new runs fenced, and leaves shutdown
retriable. It does not claim that an ambiguous broker leave failed. `Close`
uses `ConsumerConfig.ShutdownTimeout`; applications must handle its error.
Concurrent shutdown calls fail with `ErrConsumerShutdownActive`, and completed
shutdown is idempotent.
Each attempt that acquires shutdown ownership emits one payload-free
`ObservationConsumerShutdown` after waiting, group leave, and close finish. An
incomplete attempt and its successful retry are separate observations;
concurrent, observer-reentrant, and already-completed calls emit nothing.

Drain and shutdown never cancel an admitted handler on their own. A pending
Kafka rebalance can cancel it only under `RebalanceCancelHandler`; otherwise
the handler deadline and application-owned runner context remain its bounds. A
handler, commit, or rebalance outcome interrupted before completion can be
redelivered; an application side effect may already have occurred.

## Ownership boundaries

Kafka owns the broker group generation, assignments, offsets, retention, and
broker acknowledgements. franz-go implements the group protocol and heartbeats.
This package owns configuration, bounded assignment tracking and local epochs,
polling, handler invocation, contiguous settlement, and the exposed lifecycle
policy. The application owns durable side effects, idempotency, poison-record
decisions, and deployment-specific IDs.
