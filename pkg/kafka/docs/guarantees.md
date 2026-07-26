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

Automatic commits are disabled. A poll is processed in fetch order and committed
only after all handlers return nil. A handler error or panic leaves the batch
uncommitted. Rebalances are released after each poll.

Delivery is at least once. A crash after a durable side effect but before the
offset commit replays the record. Kafka may partially persist a multi-partition
commit before returning an error, so result counters never claim a failed commit
was wholly persisted or wholly rejected. Side effects must be idempotent.

The integration suite proves Zstandard production, same-key record order,
explicit partition delivery, successful offset commits, and redelivery after
handler failure against
Confluent Local 7.5.0 using franz-go v1.21.5. The container image is pinned by
repository digest. This compatibility fixture does not replace testing against
an application's production broker version and configuration.

## Context and memory

Handler deadlines are cooperative; a handler must honor context cancellation.
Consumed byte slices reference the current fetch. Use `ConsumedRecord.Retain`
before keeping a record beyond its handler call. Configuration and record
bounds prevent unbounded caller-controlled allocation inside this module.
