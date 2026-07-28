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

Services should compose concrete producers and consumers through
the independently versioned
[`kafkaservice`](kafkaservice) module; see the
[service integration guide](docs/service-integration.md). The adapter retains
the exact Kafka client, keeps startup and readiness policy explicit, drains
accepted publishes before closing an owned producer, supervises consumer
intake through the service task lifecycle, and creates correlation-owned
record and delivery hops. Optional W3C trace propagation uses a caller-owned
OpenTelemetry propagator without installing global providers. These
adapter dependencies do not enter the root Kafka production package or its
direct requirements.

Optional `ProducerConfig.Observers` receive ordered synchronous completion
events for record, batch, and asynchronous delivery plus broker connections,
Kafka protocol requests, throttling, disconnects, and shutdown attempts. Events contain copied
payload-free metadata and never broker endpoints; callback errors and panics
cannot replace the delivery result. Callbacks are cooperatively deadline-bound
and must not re-enter the producer. See the
[observability guide](docs/observability.md).

Set a unique `TransactionalID` to use `RunTransaction` for Kafka-only atomic
production. Transaction lifecycle failures are redacted `TransactionError`
values with an operation, stable category, abortability, and explicit outcome
knowledge. An unknown commit outcome requires reconciliation rather than a
blind retry. Producer observers report payload-free begin, commit, and abort
outcomes without changing the transaction result.

Use `TransactionProcessor` when source offsets and Kafka output records must be
committed atomically. It always reads with `read_committed`, disables automatic
commits, and treats one bounded poll as one transaction. Every fetched record
must succeed; any validation, handler, panic, timeout, or delivery failure
aborts all poll outputs and leaves every source offset for redelivery.
`TransactionalID` must be unique to one live processor instance. This guarantee
ends at the Kafka read-process-write boundary and never includes databases,
HTTP calls, object storage, email, or other external effects. See the
[transaction guide](docs/transactions.md). Processor observers report the same
transaction lifecycle plus shutdown attempts and broker activity with copied
client and group identity.

Use `ClientSecurity{TLS: tlsConfig}` for caller-provided roots or static mTLS
material. PLAIN, SCRAM-SHA-256, SCRAM-SHA-512, and OAUTHBEARER use the package's
bounded credential-provider contracts; no franz-go authentication type appears
in the public API. Unencrypted connections require the visibly development-only
`DevelopmentPlaintextSecurity()` policy and cannot be combined with
authentication. The independently versioned
[`adapters/mskiam`](adapters/mskiam) module supplies AWS's supported
SASL/OAUTHBEARER signer and refreshing SDK v2 credential chain without adding
AWS dependencies to this root module. Amazon MSK Provisioned and Serverless
remain unverified until direct repository integration evidence exists.

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

Optional `ConsumerConfig.Observers` report copied, payload-free record,
partition-batch, commit, complete poll, broker connection, Kafka request,
throttle, disconnect, and shutdown outcomes. Commit failures report zero committed
records even when handlers succeeded. Consumer callbacks can run concurrently
across partition workers and franz-go broker goroutines, execute before the
rebalance gate is released, and cannot re-enter mutating or lifecycle
operations on the consumer. See the
[observability guide](docs/observability.md). The independently versioned
[`adapters/gotelemetry`](adapters/gotelemetry) module maps these stable events
to OpenTelemetry with deny-by-default topic, group, and client attributes; it
does not install providers or own record-header context propagation.
The independently versioned `kafkaservice` module separately accepts only the
OpenTelemetry propagation contract for explicit record headers. The
standard-library
[`adapters/golog`](adapters/golog) package maps the same observations to fixed
`log/slog` records. It also denies client, topic, and group identities unless
they are present in copied bounded allowlists. Adapter-generated fields never
contain payloads, headers, credentials, broker endpoints, or application error
text.

`NewFailureHandler` composes explicit stop, bounded in-process retry,
versioned retry-topic, versioned dead-letter, or application-delegated policy
around a per-record handler. The zero terminal mode stops without settling.
Retry categories, attempts, exponential backoff, publication time, target
topic, and target version are bounded. Retry and dead-letter records preserve
the original key, value, ordered headers, timestamp, source coordinates,
attempt, and a safe error category without adding handler error text.
Non-transactional target publication precedes source offset commit, so a crash
or ambiguous commit can duplicate the target record. Use
`TransactionProcessor` only when target publication and source settlement must
be one Kafka transaction. See the
[retry and dead-letter guide](docs/retry-dead-letter.md).

When a rebalance waits on an active poll, the default policy requests handler
cancellation for every active partition and stops every worker from admitting
later records. Handlers must honor their context. Select
`RebalanceDrainHandler` only when all active handlers can finish within the
validated rebalance deadline relationship.

Static `InstanceID` values must be unique within a group. If Kafka fences an
older duplicate member, its consumer permanently rejects new runners with
`ErrConsumerFatal` and `ErrConsumerInstanceFenced` while retaining the broker
cause; shut it down and construct a new consumer with a corrected identity.

Consumer fetches default to at most four concurrent broker requests, 50 MiB per
request, and 1 MiB per partition. All three limits are explicit and validated.
A broker can return one record batch larger than the per-partition limit so a
consumer can make progress; broker topic limits must remain compatible.
Application callbacks default to one at a time. Set
`MaxConcurrentHandlers` explicitly, up to 64, to process independent
partitions concurrently; records inside each partition remain sequential and
the handler must then be concurrency-safe. Only one `Run`, `RunOnce`, or
`RunBatchOnce` call may be active. Graceful shutdown fences new runs, waits for
the active runner, and leaves dynamic group membership before closing. Cancel
the runner context first, then call `Shutdown` with a bounded context or handle
the error returned by `Close`. Each admitted shutdown attempt emits one
payload-free observation, so an incomplete attempt and its successful retry
remain distinct.
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
without joining or changing a consumer group. `Plan` applies an owned external
checkpoint without contacting a broker. `PlanAgainstBroker` performs the same
dry run while confirming effective starts and exclusive ends against bounded
broker log-start and high-watermark lookups; it does not consume the reader.
Any validation error returns no plan.
`Inspector.PlanReplayByTimestamp` converts one millisecond-aligned,
inclusive-start and exclusive-end record-time window over explicit partitions
into owned exact offset ranges. It requests only those partitions, rejects
retention-ambiguous history, and performs no polling, group operation, or
handler call. Empty partition windows are visible in the timestamp plan and
omitted by `ReplayRanges`.
Handler execution requires explicit `ReplaySideEffectsAllowed`, enforces
record limits and handler deadlines, and returns exact per-range next offsets
through `ReplayResult.Checkpoint`. Replay repeats the broker-boundary validation
before the first handler. Unavailable bounds, out-of-range fetches, and offset
gaps fail closed instead of resetting.
Replay is sequential by default. Explicit `MaxConcurrentHandlers` values above
one permit bounded overlap only across independent partitions; each partition
remains ordered. A failed partition does not discard progress completed by
other partition batches already admitted from the same bounded poll.
`ProgressTimeout` prevents a compacted or empty exact range from polling
forever without advancing its checkpoint.
Optional `ReplayConfig.Observers` report payload-free broker-validated plans,
per-record outcomes, exact aggregate progress, bounded shutdown, and broker
activity. The callback policy is validated and copied before client
construction, and same-reader reentry fails closed.
Completed partitions are paused while other ranges finish. Cancel the replay
context, then use bounded `Shutdown` or error-returning `Close`. Readers are
single-use; resume with a new reader and the returned checkpoint.
`Inspector` provides bounded read-only cluster identity, controller and broker
visibility, topic replica/ISR/offline state, beginning and end offsets,
effective `min.insync.replicas`, cleanup, retention, compaction, segment, and
unclean-election policy, plus classic consumer-group lag, member identity, and
assignments. Every operation derives `RequestTimeout`; response copying is
capped by explicit broker, group-member, partition, and configuration limits.
Consumer-group inspection requests KIP-447 stable offsets so pending
transactional commits resolve within that deadline on supported brokers.
Inspection never mutates Kafka infrastructure. Retention and segment durations
remain raw Kafka milliseconds because valid broker values can exceed Go's
`time.Duration`.
Optional `InspectorConfig.Observers` report only bounded aggregate inspection
counts, dependency/readiness state, shutdown, and broker activity. Inspector
targets, cluster IDs, broker hosts, member identities, assignments, and lag
coordinates are never copied into observations.

`DependencyHealth` checks current bounded connectivity. `Readiness` applies
configurable consecutive-failure and recovery hysteresis and returns the
readiness decision separately from the latest dependency error. `Liveness`
only reports whether this inspector remains locally open; it never fails merely
because Kafka is unavailable and is not a complete process-liveness signal.
`Close` returns `ErrObserverReentry` for same-inspector callback reentry and is
otherwise idempotent.
`Health` remains an error-only compatibility alias for `DependencyHealth`.

See the [inspection guide](docs/inspection.md) for authorization, partial-state,
durability, and readiness boundaries.
Infrastructure remains responsible for topics, replication, ISR, retention,
quotas, ACLs, and destructive administrative operations.

See the [current audit](docs/audit.md), [compatibility matrix](docs/compatibility.md),
[documentation index](docs/README.md), [guarantees](docs/guarantees.md),
[observability](docs/observability.md), [operations](docs/operations.md), and
[security](docs/security.md).

## Development

Run `make check`. With Docker available, `make integration` exercises the
package against one pinned Confluent Local 7.5.0 broker and three pinned Apache
Kafka 4.3.1 combined KRaft broker/controller nodes. The Apache fixture asserts
the runtime version and proves one bounded leader/ISR failure and recovery
scenario; it is not the complete auth, compatibility, or chaos matrix. Exact
inputs and remaining evidence gaps are recorded in the
[compatibility matrix](docs/compatibility.md). Security reports follow
[SECURITY.md](SECURITY.md). The module is licensed under the [MIT
License](LICENSE).
