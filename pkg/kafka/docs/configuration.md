# Configuration reference

Every constructor validates its complete policy before allocating a franz-go
client. Configuration is passed by value and is not retained as a mutable
control surface. Invalid combinations fail closed; there is no unrestricted
`kgo.Opt` escape hatch.

## Shared connection policy

| Field | Default | Validation and meaning |
| --- | --- | --- |
| `Brokers` | none | Required ordered list of 1 to 32 unique seed addresses. Empty, padded, oversized, invalid UTF-8, or control-containing values are rejected. Seeds are discovery inputs, not a complete broker allowlist. |
| `ClientID` | none | Required valid UTF-8 identifier of at most 255 bytes without padding or control characters. |
| `Protocol.MinimumVersion` | empty | Uses franz-go `ApiVersions` negotiation with no package-imposed downgrade floor. A configured value must be a Kafka release recognized by the pinned franz-go version table. |
| `Security` | verified TLS | TLS 1.2 minimum with system roots. Plaintext requires `DevelopmentPlaintextSecurity`. See the [security guide](security.md). |
| `DialTimeout` | 10 seconds | 100 milliseconds to 2 minutes. |

Kafka brokers advertise supported request versions per connection through
`ApiVersions`. franz-go normally selects the newest request version supported
by both client and broker. `MinimumVersion` maps to franz-go `MinVersions`: it
prevents known requests from silently downgrading below that release's request
versions. It does **not** pin a maximum request version, prove the broker's
release, or establish a compatibility claim. Requests unknown to the selected
minimum table remain allowed by franz-go.
See the Apache Kafka
[`ApiVersions` negotiation sequence](https://kafka.apache.org/43/design/protocol/#protocol_compatibility)
and the pinned franz-go
[`MinVersions` contract](https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo@v1.21.5#MinVersions).

```go
protocol := kafka.ProtocolPolicy{MinimumVersion: "3.9"}
if err := protocol.Validate(); err != nil {
    return err
}
```

Patch forms such as `3.9.1` are accepted but select franz-go's `3.9` request
table because Kafka protocol tables are release-family based. An empty policy
is the recommended default unless the application has reviewed a feature that
must never be downgraded.
The recognized version set changes only when the pinned franz-go dependency is
reviewed and updated; that validation change is release-note material.

## Record limits

The zero `MessageLimits` value becomes `DefaultMessageLimits`:

| Field | Default | Maximum configurable value |
| --- | ---: | ---: |
| `MaxTopicBytes` | 249 | 249 |
| `MaxKeyBytes` | 64 KiB | 16 MiB |
| `MaxValueBytes` | 900 KiB | 100 MiB |
| `MaxHeaders` | 64 | 10,000 |
| `MaxHeaderKeyBytes` | 128 | 64 KiB |
| `MaxHeaderValueBytes` | 8 KiB | 100 MiB |
| `MaxHeaderBytes` | 32 KiB | 100 MiB |

Every value must be positive. Aggregate header bytes include every header key
and value. Producer records are checked before admission. Consumer records are
checked before the package copies header metadata or invokes the handler.
Broker fetch decoding happens below this policy, so fetch byte limits remain a
separate and necessary bound.

## Producer

| Field | Default | Allowed policy |
| --- | ---: | --- |
| `AllowedTopics` | none | Required 1 to 64 unique Kafka topic names; copied during construction. |
| `KeyPolicy` | `KeyRequired` | `UnkeyedAllowed` must be selected explicitly. |
| `MaxBufferedRecords` | 1,000 | 1 to 100,000. |
| `MaxBufferedBytes` | 64 MiB | At least `MaxBatchBytes`, at most 1 GiB. |
| `MaxBatchRecords` | 100 | 1 to 10,000. |
| `MaxBatchBytes` | 1 MiB | 512 bytes to 100 MiB and large enough for one maximum policy record. |
| `RecordRetries` | 10 | 1 to 1,000 franz-go production tries; no application retry is added. |
| `DeliveryTimeout` | 30 seconds | 1 second to 10 minutes. |
| `ShutdownTimeout` | delivery timeout | At least delivery timeout, at most 15 minutes. |
| `RequestTimeout` | 10 seconds | 100 milliseconds to 2 minutes and no longer than delivery timeout. |
| `Linger` | 5 milliseconds | 0 to 1 second. |
| `CompressionPreferences` | Snappy, then none | 1 to 5 unique codecs; `CompressionNone` may appear only last. |
| `TransactionalID` | empty | Enables transactions when set; valid UTF-8, no padding/control characters, at most 255 bytes. |
| `TransactionTimeout` | 30 seconds when transactional | 1 second to 15 minutes. Must be zero without a transactional ID. |
| `TransactionEndTimeout` | 30 seconds when transactional | 1 second to 2 minutes. Must be zero without a transactional ID. |
| `Observers` | disabled | Optional 1 to 16 ordered synchronous delivery, transaction, shutdown, and broker callbacks, a required failure handler, and one shared 1 millisecond to 5 second cooperative timeout. The default timeout is 100 milliseconds. |

Idempotence, all-ISR acknowledgements, ordering-preserving production, and
data-loss detection are mandatory package policy rather than configurable
options.

Observer callback slices are copied. Callback functions and captured state
remain application-owned and must be concurrency-safe. See the
[observability guide](observability.md) for metadata, failure, deadline, and
reentrancy rules.

## Consume-transform-produce

`TransactionProcessorConfig` separates the connection, source group, output,
record limits, observer policy, and lifecycle timeout. It reuses the same
validation bounds as the producer and consumer policies. The processor always
selects `read_committed` and disables automatic commits; neither is
caller-configurable.

| Concern and field | Default | Validation and meaning |
| --- | ---: | --- |
| `Connection.Brokers`, `ClientID`, `Security`, `DialTimeout` | shared defaults above | One immutable connection policy for both group consumption and transactional production. |
| `Connection.Protocol.MinimumVersion` | Kafka 2.5 | A caller may raise but not lower the request-version floor; franz-go avoids requests that would need to downgrade below KIP-447 rather than using its older best-effort transaction-marker delay. |
| `Group.GroupID` | none | Required bounded Kafka consumer-group identity. |
| `Group.InstanceID`, `Rack` | empty | Optional static member and rack identities. |
| `Group.Topics` | none | Required 1 to 64 unique copied source topics. |
| `Group.ResetOffset` | none | Explicit `OffsetEarliest` or `OffsetLatest` is required. |
| `Group.BalancePolicy` | cooperative-sticky | Cooperative, eager, or reviewed eager-to-cooperative migration. |
| `Group.MaxPollRecords` | 100 | 1 to 1,000; this is the maximum all-or-nothing transaction input count. |
| `Group.MaxConcurrentFetches` | 4 | 1 to 64. |
| `Group.FetchMaxBytes` | 50 MiB | 1 to 100 MiB. |
| `Group.FetchMaxPartitionBytes` | 1 MiB | 1 MiB through the aggregate fetch maximum. |
| `Group.FetchMaxWait` | 500 milliseconds | 1 millisecond to 30 seconds. |
| `Group.SessionTimeout` | 45 seconds | 1 second to 6 minutes. |
| `Group.RebalanceTimeout` | 60 seconds | 1 second to 10 minutes. |
| `Group.HeartbeatInterval` | 3 seconds | 100 milliseconds and less than the session timeout. |
| `Group.ProcessingTimeout` | 30 seconds | Bounds the complete poll's application processing, not each record independently. |
| `Output.AllowedTopics` | none | Required 1 to 64 unique copied output topics; source overlap is rejected. |
| `Output.KeyPolicy` | `KeyRequired` | Unkeyed output requires explicit `UnkeyedAllowed`. |
| `Output.MaxBufferedRecords` | 1,000 | 1 to 100,000. |
| `Output.MaxBufferedBytes` | 64 MiB | At least the batch maximum and at most 1 GiB. |
| `Output.MaxBatchBytes` | 1 MiB | 512 bytes to 100 MiB and sufficient for one policy record. |
| `Output.MaxOutputRecords` | 1,000 | 1 to 100,000 records acknowledged or attempted inside one source-poll transaction. |
| `Output.MaxOutputBytes` | 10 MiB | At least one maximum policy record and at most 1 GiB per source-poll transaction. |
| `Output.RecordRetries` | 10 | 1 to 1,000 idempotent franz-go attempts. |
| `Output.DeliveryTimeout` | 30 seconds | 1 second to 10 minutes. |
| `Output.RequestTimeout` | 10 seconds | 100 milliseconds to 2 minutes and no longer than delivery. |
| `Output.Linger` | 5 milliseconds | 0 to 1 second. |
| `Output.CompressionPreferences` | Snappy, then none | Same ordered producer compression policy. |
| `Output.TransactionalID` | none | Required, unique to one live processor, valid and at most 255 bytes. |
| `Output.TransactionTimeout` | 60 seconds | 1 second to 15 minutes; must exceed processing plus end time. |
| `Output.TransactionEndTimeout` | 10 seconds | 1 second to 2 minutes; combined heartbeat, processing, and end time must fit inside rebalance time. |
| `Observers` | disabled | Same copied 1 to 16 transaction-lifecycle and broker callback policy and shared 1 millisecond to 5 second cooperative timeout as the producer. |
| `ShutdownTimeout` | 30 seconds | 100 milliseconds to 15 minutes. |

## Consumer group

| Field | Default | Allowed policy |
| --- | ---: | --- |
| `GroupID` | none | Required valid UTF-8 identifier of at most 255 bytes. |
| `InstanceID`, `Rack` | empty | Optional valid UTF-8 identifiers of at most 255 bytes. |
| `Topics` | none | Required 1 to 64 unique topics fitting `Limits.MaxTopicBytes`. |
| `ResetOffset` | unset and rejected | Explicitly `OffsetEarliest` or `OffsetLatest`. |
| `BalancePolicy` | `BalanceCooperativeSticky` | Cooperative, eager, or the eager-to-cooperative rollout policy. |
| `RebalanceHandler` | `RebalanceCancelHandler` | Cancel every active handler or drain only handlers already active when a rebalance callback is blocked. |
| `Limits` | `DefaultMessageLimits` | Applied before package metadata copies and handler invocation. |
| `MaxPollRecords` | 100 | 1 to 1,000. |
| `MaxPausedPartitions` | 256 | 1 to 1,024; bounds each pause/resume request and accumulated pauses. |
| `MaxAssignedPartitions` | 1,024 | 1 to 65,536; bounds broker-controlled assignment callback state and copied diagnostics. |
| `MaxConcurrentFetches` | 4 | 1 to 64. |
| `MaxConcurrentHandlers` | 1 | 1 to 64 callbacks across independent partitions; one partition always remains sequential. |
| `FetchMaxBytes` | 50 MiB | 1 to 100 MiB compressed fetch bytes. |
| `FetchMaxPartitionBytes` | 1 MiB | At least 1 MiB and no greater than `FetchMaxBytes`. Kafka may return one larger record batch to make progress. |
| `FetchMaxWait` | 500 milliseconds | 1 millisecond to 30 seconds. |
| `SessionTimeout` | 45 seconds | 1 second to 6 minutes. |
| `RebalanceTimeout` | 60 seconds | 1 second to 10 minutes. |
| `HeartbeatInterval` | 3 seconds | 100 milliseconds or more and strictly less than session timeout. |
| `HandlerTimeout` | 30 seconds | 1 second to 30 minutes. |
| `CommitTimeout` | 10 seconds | 100 milliseconds to 2 minutes. |
| `ShutdownTimeout` | 30 seconds | 100 milliseconds to 15 minutes. |
| `DialTimeout` | 10 seconds | 100 milliseconds to 2 minutes. |
| `Observers` | disabled | Same copied 1 to 16 delivery and broker callback policy and shared 1 millisecond to 5 second cooperative timeout as the producer. Consumer partition workers and franz-go broker goroutines can invoke callbacks concurrently. |

`HeartbeatInterval + HandlerTimeout + CommitTimeout` must be strictly less
than `RebalanceTimeout`. Handler cancellation and deadlines remain cooperative;
applications must return when the handler context is done.
`MaxConcurrentFetches` bounds broker fetch requests and is independent of
`MaxConcurrentHandlers`, which bounds application callbacks. Values above one
require a concurrency-safe handler.

Automatic commits remain disabled and cannot be enabled through configuration.
`ConsumerConfig.Validate` applies these defaults and checks without allocating
a client or dialing brokers.
See the [consumer guide](consumer.md) for settlement and rollout semantics.

## Consumer failure handler

| Field | Default | Allowed policy |
| --- | ---: | --- |
| `Handler` | none | Required per-record handler. |
| `Classifier` | package classifier | Optional synchronous stable-category mapping for application errors. |
| `Retry.MaxAttempts` | 1 | 1 to 32, including the initial call. Values above 1 require bounded backoff. |
| `Retry.InitialBackoff` | none | 1 millisecond or more when retries are enabled. |
| `Retry.MaxBackoff` | none | At least the initial delay and at most 5 minutes. |
| `Retry.Categories` | `ErrorRetryable` when retries are enabled | Unique valid `ErrorCategory` values. No category is retried when only one attempt is configured. |
| `Mode` | `FailureModeStop` | Stop, versioned retry topic, versioned dead letter, or application delegate. |
| `Target` | none | Publish modes require a valid Kafka topic different from the runtime source topic and a non-zero application version. |
| `Publisher` | none | Required only for retry-topic and dead-letter modes. `Producer` satisfies the interface and independently enforces its topic allowlist. |
| `Delegate` | none | Required only for delegated mode. |
| `Limits` | `DefaultMessageLimits` | Bounds the complete target record after original data and 11 package metadata headers are preserved. |
| `PublishTimeout` | 10 seconds for publish modes | 100 milliseconds to 2 minutes; forbidden for non-publish modes. |

`FailureHandlerConfig.Validate` applies the same validation and defaulting as
`NewFailureHandler` without allocating resources. Retry-category slices are
copied. Interface implementations remain caller-owned and must be bounded,
concurrency-safe, and cancellation-aware for every handler using them.
Incompatible publisher, delegate, target, and timeout fields are rejected
instead of ignored. See the
[retry and dead-letter guide](retry-dead-letter.md).

## Replay and inspection

Replay requires 1 to 1,024 unique topic-partition ranges. Start offsets are
inclusive, end offsets are exclusive, partitions and offsets are non-negative,
and every end must exceed its start. The aggregate remaining offset span must
fit in a signed 64-bit integer. `Checkpoint.Positions` are copied and must be
unique, target configured ranges, and stay within inclusive-start through
inclusive-end next-offset bounds. Missing positions use the range start.

Timestamp replay planning accepts 1 to 1,024 unique explicit partitions across
at most 64 topics. Its common inclusive start and exclusive end must be exact
millisecond values at or after the Unix epoch, and the end must be later.
Planning inherits `InspectorConfig.RequestTimeout` and
`MaxMetadataPartitions`; it does not allocate topic-wide partition metadata.
The request partition slice is copied before sorting or broker use.

`SideEffects` defaults to `ReplaySideEffectsDenied`; only the explicit
`ReplaySideEffectsAllowed` value permits handler execution. `Limits` defaults
to `DefaultMessageLimits` and must also admit every configured topic. Replay
defaults to 100 poll records, one concurrent fetch, one handler, 50 MiB
aggregate fetch bytes, 1 MiB per-partition fetch bytes, 500 millisecond fetch
wait, a 10 second broker-bound planning timeout, 30 second handler and shutdown
timeouts, and a 10 second dial timeout. `MaxConcurrentFetches` and
`MaxConcurrentHandlers` each accept 1 through 64. Fetch concurrency bounds
franz-go broker requests independently of handler concurrency. Handler values
above one require a concurrency-safe callback and overlap only independent
partitions; each partition remains sequential.
The planning timeout accepts 100 milliseconds through 2 minutes.
`ProgressTimeout` defaults to 30 seconds, accepts 100 milliseconds through 30
minutes, and cannot be shorter than `FetchMaxWait`. Other ranges match the
corresponding consumer bounds.
`Observers` uses the same copied 1 to 16 callback policy and shared
1 millisecond to 5 second cooperative timeout as producer and consumer
configuration. Replay partition workers and franz-go broker goroutines can
invoke callbacks concurrently. `ReplayConfig.Validate` applies every replay,
security, limit, deadline, checkpoint, and observer rule without allocating a
Kafka client or retaining caller-owned slices.

Inspection uses the shared connection policy plus these owned bounds:

| Field | Default | Validation and meaning |
| --- | --- | --- |
| `RequestTimeout` | 10 seconds | 100 milliseconds through 2 minutes; each cluster, topic, group-lag, and dependency-health call derives this deadline. |
| `MaxMetadataBrokers` | 1,000 | 1 through 10,000; caps copied cluster brokers and every partition replica set. |
| `MaxMetadataPartitions` | 100,000 | 1 through 1,000,000; caps aggregate topic partitions, or group lag partitions plus assignment topic and partition entries, copied by one request. |
| `MaxGroupMembers` | 10,000 | 1 through 100,000; caps members copied across one explicit group request. |
| `Readiness.FailureThreshold` | 3 | 1 through 100 consecutive failed dependency probes before a ready inspector becomes unready. |
| `Readiness.RecoveryThreshold` | 2 | 1 through 100 consecutive successful dependency probes before initial or recovered readiness. |
| `Observers` | disabled | Shared `ObserverPolicy`: 1 through 16 copied ordered callbacks, explicit failure handler, and one 1 millisecond through 5 second cooperative event budget. |

Topic and group operations separately require 1 to 64 unique explicit targets.
Topic inspection lists metadata, log-start and high-watermark offsets, and
effective durability, cleanup, retention, compaction, segment, and
unclean-election configuration. It accepts at most 1,024 returned configuration
entries per topic and at most 64 bytes for each selected value. It fails closed
when any requested target or selected field is missing, inconsistent,
unauthorized, excessive, or unavailable. Kafka millisecond values remain
signed 64-bit integers because valid values may exceed `time.Duration`;
retention time and per-partition retention bytes preserve `-1` as unlimited.
Construction and inspection do not authorize topic, group, offset, ACL, or
broker mutation.

Readiness state belongs to one inspector instance and is safe for concurrent
probes. A nil or caller-canceled probe is inconclusive and does not advance
either threshold. A completed timeout or broker/authentication failure is an
unhealthy dependency observation. Use `ReadinessState.Ready` for the readiness
decision; the accompanying error is diagnostic and may be non-nil while a
previously ready instance remains inside its failure threshold.
Observer callbacks cannot re-enter inspection, health, readiness, or close on
the same inspector. `Close` returns `ErrObserverReentry` while any inspector
observer is active and otherwise remains idempotent.

## Ownership and logging

`AllowedTopics`, compression preferences, replay ranges, security material,
and TLS mutable data used after construction are copied where the package owns
their lifetime. Credential and certificate provider implementations remain
caller-owned and must be concurrency-safe for the client lifetime. Consult the
public Go documentation for callback-specific ownership.

Do not log configuration with generic reflection. `ClientSecurity.String` and
`GoString` are redacted, but broker URLs, application identifiers, and custom
provider values remain application-controlled. Record values, keys, arbitrary
headers, passwords, tokens, certificates, and private keys must not be logged.
