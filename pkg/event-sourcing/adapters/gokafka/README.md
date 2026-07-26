# Event sourcing Kafka adapter

`gokafka` is the dedicated Kafka boundary for event-sourcing messages. It uses
the repository's franz-go-backed `kafka` module so acknowledgements, producer
idempotence, retries, consumer groups, cooperative rebalancing, offset
settlement, TLS, SASL, and broker operations remain observable Kafka
semantics.

The stable record codec, synchronous direct dispatcher, consumer-group record
handler, explicit poison/retry policy, and first-party synchronous dead-letter
publisher are implemented. Real-broker compatibility covers the complete live
and dead-letter delivery paths.

## Record mapping

```go
codec, err := gokafka.NewRecordCodec(gokafka.RecordCodecConfig{
	Resolver:      gokafka.FixedTopic("accounts.events.v1"),
	AllowedTopics: []string{"accounts.events.v1"},
})
if err != nil {
	return err
}

record, err := codec.Encode(delivery)
if err != nil {
	return err
}
err = producer.Publish(ctx, record)
```

The topic resolver output must match one constructor-validated allowlist entry.
The aggregate-root ID is the default Kafka key, preserving per-aggregate order
within one topic. The event payload is the record value.

Ordered `es.*` headers carry:

- message ID;
- aggregate type and ID;
- stream version;
- event name and schema version;
- content type and recorded time;
- optional correlation, causation, tenant, partition, and global position;
- canonical JSON application metadata; and
- explicit `live` or `replay` delivery mode.

Decode rejects duplicate or unknown `es.*` headers, missing identities,
noncanonical numbers, timestamps, or metadata, mismatched aggregate keys,
disallowed topics, and values outside the configured bounds. Non-reserved
headers are ignored so an optional telemetry adapter can propagate trace
context without changing the event wire identity.

## Ownership and limits

`RecordCodec` is immutable and safe for concurrent use when its application
resolver is. Encode and decode defensively own payload, key, header, and
metadata bytes. Resolver errors and panics retain stable `errors.Is` categories
without exposing their diagnostic text.

`DefaultRecordLimits` reserves space below a typical one-megabyte Kafka record
limit: an event payload can be at most 900 KiB after encoding and total headers
at most 72 KiB. Applications may supply stricter valid `kafka.MessageLimits`,
but the same limits must configure the producer and inbound record boundary.
Broker and topic limits remain operational prerequisites.

## Direct synchronous dispatch

`Dispatcher` implements the core `eventsourcing.Dispatcher` contract over any
synchronous `Publisher`, including `*kafka.Producer`:

```go
dispatcher, err := gokafka.NewDispatcher(producer, codec)
if err != nil {
	return err
}
err = dispatcher.Dispatch(ctx, deliveries)
```

Dispatch is sequential and returns only after each producer acknowledgement.
The default stops at the first failure. `ContinueOnPublishError` attempts later
records and returns a redacted `DispatchError` with exact published, failed,
attempted, and total counts. Encoding failure, replay denial, and cancellation
always stop the batch. Publisher panics are contained.

`Published` counts records acknowledged by Kafka. A failed acknowledgement can
still have an ambiguous broker outcome, so retrying can duplicate a record.
Continuation can also create an application-visible gap when a later record
for the same aggregate is acknowledged after an earlier failure. Use
`ContinueOnPublishError` only for independently orderable records and
idempotent consumers.

Replay publication is denied by default. `AllowReplay` is an explicit opt-in
and does not bypass the `replay` wire marker. Empty batches succeed, duplicate
deliveries are published in order, and no locks are held across publisher
calls, so reentrant dispatch is permitted when the resolver and publisher
support it.

Use the repository `kafka.ProducerConfig` to select bounded buffering, batch
size, retries, delivery and request timeouts, linger, TLS, and SASL. The
first-party producer requests all in-sync replica acknowledgements and retains
franz-go idempotent production. `CompressionPreferences` selects an ordered,
validated compression fallback policy; the default is Snappy followed by
uncompressed records. These settings do not create cross-system atomicity.

## Consumer groups and offset settlement

`RecordHandler` implements `kafka.Handler` and composes directly with the
first-party franz-go-backed group consumer:

```go
group, err := kafka.NewConsumer(kafka.ConsumerConfig{
	Brokers:        brokers,
	ClientID:       "account-projection",
	GroupID:        "account-projection-v1",
	Topics:         []string{"accounts.events.v1"},
	ResetOffset:    kafka.OffsetEarliest,
	MaxPollRecords: 100,
})
if err != nil {
	return err
}
defer group.Close()

handler, err := gokafka.NewRecordHandler(codec, projectionConsumer)
if err != nil {
	return err
}
return group.Run(ctx, handler)
```

The group consumer disables automatic commits, bounds each poll, blocks a
cooperative rebalance while the batch is handled, commits offsets only after
every handler call succeeds, and releases the rebalance after each poll.
`RecordHandler` returns an error for corrupt records, replay without explicit
`AllowReplayHandling`, consumer errors or panics, and cancellation. Those
errors leave the batch unsettled for at-least-once retry.

`HandlerError` exposes topic, partition, and offset while redacting record
data, consumer diagnostics, and panic values. The adapter does not deduplicate
records: consumers must make durable side effects idempotent. The caller owns
the `kafka.Consumer`, its context, shutdown, rebalance timeout, handler
deadline, and commit deadline.

No poison record is skipped automatically. Without `WithFailurePolicy`, decode
or consumer failure returns `HandlerError` and leaves the offset unsettled for
retry. A configured policy receives the borrowed Kafka record and original
cause synchronously:

```go
policy, err := gokafka.NewDeadLetterPolicy(
	producer,
	gokafka.DeadLetterPolicyConfig{
		Topic:  "accounts.events.dead-letter.v1",
		Limits: gokafka.DefaultRecordLimits(),
	},
)
if err != nil {
	return err
}
handler, err := gokafka.NewRecordHandler(
	codec,
	projectionConsumer,
	gokafka.WithFailurePolicy(policy),
)
```

`FailureHandled` permits settlement only after the policy returns successfully
under an active context. The first-party policy synchronously publishes an
owned copy of the original key, value, and headers to one validated fixed
topic. It appends ordered `esdlq.source_topic`,
`esdlq.source_partition`, `esdlq.source_offset`, and
`esdlq.source_time` headers without serializing the failure cause. A source
record already at the destination or carrying any `esdlq.*` source header is
rejected to prevent a dead-letter loop.

`FailureRetry`, publication failure or panic, an invalid disposition, and
cancellation all fail closed. Replay denial and invalid Kafka positions bypass
the policy and cannot be converted into settlement. `DeadLetterError` exposes
only the source position and stable error categories; its diagnostic string
does not include record, application, broker, credential, or panic data.

Zero `DeadLetterPolicyConfig.Limits` use `DefaultRecordLimits`. The policy
validates the borrowed source record and space for all four source headers
before copying bytes. Configure the same limits on the codec, dead-letter
policy, and producer; a source record that cannot fit the bounded destination
remains unsettled.

To reconstruct dead-lettered events with `RecordCodec`, include the fixed
dead-letter topic in `AllowedTopics`. The resolver may still route new live
events only to the primary topic.

Dead-letter publication and the source consumer-group offset commit are not
atomic. A crash after the policy succeeds, or a later record failing in the
same polled batch, can repeat both handling and dead-letter publication.
Dead-letter identities and storage must therefore be idempotent.

Record key, value, and header bytes passed to a custom policy are borrowed for
that call. A custom policy must copy them before retention. The cause may
contain application diagnostics; inspect it with `errors.Is` or `errors.As`
and never serialize or log its text without application-owned redaction. The
adapter does not count attempts, apply backoff, create dead-letter topics, or
start retry workers; those operational decisions stay explicit and durable
outside this handler.

## Delivery guarantees

The codec does not publish, retry, or commit an offset. The dispatcher starts
no goroutines and delegates broker policy to the supplied synchronous
publisher. Direct publication remains non-atomic with PostgreSQL. Durable
production publication should use the PostgreSQL event/outbox transaction and
outbox-owned Kafka publisher.

Replay mode survives the wire round trip. Applications must keep replay
records away from process managers and external side effects unless a
separately authorized replay operation opts in.

## Development

Run `make check` from this module. With Docker available, `make integration`
verifies synchronous Zstandard dispatch, complete envelope reconstruction,
per-aggregate order, consumer handling, dead-letter publication and recovery,
and committed offsets against the digest-pinned Confluent Local 7.5.0 fixture
using franz-go v1.21.5.
