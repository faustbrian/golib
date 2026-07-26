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

## Consumer

Automatic commits are disabled. A poll is processed in fetch order, sequentially
within each partition. A partition stops at its first handler error, panic, or
timeout, and later fetched records in that partition are skipped. The highest
successfully processed record before that failure is committed, as are
successful prefixes from independent partitions. The first handler failure is
returned after the bounded commit attempt. If that commit also fails, the
returned error preserves both identities. Rebalances are released after each
poll. A waiting rebalance stops admission from that poll. The default policy
cancels the active handler with `ErrConsumerRebalance`; the explicit drain
policy permits that handler alone to finish and settle. Earlier safe prefixes
can still commit before the rebalance gate is released. Handler cancellation
is cooperative, and the configured heartbeat, handler, and commit deadlines
must fit strictly inside the rebalance timeout.

Delivery is at least once. A crash after a durable side effect but before the
offset commit replays the record. `PollResult.Committed` counts processed records
covered by a wholly successful commit call, not the number of partition offsets
sent. Kafka may partially persist a multi-partition commit before returning an
error, so the counter remains zero after a failed commit and does not claim the
request was wholly persisted or wholly rejected. Side effects must be
idempotent.

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

The integration suite proves Zstandard production, same-key record order,
explicit partition delivery, per-partition contiguous settlement, successful
offset commits, redelivery after handler failure, eager group membership,
partition pause/resume, and a static member restart using the same instance ID
against Confluent Local 7.5.0 using franz-go v1.21.5. The container image is
pinned by repository digest. This compatibility fixture does not replace
testing against an application's production broker version and configuration.

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
