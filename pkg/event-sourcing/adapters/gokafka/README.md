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

Wire version 1 uses the following canonical mapping. Reserved headers MUST
appear in this relative order; optional headers are omitted when absent.
Non-reserved transport headers may appear anywhere and do not affect the
reserved-header order.

| Kafka field | Event-sourcing value |
| --- | --- |
| Topic | Allowlisted resolver result |
| Key | Aggregate ID bytes |
| Value | Encoded event payload |
| Timestamp | Producer create time equal to recorded time truncated to milliseconds |
| `es.wire_version` | `1` |
| `es.message_id` | Message ID |
| `es.aggregate_type` | Aggregate type |
| `es.aggregate_id` | Aggregate ID |
| `es.stream_version` | Canonical positive decimal stream version |
| `es.event_name` | Persisted event name |
| `es.event_schema_version` | Canonical positive decimal schema version |
| `es.content_type` | Canonical event media type |
| `es.recorded_at` | Canonical UTC RFC 3339 timestamp at microsecond precision |
| `es.correlation_id` | Optional correlation ID |
| `es.causation_id` | Optional causation ID |
| `es.tenant` | Optional tenant |
| `es.partition` | Optional application partition |
| `es.global_position` | Optional canonical positive decimal global position |
| `es.metadata` | Canonical JSON object with string values |
| `es.delivery_mode` | `live` or `replay` |

Decode accepts only wire version 1 and rejects reordered, duplicate, unknown,
empty, or missing reserved headers; noncanonical numbers, timestamps, or
metadata; a non-create-time Kafka timestamp or one that does not match the
millisecond-truncated recorded time; mismatched aggregate keys; disallowed
topics; and values outside the configured bounds. Non-reserved headers are
ignored so an optional telemetry adapter can propagate trace context without
changing the event wire identity.

Canonical unsigned integers use base 10 with no sign or leading zero and must
be greater than zero. `es.recorded_at` is UTC with `Z`, uses the RFC 3339 date
and time separators and a four-digit year, and omits the fractional part when
it is zero; otherwise it carries one through six fractional digits with
trailing zeros removed. An event time outside that grammar is rejected before
topic resolution or publication.

`es.metadata` is a UTF-8 JSON object with at most 64 entries. Keys are non-empty
ASCII tokens of at most 128 bytes using letters, digits, `.`, `_`, `:`, or `-`;
the `es.` prefix is reserved case-insensitively. Values are valid UTF-8 strings
of at most 4 KiB, may be empty, and contain no Unicode control characters. The
combined unencoded key and value bytes must not exceed 64 KiB.

Keys are ordered lexicographically by their UTF-8 bytes. The JSON encoding has
no insignificant whitespace, uses the short JSON escapes for quote and
backslash, escapes `<`, `>`, `&`, U+2028, and U+2029 as lowercase `\u`
sequences, and otherwise emits valid non-ASCII characters as UTF-8. Empty
metadata is `{}`; `null`, duplicate keys, non-string values, alternate escaping,
and any byte-different representation are noncanonical.

## Compatibility

Wire version 1 is the only supported record format. The mapping is independent
of franz-go and can be produced or consumed by another Kafka client when it
preserves byte values, reserved-header order, create-time timestamps, and all
canonical encodings above. Brokers configured to replace producer create time
with log-append time are incompatible because the decoder verifies the Kafka
timestamp against `es.recorded_at`.

Unknown versions and pre-version records fail closed as `ErrRecordCorrupt` and
remain unsettled. Adding an optional application header outside the `es.*`
namespace is compatible. Changing a reserved field, its order, or its encoding
requires a new wire version and an explicit migration; version 1 will never be
silently reinterpreted.

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
owned copy of the original key, value, headers, and Kafka timestamp to one
validated fixed topic. It appends ordered `esdlq.source_topic`,
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

## API reference

- `RecordCodec`, `RecordCodecConfig`, `TopicResolver`, `TopicResolverFunc`,
  `FixedTopic`, and `DefaultRecordLimits` own canonical conversion and routing.
- `Dispatcher`, `NewDispatcher`, `AllowReplay`, and
  `ContinueOnPublishError` provide synchronous direct publication.
- `RecordHandler`, `NewRecordHandler`, `AllowReplayHandling`, and
  `WithFailurePolicy` provide the consumer boundary.
- `FailurePolicy`, `FailurePolicyFunc`, `FailureRetry`, and `FailureHandled`
  make poison disposition explicit.
- `DeadLetterPolicy`, `DeadLetterPolicyConfig`, and `NewDeadLetterPolicy`
  provide synchronous first-party quarantine.
- `RecordError`, `DispatchError`, `HandlerError`, and `DeadLetterError` expose
  stable categories and bounded progress or source positions without payloads.
- Exported `Header*` constants identify the versioned event and dead-letter
  headers. Callers must not add their own values in the reserved `es.*` or
  `esdlq.*` namespaces.

Use `go doc github.com/faustbrian/golib/pkg/event-sourcing/adapters/gokafka`
for the complete exported signatures and error categories.

## Adoption

1. Allocate versioned primary and dead-letter topics with compatible record,
   retention, timestamp, and replication policy.
2. Configure the same topic allowlist and `kafka.MessageLimits` for the codec,
   producer, handler, and dead-letter policy.
3. Use aggregate IDs as keys and keep all events for one aggregate on one
   topic when per-aggregate ordering is required.
4. Make event consumers and dead-letter storage idempotent by message ID before
   enabling retries or offset settlement.
5. Set bounded producer, handler, commit, rebalance, and shutdown deadlines.
6. Keep replay publication and handling disabled until a separately reviewed
   replay workflow opts in.
7. Exercise the real-broker integration path with the deployment's broker
   timestamp policy and security configuration before rollout.

## Security notes

Topic resolvers, producers, consumers, and failure policies are application
trust boundaries. Keep their deadlines bounded, use TLS and an appropriate
SASL mechanism through the `kafka` module, and authorize both primary and
dead-letter topics with least privilege. Error values intentionally omit
payloads, headers, metadata, callback diagnostics, panic values, and
credentials. Applications must apply the same redaction rule to custom policy
causes and telemetry. Limits are denial-of-service boundaries and must not be
raised beyond broker policy without a resource review.

## FAQ

### Does the adapter provide exactly-once effects?

No. Producer acknowledgement can be ambiguous, handling and offset commit are
separate operations, and dead-letter publication is non-atomic with source
settlement. Durable side effects must be idempotent.

### Are duplicate messages removed?

No. The message ID is preserved so application storage can detect duplicates,
but the adapter never suppresses delivery.

### Is ordering global?

No. The deterministic aggregate key preserves Kafka partition order for one
aggregate within one topic. It does not order different aggregates, topics, or
independent dispatch calls.

### What happens to an invalid or poison record?

It remains unsettled by default. A configured policy may return
`FailureHandled` only after durable synchronous quarantine succeeds; otherwise
the handler fails closed for retry.

### Can replay records use the normal consumer?

Only with explicit `AllowReplayHandling`. Publication separately requires
`AllowReplay`; neither option removes the `replay` wire marker.

## Migrations

The version-1 mapping adds `es.wire_version`, requires canonical reserved-header
order, and binds the Kafka create-time timestamp to `es.recorded_at` at
millisecond precision. Unversioned records are intentionally not accepted.

For an existing unversioned topic, deploy on a new versioned topic: stop old
publication, drain the old consumer group with the old codec, deploy version-1
consumers, then switch producers. If overlap is required, run a separately
owned legacy reader that validates and republishes old records as version 1;
do not loosen the version-1 decoder or rewrite retained records in place.

## Runnable examples

The package examples compile complete direct-dispatch and consumer-group
handling workflows. They show stable event construction, bounded topic policy,
acknowledgement-aware producer cleanup, durable-handler settlement, and group
shutdown. Replace their placeholder endpoint, identities, and persistence seam
before use. See [`example_test.go`](example_test.go).

## Development

From the repository root, run
`./scripts/run-modules.sh check --jobs 1 --modules pkg/event-sourcing/adapters/gokafka`
for the complete CI-equivalent contract. The module-local `make check` is a
quick development loop and is not release evidence. With Docker available,
`make integration` verifies synchronous Zstandard dispatch, complete envelope
reconstruction, per-aggregate order, consumer handling, dead-letter publication
and recovery, replay rejection without settlement, explicit replay opt-in and
recovery, and committed offsets against the digest-pinned Confluent Local 7.5.0
fixture using franz-go v1.21.5.
