# API

Use `NewProducer`, `NewConsumer`, `NewReplayReader`, and `NewInspector` as the
composition roots. Every constructor validates identities and bounded resource
policy before franz-go is configured.

Topic names are validated consistently at every producer, consumer, replay,
and inspection boundary against Kafka's broker naming rules: 1 to 249 ASCII
bytes containing only alphanumerics, `.`, `_`, or `-`, excluding `.` and `..`.
Invalid names fail before client admission or a broker request.
Producer construction additionally requires 1 to 64 unique
`ProducerConfig.AllowedTopics`. The constructor copies the slice, and every
single, batch, asynchronous, and transactional publish rejects a topic outside
the resulting immutable allowlist with `ErrTopicNotAllowed`.

Use `ProducerConfig.Validate` when a composition root must validate producer
policy before constructing a client. It applies the same validation and
defaulting policy as `NewProducer` without allocating a client or dialing
brokers.

`Producer.Publish` is the compatibility synchronous error-only method.
`Producer.PublishRecord` returns one `DeliveryResult`; `PublishBatch` returns
input-ordered results including partial failures; and `PublishAsync` returns a
buffered one-result channel after bounded admission. `ProducerRecord` input
bytes are copied before admission. `ConsumedRecord.Retain` makes an owned copy
of borrowed fetch bytes for retention beyond a handler call.

`ProducerConfig.KeyPolicy` defaults to `KeyRequired`; callers must select
`UnkeyedAllowed` explicitly when unkeyed partition selection is intended.
`ProducerRecord.Partition` defaults to automatic selection. Use
`ExplicitPartition` for an exact non-negative partition; invalid modes and
partition numbers fail before producer admission. An explicit partition
overrides key-based selection but does not remove the configured key
requirement.
Producer admission is bounded independently by record count and total buffered
bytes; a Kafka batch also has its own smaller byte and record limits.
`Producer.Drain` preserves admitted records, `Abort` explicitly discards
buffered records, and `Shutdown` performs a bounded drain before close.
`ProducerConfig.ShutdownTimeout` defaults to `DeliveryTimeout`, cannot be
shorter than delivery, and bounds the error-returning `Close` convenience
method. Shutdown first fences new work and waits for already-started calls to
finish backend admission so a concurrent flush cannot miss them.
Every broker delivery failure is a redacted `DeliveryError`. Its stable
category distinguishes retryable, authorization, fenced, oversized, timeout,
canceled, shutdown, fatal producer-state, permanent, and ambiguous outcomes.
`errors.Is` and `errors.As` retain the underlying identity for deliberate
inspection; application retry is a separate policy decision.
The franz-go backend is configured to stop after detecting idempotent-producer
data loss; a fatal delivery requires producer replacement or an explicit
application recovery decision.

`Consumer.RunOnce` returns one bounded poll result. Processing is sequential
within a partition. After one partition fails, its later fetched records are
skipped while independent partitions continue; only each partition's contiguous
successful prefix is submitted for commit. `Consumer.Run` exits cleanly when
its context is canceled.
`ConsumerConfig.MaxConcurrentFetches`, `FetchMaxBytes`, and
`FetchMaxPartitionBytes` jointly bound compressed fetch buffering. The
per-partition limit follows Kafka's progress rule: one larger record batch may
still be returned.
`ConsumerConfig.BalancePolicy` selects cooperative-sticky, eager-sticky, or the
ordered eager-to-cooperative migration pair without exposing franz-go
balancers. Optional validated `InstanceID` and `Rack` values select static
membership and rack-aware fetching respectively.
`ReplayReader.Replay` completes only after every requested offset is processed.
`Inspector.Topics` and `Inspector.ConsumerGroupLag` require explicit bounded
target lists.

`ProducerConfig.CompressionPreferences` is an ordered, constructor-copied list
of `CompressionCodec` values. An empty list defaults to Snappy followed by no
compression. Duplicate codecs and ineffective orders are rejected, and
`CompressionNone` may appear only last.

The canonical machine-checked exported API is in `api/baseline.txt`.
