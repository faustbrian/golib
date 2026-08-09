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

Production retries use bounded exponential per-client jitter. The default
range is 250 milliseconds through 1 second and is configurable through
`RetryBackoffMin` and `RetryBackoffMax`. Non-transactional delivery remains
bounded when a broker response is lost; the resulting timeout is classified
as ambiguous because Kafka may already contain the record. Do not resubmit
that record without reconciliation and an explicit duplicate policy.
Synchronous caller cancellation can stop an admitted record and is ambiguous
for the same reason. Asynchronous caller cancellation applies while admission
is blocked; after `PublishAsync` returns, the producer-owned record continues
under the configured delivery bound and reports its eventual buffered result.

Call `Shutdown` with a bounded context for graceful drain and close. A failed
drain fences new production while retaining admitted records so the caller can
retry shutdown or explicitly accept data loss through `Abort`. `Close` uses the
configured bounded `ShutdownTimeout` and returns any incomplete-drain error.
`Producer.Diagnostic` is the payload-free local status surface for admission,
transaction ownership, maintenance, shutdown, fatal category, and current
franz-go buffered record and byte counts. It performs no Kafka request and does
not prove broker connectivity, coordinator state, or record delivery; use the
`Health` probe, which derives `ProducerConfig.RequestTimeout`, and `Inspector`
separately for those concerns.

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
Kafka protocol requests, throttling, disconnects, and shutdown attempts.
Broker-connect events identify only the configured bounded SASL method; a
successful event covers API-version negotiation and that authentication flow.
Events contain copied payload-free metadata and never broker endpoints or
credentials; callback errors and panics cannot replace the delivery result.
Callbacks are cooperatively deadline-bound and must not re-enter the producer.
See the
[observability guide](docs/observability.md).

Set a unique `TransactionalID` to use `RunTransaction` for Kafka-only atomic
production. Transaction lifecycle failures are redacted `TransactionError`
values with an operation, stable category, abortability, and explicit outcome
knowledge. An unknown commit outcome requires reconciliation rather than a
blind retry. If an admitted transactional publish loses every response and its
delivery context expires, the producer closes its franz-go client, returns an
ambiguous delivery joined with `ErrProducerFatal`, and rejects reuse. The open
broker transaction is never committed by that client; replace the producer
with the same reviewed transactional identity to fence and recover it.
Producer observers report payload-free begin, commit, and abort outcomes
without changing the transaction result.

Use `TransactionProcessor` when source offsets and Kafka output records must be
committed atomically. It always reads with `read_committed`, disables automatic
commits, and treats one bounded poll as one transaction. Every fetched record
must succeed; any validation, handler, panic, timeout, or delivery failure
aborts all poll outputs and leaves every source offset for redelivery.
`TransactionalID` must be unique to one live processor instance. This guarantee
also applies when an admitted output loses every response: the processor closes
and returns `ErrTransactionProcessorFatal`, no source offset is committed, and
the application must construct a replacement before retrying. This guarantee
ends at the Kafka read-process-write boundary and never includes databases,
HTTP calls, object storage, email, or other external effects. See the
[transaction guide](docs/transactions.md). Processor observers report the same
transaction lifecycle plus shutdown attempts and broker activity with copied
client and group identity.
`TransactionProcessor.Diagnostic` likewise reports only package-local run,
transaction, shutdown, fatal-category, forced-client-termination, and buffered
output state. In particular, `TransactionActive` is the local transaction
attempt boundary, not an authoritative coordinator description or commit
outcome.

Use `ClientSecurity{TLS: tlsConfig}` for caller-provided static roots or mTLS
material. Use `TrustAnchorProvider` for bounded overlap-first server trust
rotation; it supplies the complete root set for each new TLS connection and
cannot be combined with static `TLS.RootCAs`. PLAIN, SCRAM-SHA-256,
SCRAM-SHA-512, and OAUTHBEARER use the package's bounded credential-provider
contracts; no franz-go authentication type appears in the public API. An OAuth
provider may acquire `client_credentials` tokens from an external HTTPS
endpoint, but the provider owns endpoint trust, request and response bounds,
token caching, refresh scheduling, and identity-provider-specific behavior.
Unencrypted connections require the visibly development-only
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
All package-owned clients retry a broker EOF that occurs before the first
response, within their existing operation deadlines, so broker restart and
coordinator failover do not permanently poison a new client. A listener,
TLS, or SASL mismatch can therefore surface only when that bound expires.

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

`NewBatchFailureHandler` applies the same explicit strategies to the complete
partition batch accepted by `RunBatchOnce`. Retries repeat the whole batch.
Retry-topic and dead-letter modes submit every source record through one
bounded publication call with input-ordered results and permit source
settlement only when every delivery is definitely successful. Partial target
delivery leaves the entire
source batch unsettled and is exposed through
`FailureHandlingError.DeliveryResults`; a later redelivery can therefore
duplicate target records that already succeeded.

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
request, and 1 MiB per partition. A separate 64 MiB hard broker-response cap,
8 MiB decoded-batch cap, and 64 MiB active decoded-buffer budget prevent a
broker progress exception or compressed batch from bypassing memory policy.
All limits are explicit and validated.
A broker can return one record batch larger than the per-partition limit so a
consumer can make progress; the hard response and decompression limits still
fail closed before handler admission or offset settlement. Broker topic limits
must remain compatible.
Application callbacks default to one at a time. Set
`MaxConcurrentHandlers` explicitly, up to 64, to process independent
partitions concurrently; records inside each partition remain sequential and
the handler must then be concurrency-safe. Only one `Run`, `RunOnce`, or
`RunBatchOnce` call may be active. `Drain` interrupts an idle poll, lets an
already-admitted poll finish handling and contiguous settlement, and returns
without leaving the group or closing the client. An incomplete drain fences
new work until a successful retry. Graceful shutdown uses the same drain
boundary before leaving dynamic group membership and closing, so caller-owned
runner cancellation is optional. Each admitted shutdown attempt emits one
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
Wrap a `BatchHandler` with `NewBatchFailureHandler` when failure handling must
remain whole-batch. It validates and owns the complete batch before retrying or
rerouting it; it never guesses which records an application handler may have
processed.

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
unclean-election policy, plus separate classic and KIP-848 consumer-group lag,
member identity, assignment, and reconciliation state. Every operation derives
`RequestTimeout`; response copying is
capped by explicit broker, group-member, partition, and configuration limits.
Consumer-group inspection requests KIP-447 stable offsets so pending
transactional commits resolve within that deadline on supported brokers.
`ConsumerProtocolGroupLag` preserves KIP-848 group, assignment, and member
epochs plus distinct current and target assignments; it does not silently
reinterpret classic state. `InspectTopics`, `InspectConsumerGroups`, and
`InspectConsumerProtocolGroups` are the bounded per-target variants: they
preserve input order and independent successes, attach stable error categories
to failed targets, and return `ErrInspectionTargetsFailed` when any target
fails. Their independent requests share one deadline and are limited by
`MaxConcurrentInspections`. `Topics`, `ConsumerGroupLag`, and
`ConsumerProtocolGroupLag` remain the fail-closed batch methods. Kafka reports
an unknown classic group as a successful `Dead` group state rather than a
target error.
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
[public conformance suites](docs/conformance.md),
[observability](docs/observability.md), [performance evidence](docs/performance.md),
[operations](docs/operations.md), and [security](docs/security.md).

## Development

Run `make check`. `make conformance` exercises the public `kafkatest` producer,
consumer, transaction, replay, inspector, authentication-provider, and observer
contracts against the pinned single-broker fixture and deterministic provider
implementations. With Docker available, `make integration` exercises the
package against one pinned Confluent Local 7.5.0 broker and three pinned Apache
Kafka 4.3.1 combined KRaft broker/controller nodes. Separate pinned Apache
Kafka 4.3.1 fixtures prove verified TLS 1.2 and 1.3, mutual TLS, PLAIN,
SCRAM-SHA-256, SCRAM-SHA-512, and signed-JWT OAUTHBEARER through the package's
producer, consumer, inspector, and provider-backed authentication policies.
An authenticated but ACL-denied principal also proves producer and inspector
authorization failures. Three independent producers for each SCRAM mechanism
cross broker-enforced reauthentication through three successive credential
replacements, refresh every provider, verify every acknowledged record, and
reject every retired secret. Three independent OAUTHBEARER producers similarly
cross three broker-enforced reauthentication cycles through successive signed
JWT replacements, refresh every provider, and preserve every acknowledged
record; retired tokens remain valid until their signed expiry. A separate
fixture refreshes Kafka's production validator from a verified HTTPS JWKS,
accepts a new RS256 signing key during overlap, then rejects the still-valid
retired key after its removal. RFC 7628 broker error challenges are normalized
to a stable redacted authentication identity. Another fixture obtains and
refreshes those signed tokens from a verified HTTPS `client_credentials`
endpoint, proves cancellation reaches the endpoint, rejects an untrusted peer,
and preserves every acknowledged record. Three provider-backed PLAIN
producers additionally recover after a bounded broker
restart replaces the server credential, verify every acknowledged record, and
reject the retired password. This does not claim zero-downtime PLAIN rotation.
Three independent mTLS producers also cross three broker-enforced idle
disconnect cycles, obtain each successive client certificate from every
provider, and preserve every acknowledged record. A compact-only Apache topic
also proves replay fails closed on a missing
requested offset while the broker log start remains unchanged. A three-process
consumer fixture proves the documented
eager-to-cooperative rolling protocol transition with exact partition
ownership. The Apache
fixtures assert the runtime version; the failure fixture proves one bounded
leader/ISR failure and recovery scenario. They are not the complete managed
service, cross-mechanism rotation, compatibility, or chaos matrix. Exact
inputs and remaining evidence gaps are recorded in the
[compatibility matrix](docs/compatibility.md). Security reports follow
[SECURITY.md](SECURITY.md). The module is licensed under the [MIT
License](LICENSE).

The non-releasable [`benchmarks/clients`](benchmarks/clients) module keeps
franz-go, kafka-go, Sarama, benchstat, and broker-fixture comparison tooling out
of the production dependency graph. Its producer ranking includes only clients
that can match the idempotent all-ISR contract; see the
[performance guide](docs/performance.md) for the current capture and remaining
matrix.

From the repository root, `make release-dry-run MODULES=pkg/kafka` verifies the
committed module through a fresh `GOWORK=off` consumer and local source proxy.
That local proof does not replace published-tag and public-proxy verification.
