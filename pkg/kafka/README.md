# kafka

`kafka` is the bounded first-party Apache Kafka client policy for Go services.
It wraps franz-go with safe producer, consumer, transaction, replay, TLS, SASL,
health, topic inspection, and consumer-lag behavior.

The module provides at-least-once building blocks. It does not make a database
write and Kafka publication atomic, does not make consumer side effects exactly
once, and does not own topic creation or broker configuration.

## Requirements

- Go 1.26.5 or later
- Apache Kafka compatible with franz-go v1.21.5
- TLS 1.2 or later when TLS is configured
- durable, idempotent consumer side effects

## Producer

```go
producer, err := kafka.NewProducer(kafka.ProducerConfig{
    Brokers:  []string{"kafka.internal:9093"},
    ClientID: "track-outbox",
    Security: kafka.ClientSecurity{TLS: tlsConfig},
})
if err != nil {
    return err
}
defer producer.Close()

err = producer.Publish(ctx, kafka.Message{
    Topic: "track.tracking-event.v1",
    Key:   []byte(trackedItemID),
    Value: payload,
})
```

Production uses Kafka idempotence, all in-sync replica acknowledgements,
bounded retries, bounded buffering, and synchronous delivery results.

## Consumer

```go
consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
    Brokers:     brokers,
    ClientID:    "track-delivery-planner",
    GroupID:     "track-outbound-delivery-planner-v1",
    Topics:      []string{"track.tracking-event.v1"},
    ResetOffset: kafka.OffsetEarliest,
})
if err != nil {
    return err
}
defer consumer.Close()

return consumer.Run(ctx, kafka.HandlerFunc(func(
    ctx context.Context,
    message kafka.ConsumedMessage,
) error {
    return durableInbox.Insert(ctx, message.Value)
}))
```

Offsets are committed only after every handler in the bounded poll succeeds.
Handlers must tolerate duplicates. Retain copies if bytes are needed after the
handler returns.

## Replay and inspection

`ReplayReader` reads explicit inclusive-start, exclusive-end partition ranges
without joining or changing a consumer group. It fails closed on offset gaps.
`Inspector` provides bounded read-only topic metadata and consumer-group lag.
Infrastructure remains responsible for topics, replication, ISR, retention,
quotas, ACLs, and destructive administrative operations.

See [the documentation index](docs/README.md), [guarantees](docs/guarantees.md),
[operations](docs/operations.md), and [security](docs/security.md).

## Development

Run `make check`. Security reports follow [SECURITY.md](SECURITY.md). The module
is licensed under the [MIT License](LICENSE).
