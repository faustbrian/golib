# Outbox Kafka adapter

`gokafka` is the canonical synchronous adapter from `outbox.Envelope` to the
first-party `kafka.Producer`. It maps one persisted envelope to one Kafka
record and returns only after the producer reports the broker delivery result.
It owns no worker, retry loop, transaction, topic, or producer lifecycle.

## Quick start

```go
producer, err := kafka.NewProducer(kafka.ProducerConfig{
	Brokers:       brokers,
	ClientID:      "billing-outbox",
	AllowedTopics: []string{"billing.events.v1"},
	Limits:        gokafka.DefaultLimits().Kafka,
	Security:      kafka.DevelopmentPlaintextSecurity(), // development only
})
if err != nil {
	return err
}
defer producer.Close()

publisher, err := gokafka.New(producer)
if err != nil {
	return err
}
relayConfig.ClassifyError = gokafka.ClassifyError
relay, err := outboxrelay.New(store, publisher, relayConfig)
```

Production callers must configure TLS or SASL policy appropriate for their
cluster instead of development plaintext security.

## Record mapping

| Envelope field | Kafka record field |
| --- | --- |
| `Topic` | topic |
| `Payload` | value, defensively copied |
| `OrderingKey` | key when non-empty |
| `IdempotencyKey` | key fallback and optional `idempotency-key` header |
| `ID` | final key fallback and `event-id` header |
| `PayloadVersion` | decimal `schema-version` header |
| `Metadata["es.content_type"]` | `content-type`, defaulting to `application/json` |
| all metadata | headers sorted lexicographically by key |

Fixed headers appear first in this order: `content-type`, `event-id`,
`schema-version`, then optional `idempotency-key`. Sorted metadata follows.
Metadata cannot replace those reserved headers. The original
`es.content_type` metadata header remains present, so event-sourcing identity
and the normalized content type are both available.

The adapter copies payload, key, and every header value before it calls the
client. It validates both persisted-envelope limits and Kafka record limits
before publication. Configure the producer with the same
`gokafka.Limits.Kafka` value:

```go
limits := gokafka.DefaultLimits()
limits.Envelope.MaxPayloadBytes = 512 << 10
limits.Kafka.MaxValueBytes = 512 << 10

publisher, err := gokafka.New(producer, gokafka.WithLimits(limits))
```

Broker `message.max.bytes`, topic `max.message.bytes`, and producer batch
settings can be stricter than these local bounds and remain operational
prerequisites.

## Ordering

The key precedence is `OrderingKey`, then `IdempotencyKey`, then envelope
`ID`. Sequential acknowledgements for one key and topic preserve Kafka
partition order for that logical stream. Kafka does not provide a global order
across topics or partitions. Concurrent callers must coordinate their own
logical-stream submission order; the adapter adds no lock or scheduler.

## Delivery and duplicates

`Publish` is synchronous because its `Client` contract returns the delivery
result. With `*kafka.Producer`, success means the configured Kafka
acknowledgement policy accepted the record. It does not mean an application
consumer completed a side effect.

Kafka acknowledgement and `outbox.Store.MarkDelivered` are separate durable
operations. If the process stops after Kafka acknowledges the record but before
the outbox transition commits, the relay publishes the same envelope again.
Consumers must deduplicate durable side effects using the stable `event-id` or
an application event identity. Producer idempotence only protects supported
producer-session retries; it does not make the database transition and Kafka
publication atomic.

The adapter adds no nested retry. Configure bounded Kafka record retries,
backoff, request timeout, and delivery timeout on `kafka.ProducerConfig`, and
configure durable retry/backoff on the outbox relay. Do not let both layers
perform unbounded retries.

Set `relay.Config.ClassifyError` to `gokafka.ClassifyError`. Locally rejected
malformed envelopes and authorization, fencing, oversized-record, permanent,
and producer-fatal categories are sent directly to the relay's dead-letter
transition. Retryable, timeout, cancellation, shutdown, unknown, and ambiguous
categories remain transient.
Ambiguous outcomes are retried because Kafka may already contain the record;
consumers must deduplicate the stable event identity.

## Failure and ambiguity recovery

Producer errors are wrapped with `Unwrap`, so `errors.Is` and `errors.As` retain
the first-party `kafka.DeliveryError` and its permanent, retryable, and
ambiguous categories. Cancellation and timeout after admission can mean the
broker outcome is unknown. Treat an ambiguous result like a possible success:
retain or retry the outbox envelope and rely on consumer deduplication. Do not
mark it delivered from local inference alone.

Adapter error strings are fixed, low-cardinality diagnostics. They do not
render wrapped client errors because alternate clients may include envelope
data, metadata, Kafka endpoints, or credentials in their error text.
`errors.Is` and `errors.As` still expose the wrapped cause for deliberate
programmatic handling. Health-check failures follow the same rule.

A panic from an alternate `Client` is contained and returned as
`ErrPublishPanic` with `kafka.ErrorAmbiguous`, because the adapter cannot know
whether that client sent the record before panicking. Error text excludes the
envelope ID, payload, metadata, broker endpoint, credentials, and panic value.

### Relay crash and reconciliation matrix

| Interruption point | Durable state | Recovery |
| --- | --- | --- |
| before producer admission | no Kafka record; envelope remains claimed until release or lease expiry | retry the envelope |
| after admission, before acknowledgement | Kafka outcome is unknown | retry after lease recovery and deduplicate by stable event identity |
| after acknowledgement, before `MarkDelivered` | Kafka contains the record; outbox is not delivered | retry creates an observable duplicate; consumers deduplicate |
| while `MarkDelivered` commits | database outcome is unknown independently of Kafka | reconcile the outbox row; retry only when it is not durably delivered |
| after `MarkDelivered` commits | Kafka contains the record; outbox is delivered | no relay retry is required |

The relay must never infer an outbox transition from Kafka state alone. When a
mark outcome is ambiguous, reconciliation reads the durable outbox row. A row
that is still eligible may be published again, which is safe only when
consumers make side effects idempotent by `event-id` or the application event
identity.

## API and ownership

- `New(Client, ...Option)` validates dependencies and limits without dialing
  Kafka.
- `Publish(context.Context, outbox.Envelope)` maps, validates, owns, and
  synchronously publishes one record.
- `ClassifyError(error)` maps Kafka delivery categories to relay retry or
  permanent-failure policy without inspecting diagnostic text.
- `Health(context.Context)` forwards the producer health probe; it does not
  publish or prove end-to-end delivery.
- `DefaultLimits`, `Limits`, and `WithLimits` bind outbox and Kafka bounds.

`Publisher` is immutable after construction and safe for concurrent use when
the supplied `Client` is safe. The first-party producer is the intended client.
The caller owns the producer, context deadlines, relay lifecycle, and shutdown.

## Adoption and compatibility

Use this adapter when an application already persists `outbox.Envelope` values
and wants the first-party Kafka producer boundary. Keep topic creation in
deployment automation, keep the database transaction in the outbox writer,
and keep relay claiming/marking in `outbox/relay`.

The module is pre-v1 and independently versioned. The initial hardened API is
source-compatible with the earlier `New(client)` call because configuration is
optional. Existing callers gain validation, defensive ownership, redacted
publish diagnostics, and panic containment. Metadata using a fixed header name
is now rejected instead of producing an ambiguous duplicate header; rename
such metadata before adoption.

The adapter targets the current first-party `outbox` and `kafka` modules and
franz-go-backed synchronous producer semantics. It intentionally does not
offer generic Kafka clients, transactions, asynchronous callbacks, topic
administration, consumers, or exactly-once processing.

## Security and tradeoffs

Topic, key, payload, metadata count, individual headers, and aggregate header
bytes are bounded before producer admission. Payloads and identities are not
included in adapter errors. Applications must still treat Kafka headers as
untrusted input at consumers, configure authenticated encryption, restrict the
producer topic allowlist, and avoid putting credentials or secrets in envelope
metadata.

Synchronous publication provides a simple acknowledgement boundary and natural
relay backpressure, at the cost of one waiting relay operation per record.
Batching and buffering remain producer concerns; application-level batch
publication is deliberately outside this adapter so partial outcomes cannot be
hidden.

## FAQ

**Does a nil result mean exactly once?** No. It means Kafka acknowledged this
publication. A crash before the outbox mark can create a duplicate.

**Which identity should a consumer deduplicate?** Prefer the stable application
event identity when one exists; otherwise use the `event-id` envelope identity.

**Can the adapter create topics or start workers?** No. Deployment automation
creates topics and `outbox/relay` owns worker and retry lifecycle.

**Should `IdempotencyKey` replace `OrderingKey`?** No. Ordering wins because all
events in one logical stream need the same partition key. Idempotency is the
fallback when no stream identity is supplied.

**How should an ambiguous timeout be recovered?** Leave the envelope eligible
for durable retry, investigate producer health, and deduplicate any repeated
consumer side effect by stable identity.

## Development

`make check` runs formatting, vet, unit tests, race, exact statement coverage,
fuzzing, benchmarks, and documentation. `make integration` uses a digest-pinned
Confluent Local 7.5.0 container to verify real-broker acknowledgement,
key-partition order, deterministic headers, broker-restart recovery, redacted
ambiguous failure diagnostics, and duplicate recovery evidence. The first-party
producer uses idempotent production with all in-sync replicas required; the
integration test exercises those same durability semantics. Its explicit
duplicate remains observable because producer idempotence cannot join the
separate outbox database transition.
Repository gates additionally enforce security, API compatibility, lint,
static analysis, SBOM/licenses, and exactly 100% viable mutant kills.

`BenchmarkPublisherMappingOnly` uses an in-memory client and therefore measures
only envelope validation, deterministic mapping, copying, and client-call
overhead. Broker latency, producer batching, compression, and network
backpressure require first-party Kafka producer benchmarks and are not mixed
into the mapping number.

See [CHANGELOG.md](CHANGELOG.md) for release notes and [LICENSE](LICENSE) for
the MIT license.
