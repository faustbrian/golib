# Architecture

The module owns client-side Kafka policy, not domain schemas or cluster
infrastructure.

The independently versioned nested `kafkaservice` module is the integration
boundary for `github.com/faustbrian/golib/pkg/service`. It imports the root
Kafka, service, correlation, and optional OpenTelemetry propagation contracts.
None of those adapter-only dependencies enter this root module, and the
service module never imports Kafka. The adapter keeps concrete producer and
consumer APIs visible and adds only lifecycle, readiness, correlation, and
optional trace propagation. This direction prevents a dependency cycle and
keeps retry, settlement, and topic policy in Kafka.

- `Producer` validates bounded records and waits for broker delivery.
- `Consumer` uses a group, disables automatic commits, blocks rebalancing while
  a bounded poll is processed, runs a fixed-size worker set across independent
  partitions while keeping each partition sequential, and commits only each
  partition's contiguous durable success prefix.
- `FailureHandler` composes bounded per-record retry and terminal
  retry-topic, dead-letter, or delegated decisions without owning group
  offsets. Non-transactional target publication completes before the normal
  consumer submits its separate source commit.
- `NewBatchFailureHandler` applies the same explicit decisions to one complete
  partition batch. It retries or reroutes the whole batch and resolves it only
  after every target delivery is definitely successful; it never infers a
  successful prefix from an application batch error.
- `Transaction` serializes a configured transactional producer and prevents a
  retained callback capability from publishing after completion.
- `TransactionProcessor` owns one read-committed group member and transactional
  producer; it commits one complete bounded source poll and its Kafka outputs
  together or aborts both.
- `ReplayReader` directly assigns explicit no-reset partition ranges, applies
  caller-owned checkpoints, requires explicit side-effect authorization, and
  never joins or commits a consumer group.
- `Inspector` exposes bounded read-only cluster, broker, topic durability,
  partition offset, and lag state without infrastructure mutation.
- `ObserverPolicy` exposes ordered, payload-free producer delivery, consumer
  processing/commit/poll, and copied broker connection/request/throttle/
  disconnect metadata without exporting franz-go hooks or making observation
  part of Kafka correctness. The root module's `adapters/golog` package maps
  those stable observations to `log/slog`; the independently versioned
  `adapters/gotelemetry` module maps them to OpenTelemetry and separately owns
  an explicit bounded W3C record-header propagation policy. Neither adapter
  changes Kafka delivery or settlement.

franz-go remains an implementation detail. The root module exposes owned TLS,
mTLS, PLAIN, SCRAM, and OAUTHBEARER policy contracts; optional vendor
authentication belongs in independently versioned adapters. Topic lifecycle,
ACLs, replication, ISR, retention, quotas, and destructive group operations
belong to infrastructure automation and audited operator procedures.
