# Consumer retry and dead-letter policy

Kafka stores ordered partition logs and consumer-group positions. It does not
provide queue-style nack, per-record visibility timeouts, or an invisible
in-flight state. A failed source record remains eligible for redelivery until
the group commits an offset after it. This package therefore keeps failure
actions explicit and preserves partition settlement rules described by
[Kafka's consumer position and delivery model](https://kafka.apache.org/43/design/design/).

## Available strategies

`NewFailureHandler` decorates a per-record `Handler`.
`NewBatchFailureHandler` decorates the whole-partition `BatchHandler` contract.
Both use `FailureModeStop` as the zero terminal mode.

Both decorators validate source Kafka coordinates, timestamp metadata, and
complete record material before retaining bytes or invoking application
callbacks. Invalid source input fails closed with `ErrFailureRecordInvalid`
(and `ErrInvalidFailureBatch` for the batch boundary); a specific record-limit
identity remains available through `errors.Is` when applicable.

| Strategy | Package behavior | Source settlement |
| --- | --- | --- |
| stop | Return a redacted `FailureHandlingError`. | No settlement for the failed record. Kafka may redeliver it. |
| bounded in-process retry | Retry only configured `ErrorCategory` values, at most `MaxAttempts`, with capped exponential backoff. | Settlement occurs only if an attempt succeeds or a terminal action later succeeds. |
| versioned retry topic | Publish an owned source copy plus bounded failure metadata to one explicit `FailureTarget`. | The normal consumer commit occurs after a definite publish success. |
| versioned dead-letter topic | Publish the same owned failure record after the application selects a terminal decision. | The normal consumer commit occurs after a definite publish success. |
| application delegate | Invoke one synchronous `FailureDelegate`. | A nil delegate result explicitly resolves the record; an error leaves it unsettled. |

For a batch decorator, every row applies to the complete source partition
batch. Retry repeats the complete handler call. Publishing reroutes every
record through one bounded call with input-ordered results; target partitioning
can make that call multiple non-atomic Kafka requests. Delegation receives one
`BatchFailure`; it cannot settle a guessed prefix.

Retries, publishing, and delegates run inside the consumer's existing handler
context. Rebalance cancellation or any other context cancellation stops the
failure policy before a terminal action. Backoff is timer-bounded and
cancellation-aware. `MaxAttempts` includes the initial call, is capped at 32,
and defaults to one. Enabling retries requires a 1 millisecond or greater
initial delay, a maximum delay no smaller than the initial delay, and a maximum
of 5 minutes. Empty retry categories default to `ErrorRetryable`; no other
category is retried implicitly.

An optional `FailureClassifier` maps application errors to the package's stable
low-cardinality categories. Without one, package, context, franz-go, and Kafka
errors use the normal package classifier and an arbitrary application error is
permanent. Classifier, publisher, and delegate panics are contained. Their
error text is not rendered by `FailureHandlingError`; `errors.Is` and
`errors.As` preserve deliberate programmatic inspection.

## Retry-topic example

```go
failureHandler, err := kafka.NewFailureHandler(kafka.FailureHandlerConfig{
    Handler: kafka.HandlerFunc(processRecord),
    Classifier: kafka.FailureClassifierFunc(func(err error) kafka.ErrorCategory {
        if errors.Is(err, errDependencyUnavailable) {
            return kafka.ErrorRetryable
        }
        return kafka.ErrorPermanent
    }),
    Retry: kafka.FailureRetryPolicy{
        MaxAttempts:    3,
        InitialBackoff: 100 * time.Millisecond,
        MaxBackoff:     time.Second,
    },
    Mode: kafka.FailureModeRetryTopic,
    Target: kafka.FailureTarget{
        Topic:   "track.tracking-event.retry.v2",
        Version: 2,
    },
    Publisher:      producer,
    PublishTimeout: 10 * time.Second,
})
if err != nil {
    return err
}

return consumer.Run(ctx, failureHandler)
```

The producer must independently allow the target topic. The source and target
topics must differ. A target version is mandatory; topic creation, retention,
partition count, ACLs, and the routing from one retry generation to the next
remain application and infrastructure responsibilities.
Target records use automatic producer partition selection with the preserved
source key. They do not force the source partition number onto a topic whose
partition topology may differ.

## Failure record

The published record keeps:

- the original key, value, timestamp, and ordered headers;
- source topic, partition, offset, timestamp type, and leader epoch;
- the local handler attempt and stable error category;
- retry or dead-letter kind; and
- failure metadata schema version 1 plus the configured target version.

Package metadata uses the `golib.kafka.failure.` header prefix and is appended
after original headers. The package never adds handler error text. Original
headers are preserved because they may carry reviewed correlation metadata;
they can also contain sensitive application data, so retry and dead-letter
topics require matching encryption, ACL, retention, telemetry, and operator
access policy.

`attempt` is the handler attempt within the current consumer invocation. If a
retry record is routed again, its prior ordered metadata headers remain intact
and the next complete schema block is appended. This preserves the hop and
attempt history without trusting an unverified caller-supplied counter.

Every source record and added header counts against
`FailureHandlerConfig.Limits`. If all
original data and metadata cannot fit, publication fails closed with
`ErrFailureRecordInvalid` and the source record remains unsettled. Applications
must reserve header-count and byte headroom for the 11 metadata headers. Record
bytes supplied to the publisher are owned copies. The decorator validates the
source before retaining it or calling the wrapped handler, then gives each
in-process attempt an isolated copy; mutation by one attempt cannot change
later attempts or the published failure record. `HandlerFailure` remains
borrowed during delegate calls; call `Retain` before storing it.

Batch publication appends two more headers to every record:
`golib.kafka.failure.batch-index` is the zero-based position in the source
partition batch and `golib.kafka.failure.batch-count` is its complete size.
`BatchFailureHandlerConfig.MaxBatchRecords` and `MaxBatchBytes` bound both the
retained source and encoded target batches. Each target record must still fit
`Limits`. `BatchFailure` remains borrowed during delegate calls; `Retain`
deeply copies the complete batch.

## Non-transactional failure window

Retry-topic and dead-letter publication through `FailureHandler` is
non-transactional:

1. the target producer receives a definite Kafka acknowledgement;
2. the handler returns success;
3. the consumer submits the source offset commit.

If the process crashes or the commit is rejected or ambiguous after step 1,
Kafka can redeliver the source and another target record can be published.
This is an at-least-once target publication window: duplicates are possible,
while a definite target publish failure does not advance the source offset.
Target consumers must deduplicate with the preserved source coordinates or an
application identifier.

Whole-batch publication has an additional partial-delivery window. The
publisher returns one input-ordered `DeliveryResult` per source record. The
decorator resolves the source batch only when all results are definite
successes. A partial or ambiguous target result leaves every source offset
unsettled and is available through
`FailureHandlingError.DeliveryResults`. Redelivery can republish target records
that previously succeeded; target consumers must deduplicate by source topic,
partition, and offset.

If target publication and source settlement must be one Kafka effect, use
`TransactionProcessor` with a `read_committed` target consumer. That guarantee
is Kafka-only. Neither path is atomic with databases, HTTP calls, email, object
storage, or other external systems.

## Failure windows by strategy

- **Stop:** no source commit is submitted for the failed record. A handler side
  effect completed before its error can run again after the next poll,
  rebalance, restart, or process recovery.
- **In-process retry:** every attempt can repeat a partial application side
  effect from an earlier failed attempt. Exhaustion continues into the selected
  terminal mode; it never commits by itself.
- **Retry topic:** a definite target acknowledgement precedes source commit.
  Failure before acknowledgement leaves the source unsettled; failure after
  acknowledgement but before a definite source commit can duplicate the retry
  record.
- **Dead letter:** it has the same two-effect window as a retry topic. Calling
  it terminal does not make publication and source settlement atomic.
- **Application delegate:** a nil result tells the consumer to settle after
  the callback. Any external effect performed by the delegate has the usual
  effect-then-commit duplicate window. If the delegate commits an external
  checkpoint first or performs irreversible work in another order, its loss
  window is entirely application-defined.
- **Kafka transaction:** `TransactionProcessor` removes the Kafka
  target/source-offset gap only for its bounded Kafka read-process-write
  transaction. Handler effects outside Kafka remain outside that transaction.

## Poison records and ordering

A stop or failed terminal action blocks later settlement in the same source
partition. Independent partitions may continue and commit their own contiguous
success. Publishing a record to a retry or dead-letter topic resolves that
source position, so later source records may advance. Retry topics establish a
new Kafka log and do not preserve ordering relative to later records remaining
on the source topic.

`FailureHandler` remains per-record. `NewBatchFailureHandler` is the explicit
whole-batch alternative for `RunBatchOnce`; it retries or reroutes every record
and never infers which record inside a failed application batch succeeded.
