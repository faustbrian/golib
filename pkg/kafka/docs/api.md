# API

Use `NewProducer`, `NewConsumer`, `NewReplayReader`, and `NewInspector` as the
composition roots. Every constructor validates identities and bounded resource
policy before franz-go is configured.

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

`Consumer.RunOnce` returns one bounded poll result. `Consumer.Run` exits cleanly
when its context is canceled.
`ReplayReader.Replay` completes only after every requested offset is processed.
`Inspector.Topics` and `Inspector.ConsumerGroupLag` require explicit bounded
target lists.

`ProducerConfig.CompressionPreferences` is an ordered, constructor-copied list
of `CompressionCodec` values. An empty list defaults to Snappy followed by no
compression. Duplicate codecs and ineffective orders are rejected, and
`CompressionNone` may appear only last.

The canonical machine-checked exported API is in `api/baseline.txt`.
