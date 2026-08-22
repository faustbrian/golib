# Outbox RabbitMQ Streams adapter

`gorabbitstream` maps one persisted `outbox.Envelope` to one confirmed
`rabbitstream.Message`. It owns no producer, topology, relay, retry loop,
database transaction, or outbox state transition.

## Quick start

```go
producer, err := rabbitmq.OpenProducer(ctx, connection, rabbitstream.ProducerConfig{
    Stream: "billing.events",
})
if err != nil {
    return err
}

publisher, err := gorabbitstream.New(producer, gorabbitstream.Config{
    Stream: "billing.events",
})
if err != nil {
    return err
}

worker, err := relay.New(store, publisher, relay.Config{
    Owner: "billing-outbox-1",
    ClassifyError: gorabbitstream.ClassifyError,
})
```

The caller creates and closes the producer. Production topology remains
operator-owned.

## Wire mapping

| Envelope field | Stream message field |
| --- | --- |
| `Topic` | configured Stream or Super Stream; it must match exactly |
| `Payload` | opaque copied payload bytes |
| `ID` | AMQP message ID |
| `OrderingKey`, then `IdempotencyKey`, then `ID` | routing key |
| `PayloadVersion` | `schema-version` application property |
| `IdempotencyKey` | optional `idempotency-key` application property |
| `CreatedAt` | creation timestamp |
| `Metadata["es.content_type"]` | content type, default `application/json` |
| `Metadata["correlation-id"]` | correlation ID |
| `traceparent` and `tracestate` metadata | W3C message annotations |
| remaining metadata | sorted application properties |

The adapter copies every mutable value before client admission and enforces the
configured root message limits. `schema-version`, `idempotency-key`, and
`content-type` metadata are reserved because the adapter owns those fields.

## Confirmation, retries, and duplicates

A nil `Publish` error means the first-party client returned
`DeliveryConfirmed`. Memory admission alone is never success. Rejection,
ambiguity, timeout, connection loss, and malformed client results remain
errors.

RabbitMQ confirmation and the database `MarkDelivered` transition are separate
effects. A process failure after confirmation but before the database commit
causes a safe at-least-once retry and may publish a duplicate. Consumers must
durably deduplicate the stable event ID with the business side effect.

The adapter does not derive a RabbitMQ publishing ID from the string event ID.
Broker deduplication requires an application-owned stable producer name and a
durable monotonic publishing-ID sequence; it is optional and never replaces
application idempotency.

`ClassifyError` treats local validation, oversized messages, invalid producer
configuration, and broker rejection as permanent. Cancellation, authorization,
connection loss, timeouts, unconfirmed or ambiguous outcomes, fatal producer
state, and contained client panics remain transient because replacing the
runtime or credentials can make the same durable envelope publishable. Retrying
an ambiguous outcome can duplicate an accepted event.

## Ordering and Super Streams

One adapter targets exactly one Stream or Super Stream. The stable routing key
preserves per-aggregate routing. A Super Stream provides order only within its
selected backing stream; topology changes can change routing and there is no
global partition order. Concurrent callers must preserve their own submission
order for one aggregate.

## Adoption and tradeoffs

Use this adapter for retained event distribution from a transactional outbox.
Use the existing queue adapter for executable jobs, delayed work, or competing
workers. Do not hide both behind one generic messaging API.

The adapter intentionally has no topology administration, replay, consumer,
health, telemetry-provider, transaction, domain-envelope, JSON-schema, or
cross-system exactly-once API. Use the root inspection/replay contracts and
nested RabbitMQ/OpenTelemetry adapters for those responsibilities.

## Security

Returned adapter diagnostics are fixed and omit payload, metadata, routing
keys, endpoints, credentials, and panic values. Wrapped causes remain available
through `errors.Is` and `errors.As`; do not unwrap unknown client errors into
untrusted logs. Treat all event metadata as untrusted input.

## FAQ

### Does confirmation mark the outbox row delivered?

No. The relay performs that separate database transition.

### Does this provide exactly-once delivery?

No. It provides confirmed at-least-once publication with a stable identity for
application deduplication.

### Can one publisher route arbitrary topics?

No. One publisher accepts one exact configured Stream or Super Stream target.

See [CHANGELOG.md](CHANGELOG.md) and [LICENSE](LICENSE).
