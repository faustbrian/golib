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
- verified TLS 1.2 or later by default
- durable, idempotent consumer side effects

## Producer

Configuration can be validated during application bootstrap without allocating
a client or dialing brokers:

```go
config := kafka.ProducerConfig{
    Brokers:       []string{"kafka.internal:9093"},
    ClientID:      "track-outbox",
    AllowedTopics: []string{"track.tracking-event.v1"},
}
if err := config.Validate(); err != nil {
    return err
}
```

```go
producer, err := kafka.NewProducer(kafka.ProducerConfig{
    Brokers:               []string{"kafka.internal:9093"},
    ClientID:              "track-outbox",
    AllowedTopics:         []string{"track.tracking-event.v1"},
    Security:              kafka.ClientSecurity{}, // TLS 1.2+, system roots
    CompressionPreferences: []kafka.CompressionCodec{
        kafka.CompressionZstd,
        kafka.CompressionSnappy,
        kafka.CompressionNone,
    },
})
if err != nil {
    return err
}

publishErr := producer.Publish(ctx, kafka.Message{
    Topic: "track.tracking-event.v1",
    Key:   []byte(trackedItemID),
    Value: payload,
})
return errors.Join(publishErr, producer.Close())
```

The producer leaves franz-go idempotence enabled, requests all in-sync replica
acknowledgements, and bounds retries, buffering, batch admission, and delivery
time. `PublishRecord`, `PublishBatch`, and `PublishAsync` return per-record
delivery metadata and redacted, classifiable delivery failures. Keyed records
are required by default; unkeyed production requires `UnkeyedAllowed`.
Records use automatic keyed partitioning by default. Use
`Partition: kafka.ExplicitPartition(n)` only when the application owns the
exact topic-partition routing contract; the key is still transported but no
longer selects the partition.
All producer, consumer, replay, and inspection topic names must follow Kafka's
broker rules: 1 to 249 ASCII bytes using alphanumerics, `.`, `_`, or `-`, with
`.` and `..` rejected.
`ProducerConfig.AllowedTopics` is a required, constructor-copied allowlist of at
most 64 topics. Records outside it fail before franz-go admission. Services
with dynamic routing must build and review the bounded allowlist before client
construction; unrestricted production is not a zero-value fallback.
Compression preferences are ordered and copied during construction. The
default is Snappy with an uncompressed fallback. `CompressionNone` is valid
only as the final preference; use it alone to disable compression. Confirm
broker and consumer compatibility before selecting Zstandard.

Call `Shutdown` with a bounded context for graceful drain and close. A failed
drain fences new production while retaining admitted records so the caller can
retry shutdown or explicitly accept data loss through `Abort`. `Close` uses the
configured bounded `ShutdownTimeout` and returns any incomplete-drain error.

Set a unique `TransactionalID` to use `RunTransaction` for Kafka-only atomic
production. Transaction lifecycle failures are redacted `TransactionError`
values with an operation, stable category, abortability, and explicit outcome
knowledge. An unknown commit outcome requires reconciliation rather than a
blind retry. This producer-only callback does not include consumer offsets;
therefore it does not yet provide consume-transform-produce exactly-once
processing.

Use `ClientSecurity{TLS: tlsConfig}` for caller-provided roots or static mTLS
material. PLAIN, SCRAM-SHA-256, SCRAM-SHA-512, and OAUTHBEARER use the package's
bounded credential-provider contracts; no franz-go authentication type appears
in the public API. Unencrypted connections require the visibly development-only
`DevelopmentPlaintextSecurity()` policy and cannot be combined with
authentication.

Kafka request versions are negotiated per broker connection by default. Set a
validated `ProtocolPolicy.MinimumVersion` only when a reviewed capability must
not downgrade below a known Kafka request table. This is not a broker-version
or support check. See the [configuration reference](docs/configuration.md).

## Consumer

```go
consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
    Brokers:     brokers,
    ClientID:    "track-delivery-planner",
    GroupID:     "track-outbound-delivery-planner-v1",
    Topics:      []string{"track.tracking-event.v1"},
    ResetOffset: kafka.OffsetEarliest,
    BalancePolicy: kafka.BalanceCooperativeSticky,
})
if err != nil {
    return err
}

runErr := consumer.Run(ctx, kafka.HandlerFunc(func(
    ctx context.Context,
    message kafka.ConsumedMessage,
) error {
    return durableInbox.Insert(ctx, message.Value)
}))
return errors.Join(runErr, consumer.Close())
```

Offsets are committed only after handler success. Each partition stops at its
first failure; its successful contiguous prefix and successful independent
partitions are committed before the error is returned. Later records in the
failed partition are skipped and remain available for redelivery. Handlers must
tolerate duplicates. Retain copies if bytes are needed after the handler
returns.

When a rebalance waits on an active poll, the default policy requests handler
cancellation and stops admitting later records. Handlers must honor their
context. Select `RebalanceDrainHandler` only when the active handler can finish
within the validated rebalance deadline relationship.

Consumer fetches default to at most four concurrent broker requests, 50 MiB per
request, and 1 MiB per partition. All three limits are explicit and validated.
A broker can return one record batch larger than the per-partition limit so a
consumer can make progress; broker topic limits must remain compatible.
Only one `Run` or `RunOnce` call may be active. Graceful shutdown fences new
runs, waits for the active runner, and leaves dynamic group membership before
closing. Cancel the runner context first, then call `Shutdown` with a bounded
context or handle the error returned by `Close`.
`PausePartitions` and `ResumePartitions` control future fetches for explicit
subscribed topic-partitions. Pausing does not retract records already buffered
or returned by the current poll; `MaxPausedPartitions` bounds both each request
and accumulated paused state.
`Assignment` returns a sorted copied snapshot of current partition ownership.
Its epoch is a package-local settlement fence rather than Kafka's protocol
generation ID. Broker-controlled assignment metadata is fail-closed and
bounded by `MaxAssignedPartitions`, which defaults to 1,024.

Use `RunBatchOnce` with a `BatchHandler` when one call should receive all
records returned for a single partition. Batch success settles the whole
partition batch; an error settles none of it while independent successful
partition batches can still advance. It does not provide cross-partition
atomicity.

New groups default to cooperative-sticky balancing. Existing eager groups must
use `BalanceEagerToCooperative` for one complete rolling deployment before all
members switch to `BalanceCooperativeSticky`; joining a mixed group directly
with cooperative-only policy is unsafe. `BalanceEagerSticky` remains available
for eager compatibility. Optional `InstanceID` enables static membership and
`Rack` enables broker-supported rack-aware fetching. See the
[consumer guide](docs/consumer.md) for rollout and close semantics.

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
