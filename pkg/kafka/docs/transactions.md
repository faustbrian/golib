# Transactions

Transactions require a stable, unique `TransactionalID` for each live producer
identity. Reusing an identity fences the older producer.
Transactional IDs must be valid UTF-8 without whitespace padding or control
characters and are limited to 255 bytes.

`RunTransaction` requires exclusive producer ownership. It fails when another
transaction, maintenance operation, or admitted non-transactional production
is active; it does not hold a package lock across Kafka IO or the application
callback. It waits for every publish started through the callback capability
and closes that capability before commit. Callback failure or panic triggers a
bounded safe buffer abort and transaction abort.

Transactional records cannot use franz-go's per-record idempotent cancellation
because it is incompatible with transactional IDs. The package instead bounds
each `Transaction.Publish` by the earlier caller deadline or configured
delivery timeout plus maximum retry backoff. If that bound expires after the
record may have been sent, the package cancels and closes the entire client,
returns an ambiguous `DeliveryError` joined with `ErrProducerFatal`, and skips
commit and abort on that closed client. The producer cannot be reused. Kafka
cannot expose the record to `read_committed` without a commit; constructing a
replacement with the same transactional ID fences and recovers the open
transaction. A blind application resubmission before that recovery is unsafe.

Commit and abort completion use a bounded context derived without caller
cancellation because canceling `EndTransaction` can make the broker outcome
unknowable. Kafka lifecycle failures return a redacted `TransactionError`.
`Operation` identifies begin, commit, or abort; `Category` distinguishes
authorization, fencing, fatal producer state, retryable abort-required failure,
and ambiguous outcome. `Abortable` means Kafka rejected the commit and the
package attempted the required bounded abort. `OutcomeKnown` is false when the
broker result cannot be established. An unknown commit also preserves
`ErrTransactionOutcomeUnknown`; callers must stop the affected workflow and
reconcile before retrying. `errors.Is` and `errors.As` preserve causes for
deliberate programmatic inspection while `Error` never renders them.

The real-broker fixture proves a committed record is visible at both Kafka
isolation levels and an aborted record is filtered from a `read_committed`
consumer while remaining present for `read_uncommitted` inspection. This is
producer-transaction evidence against the pinned single-node Confluent Local
fixture.

These producer transactions do not replace a transactional database outbox.

## Consume-transform-produce

`TransactionProcessor` owns one franz-go group transaction session. It forces
`read_committed` source isolation, disables automatic commits, and uses one
transactional producer identity for one live group member. Configuration is
separated into `Connection`, `Group`, `Output`, record `Limits`, and a
`ShutdownTimeout`. Source and output topic allowlists must be non-empty,
bounded, copied, and disjoint to prevent an accidental self-feeding loop.
Its protocol floor defaults to Kafka 2.5 and cannot be lowered because
[`KIP-447`](https://cwiki.apache.org/confluence/spaces/KAFKA/pages/103093950/KIP-447%2BProducer%2Bscalability%2Bfor%2Bexactly%2Bonce%2Bsemantics)
stable transactional offset fetching removes the older rebalance race;
this is a protocol safety floor, not a tested broker support claim.

```go
processor, err := kafka.NewTransactionProcessor(
    kafka.TransactionProcessorConfig{
        Connection: kafka.TransactionConnectionConfig{
            Brokers:  brokers,
            ClientID: "billing-projection",
        },
        Group: kafka.TransactionGroupConfig{
            GroupID:     "billing-projection-v1",
            Topics:      []string{"billing.payment.v1"},
            ResetOffset: kafka.OffsetEarliest,
        },
        Output: kafka.TransactionOutputConfig{
            AllowedTopics:  []string{"billing.balance.v1"},
            TransactionalID: instanceTransactionalID,
        },
    },
)
if err != nil {
    return err
}
defer processor.Close()

return processor.Run(ctx, kafka.TransactionHandlerFunc(func(
    ctx context.Context,
    source kafka.ConsumedRecord,
    transaction kafka.Transaction,
) error {
    return transaction.Publish(ctx, kafka.ProducerRecord{
        Topic: "billing.balance.v1",
        Key:   source.Key,
        Value: transform(source.Value),
    })
}))
```

One poll is one all-or-nothing transaction because the group transaction
settles the complete position returned by that poll. `MaxPollRecords` bounds
the input unit. `MaxOutputRecords` and `MaxOutputBytes` separately bound all
transactional output attempts. The processor invokes handlers sequentially and
commits only after all fetched records and all synchronous output deliveries
succeed. A record validation failure, denied topic, missing required key,
delivery failure, handler error, panic, processing timeout, or cancellation
aborts all outputs and leaves all source offsets for redelivery. The package
detects a delivery
failure even if a handler ignores the error returned by `Transaction.Publish`.
The source group also applies a hard encoded broker-response cap, a decoded
record-batch cap, and an active decoded-buffer budget. A decompression failure
occurs before transaction begin or handler admission and therefore produces no
Kafka output and settles no source offset.
`Group.FetchMinBytes` defaults to one byte and can request larger encoded fetch
responses through `Group.FetchMaxBytes`; `Group.FetchMaxWait` bounds the added
broker-side batching delay.
Reclaimable active-buffer accounting requires Kafka record batches (magic 2);
legacy compressed message sets are unsupported as documented in the
[compatibility matrix](compatibility.md).
An ambiguous output deadline is terminal rather than an ordinary handler
failure: the processor closes its combined client, returns
`ErrTransactionProcessorFatal`, leaves every source offset unsettled, and
rejects later runs before polling. The application must replace the processor;
normal in-process abort is not attempted on the closed client.
Producer record bytes are copied before franz-go retains them. Concurrent
publishes through one callback capability are safe, and transaction completion
waits for calls admitted before the callback returns. Their admission order is
scheduler-dependent; a handler that requires output order must make
synchronous calls in the required order. Ordering remains per output
topic-partition, never global.

## Lifecycle observation

`ProducerConfig.Observers` reports begin and commit on a successful producer
transaction, or begin and abort when callback failure or panic requires
cleanup. `TransactionProcessorConfig.Observers` reports the same phases for
each non-empty source poll and also enables payload-free shutdown, broker, and
group-management-error events for its combined client. Producer shutdown
attempts use the producer observer policy.

Lifecycle events contain copied client identity and local phase duration.
Processor events additionally contain the copied source group identity. They
do not contain transactional IDs, source coordinates, output counts, keys,
values, headers, broker endpoints, or application error text. A known
abort-required commit failure is retryable; an unknown commit or abort outcome
is ambiguous. Observer error, panic, or cooperative timeout is reported only
through the observer failure handler and never changes the transaction result.
An admitted shutdown reports after waiting, group leave where applicable,
flush, and close. Incomplete and later successful retry attempts remain
separate. Observers cannot re-enter the invoking producer or transaction
processor.

Caller cancellation requests a clean stop but does not cancel transaction
cleanup. `Run` returns nil only after the active transaction aborts
successfully; an abort timeout or failure is returned and fences the processor.

A group rebalance or lost assignment before completion makes franz-go abort
rather than risk committing work owned by another member. The processor
returns `ErrTransactionNotCommitted`; a later poll may safely repeat the work.
Any begin, commit, or abort lifecycle error fences further processing.
Definitive authorization, fencing, and fatal errors are classified; ambiguous
end results preserve `ErrTransactionOutcomeUnknown`. Close and replace the
processor after reconciliation instead of blindly continuing.

The single-broker fixture proves that a successful poll advances the
source-group offset with read-committed outputs and does not pass an aborted
transactional source record to the handler. An aborted processing poll leaves
the source offset unchanged, hides its output at read-committed isolation, and
redelivers the source record. The pinned three-broker Apache fixture proves the
same commit, abort, redelivery, and retry boundary while one broker process is
unavailable and the replicated source, output, offsets, and transaction-state
topics remain at ISR two. The fixture also terminates a real child process
after its transactional output is acknowledged but before commit. A replacement
with the same transactional ID reprocesses the unsettled source record, commits
one replacement output and the source offset, and leaves the interrupted output
visible only at read-uncommitted isolation. A second scenario keeps that child
transaction open after output acknowledgement while another operating-system
process joins the eager group. The child observes `ErrTransactionNotCommitted`,
its source remains unsettled, its output remains read-committed invisible, and
a later stable member reprocesses and commits the source. A cooperative
two-process scenario begins with two source partitions owned by the child,
then adds another live member. Incremental revocation aborts the child's open
transaction while the other member atomically commits its assigned partition;
after both leave, a recovery member commits the remaining unsettled source.
Read-committed consumers observe only the reassigned and recovered outputs,
while read-uncommitted inspection also sees the aborted child output.
A separate Apache Kafka 4.3.1 scenario holds a broker-acknowledged transaction
open beyond its configured one-second transaction timeout and waits on Kafka's
transaction-state API rather than sleeping. The coordinator reports
`CompleteAbort`; read-committed consumers do not observe the expired record,
while read-uncommitted consumers do. The producer's later commit receives
`INVALID_TXN_STATE`, which the package correctly reports with
`ErrTransactionOutcomeUnknown` because that response alone cannot establish
the broker's final outcome.

The same three-broker fixture forwards a real `EndTxn` commit to Kafka, reads
and drops every matching broker response, and then closes that connection.
Kafka makes the record visible to a separate read-committed consumer while
`RunTransaction` returns a commit-phase `ErrorAmbiguous` `TransactionError`
that unwraps to `ErrTransactionOutcomeUnknown`, is not abortable, and reports
that the outcome is unknown. This proves the loss window where Kafka committed
but the producer cannot independently know that result. Older-broker behavior
is not yet support evidence.

The fixture separately forwards transactional `Produce` requests while
dropping every matching response. Both standalone production and
consume-transform-produce return within the delivery bound, classify the
record outcome as ambiguous, close and fatally fence the client, and reject
reuse. Separate consumers prove the output exists only at `read_uncommitted`;
the processor's source offset remains `-1`, so no Kafka read-process-write
commit occurred.

Kafka exactly-once language is limited to this Kafka read-process-write
boundary. A handler that performs a database write, HTTP request, object-store
operation, email, webhook, or any other external effect creates a separate
failure window that Kafka cannot make atomic.
