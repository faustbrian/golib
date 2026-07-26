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

Idempotence, all-ISR acknowledgements, ordering-preserving production, and
data-loss detection are mandatory package policy rather than configurable
options.

## Consumer group

| Field | Default | Allowed policy |
| --- | ---: | --- |
| `GroupID` | none | Required valid UTF-8 identifier of at most 255 bytes. |
| `InstanceID`, `Rack` | empty | Optional valid UTF-8 identifiers of at most 255 bytes. |
| `Topics` | none | Required 1 to 64 unique topics fitting `Limits.MaxTopicBytes`. |
| `ResetOffset` | unset and rejected | Explicitly `OffsetEarliest` or `OffsetLatest`. |
| `BalancePolicy` | `BalanceCooperativeSticky` | Cooperative, eager, or the eager-to-cooperative rollout policy. |
| `Limits` | `DefaultMessageLimits` | Applied before package metadata copies and handler invocation. |
| `MaxPollRecords` | 100 | 1 to 1,000. |
| `MaxConcurrentFetches` | 4 | 1 to 64. |
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

Automatic commits remain disabled and cannot be enabled through configuration.
See the [consumer guide](consumer.md) for settlement and rollout semantics.

## Replay and inspection

Replay requires 1 to 1,024 unique topic-partition ranges. Start offsets are
inclusive, end offsets are exclusive, partitions and offsets are non-negative,
and every end must exceed its start. Replay defaults to 100 poll records,
50 MiB fetches, 500 millisecond fetch wait, 30 second handler timeout, and a
10 second dial timeout. Their ranges match the corresponding consumer bounds.

Inspection has only the shared connection policy. Each operation separately
requires a bounded explicit target set; construction does not authorize topic,
group, offset, ACL, or broker mutation.

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
