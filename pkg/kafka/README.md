# kafka

`kafka` is the pre-v1 bounded first-party Apache Kafka client policy for Go
services. The current implementation is being redesigned; existing draft APIs
and passing checks are not a release-readiness claim.

The governing boundaries are recorded in the
[production policy decisions](docs/design/decision-matrices.md).

The module provides at-least-once building blocks. It does not make a database
write and Kafka publication atomic, does not make consumer side effects exactly
once, and does not own topic creation or broker configuration.

## Requirements

- Go 1.26.5 or later
- an explicitly tested Apache Kafka version; franz-go protocol support alone is
  not a package compatibility claim
- TLS 1.2 or later when TLS is configured
- durable, idempotent consumer side effects

## Producer

Configuration can be validated during application bootstrap without allocating
a client or dialing brokers:

```go
config := kafka.ProducerConfig{
    Brokers:  []string{"kafka.internal:9093"},
    ClientID: "track-outbox",
}
if err := config.Validate(); err != nil {
    return err
}
```

```go
producer, err := kafka.NewProducer(kafka.ProducerConfig{
    Brokers:               []string{"kafka.internal:9093"},
    ClientID:              "track-outbox",
    Security:              kafka.ClientSecurity{TLS: tlsConfig},
    CompressionPreferences: []kafka.CompressionCodec{
        kafka.CompressionZstd,
        kafka.CompressionSnappy,
        kafka.CompressionNone,
    },
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

The producer leaves franz-go idempotence enabled, requests all in-sync replica
acknowledgements, and bounds retries, buffering, batch admission, and delivery
time. `PublishRecord`, `PublishBatch`, and `PublishAsync` return per-record
delivery metadata and redacted, classifiable delivery failures. Keyed records
are required by default; unkeyed production requires `UnkeyedAllowed`.
Compression preferences are ordered and copied during construction. The
default is Snappy with an uncompressed fallback. `CompressionNone` is valid
only as the final preference; use it alone to disable compression. Confirm
broker and consumer compatibility before selecting Zstandard.

Call `Shutdown` with a bounded context for graceful drain and close. A failed
drain fences new production while retaining admitted records so the caller can
retry shutdown or explicitly accept data loss through `Abort`. `Close` remains
the unbounded compatibility operation.

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

See the [current audit](docs/audit.md), [compatibility matrix](docs/compatibility.md),
[documentation index](docs/README.md), [guarantees](docs/guarantees.md),
[operations](docs/operations.md), and [security](docs/security.md).

## Development

Run `make check`. With Docker available, `make integration` exercises the
current draft against one pinned Confluent Local 7.5.0 broker. That fixture is
not the required multi-broker Apache Kafka, auth, failure, or service
compatibility matrix. Exact inputs and evidence gaps are recorded in the
[compatibility matrix](docs/compatibility.md). Security reports follow
[SECURITY.md](SECURITY.md). The module is licensed under the
[MIT License](LICENSE).
