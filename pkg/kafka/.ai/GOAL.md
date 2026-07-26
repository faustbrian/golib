# Goal: Production Kafka Client Policy for Go

## Objective

Build `kafka` as a serious open source Apache Kafka client policy library for
Go services. It MUST provide safe, explicit, bounded producer, consumer,
Kafka-transaction, replay, inspection, security, lifecycle, and observability
building blocks on top of `franz-go` without attempting to reimplement the
Kafka protocol.

The package exists because using a capable low-level client consistently across
many services requires stable operational policy. It MUST add meaningful safety
and interoperability value rather than becoming a shallow rename of
`franz-go` or an abstraction that hides Kafka's semantics.

The current pre-v1 implementation and API are an unproven draft. They MUST be
audited against this goal from first principles. Existing code, tests,
documentation, or coverage MUST NOT be treated as evidence that the required
scope or behavior is correct.

## Product Position

`kafka` MUST be:

- Kafka-specific and honest about topics, partitions, keys, offsets, consumer
  groups, rebalances, retention, transactions, and broker acknowledgements;
- safe by default for durable production workloads;
- bounded in memory, concurrency, retries, batches, polling, shutdown, and
  diagnostic output;
- explicit about delivery, ordering, duplication, and failure ambiguity;
- usable independently of event sourcing, queues, outboxes, databases,
  schemas, service frameworks, and telemetry vendors;
- composable through narrow interfaces and optional adapters;
- suitable for Apache Kafka and explicitly tested compatible services such as
  Amazon MSK; and
- operable under rolling deployments, broker failures, consumer rebalances,
  credential rotation, and replay.

The package MUST NOT describe Kafka as a generic job queue. Applications that
only need work distribution without replay, partition ordering, or independent
consumer histories SHOULD evaluate `queue` instead.

## Authoritative Sources

Implementation and documentation MUST be checked against current primary
sources, including:

- Apache Kafka documentation: https://kafka.apache.org/documentation/
- Apache Kafka protocol and configuration references;
- relevant Kafka Improvement Proposals for implemented behavior;
- `franz-go` source and documentation:
  https://github.com/twmb/franz-go
- Amazon MSK documentation for claimed compatibility:
  https://docs.aws.amazon.com/msk/
- OpenTelemetry Kafka messaging semantic conventions:
  https://opentelemetry.io/docs/specs/semconv/messaging/kafka/
- Go language, memory-model, `context`, TLS, fuzzing, race-detector, and module
  documentation; and
- this repository's `AGENTS.md`, module policies, downstream adapter contracts,
  and release gates.

At execution time, record the exact Kafka, compatible broker, `franz-go`, Go,
container image, authentication adapter, and test-tool versions. Version
`franz-go` v1.21.5 is the current baseline, not a permanent ceiling; verify the
latest stable release before implementation or hardening.

## Scope And Dependency Boundary

The root module MUST own stable policy-level types and lifecycle behavior. It
MUST NOT expose `kgo.Client`, `kgo.Record`, `kgo.Opt`, `kadm` response types, or
other `franz-go` implementation details through ordinary public APIs.

Public APIs MAY model real Kafka concepts directly. Avoiding vendor types does
not justify inventing generic names that erase Kafka behavior.

The root module SHOULD contain:

- connection, security, producer, consumer, transaction, replay, inspection,
  health, error, hook, and limit contracts;
- a `franz-go`-backed implementation behind those contracts;
- test doubles and public conformance suites only where they test an actual
  consumer-facing seam; and
- no dependency on application business logic or another `golib` module.

Optional dependency-heavy integrations MUST use independently versioned nested
modules or downstream adapters. The package MUST NOT require AWS SDK,
OpenTelemetry, schema-registry, event-sourcing, outbox, or service dependencies
for a basic producer or consumer.

An unrestricted raw-option escape hatch MUST NOT bypass safety invariants. If an
advanced Kafka capability is absent, add and review an explicit policy surface
or use `franz-go` directly rather than accepting arbitrary `kgo.Opt` values.

## Configuration Model

Configuration MUST be explicit, immutable after construction, independently
validatable, safe to log in redacted form, and divided by concern.

Support and document:

- ordered seed brokers and duplicate handling;
- client identity and optional instance identity;
- dial, request, delivery, transaction, handler, commit, rebalance, and
  shutdown deadlines;
- topic and consumer-group allowlists or resolvers;
- record, header, batch, fetch, buffer, retry, and concurrency limits;
- Kafka protocol-version negotiation and any minimum-version override;
- rack awareness and static membership where supported;
- partitioning and offset-reset policies;
- TLS, mutual TLS, SASL, and credential-provider selection;
- compression and batching policy;
- observability hooks; and
- safe defaults with a documented rationale.

Constructors MUST fail before allocating durable resources when configuration
is invalid. Partial initialization MUST close every created resource.

Security material, callback functions, byte slices, maps, and slices MUST be
defensively copied or have explicit ownership and lifetime rules. Configuration
validation MUST reject whitespace-only identifiers, duplicates, invalid UTF-8
where Kafka/application policy requires text, impossible timeout relationships,
overflow, incompatible options, and unbounded values.

## Record Model

Provide stable producer and consumed-record models containing the applicable:

- topic;
- partition;
- offset;
- key;
- value;
- ordered headers;
- event timestamp and timestamp type;
- leader epoch where available; and
- delivery or fetch metadata needed for diagnostics and settlement.

Define byte ownership before, during, and after publish and handler calls.
Callers MUST be able to request safe retained copies without accidental aliasing
or use-after-poll behavior.

Validation MUST bound:

- topic length and naming policy;
- key and payload bytes;
- header count, key size, individual value size, and aggregate header bytes;
- batch count and aggregate encoded size; and
- metadata copied into memory, errors, logs, traces, and metrics.

The package transports bytes and Kafka metadata. It MUST NOT own application
event envelopes, domain schemas, JSON conventions, protobuf messages, or
business routing.

## Producer

Provide synchronous single-record, synchronous batch, and bounded asynchronous
production. Each mode MUST expose per-record delivery results containing topic,
partition, offset, timestamp, and a classifiable error.

Safe producer defaults MUST include:

- idempotent production enabled;
- all in-sync replica acknowledgements;
- ordering-preserving in-flight request settings;
- bounded delivery time;
- bounded client buffering;
- bounded batch bytes and linger;
- reviewed compression preferences;
- no silent record drop on cancellation or shutdown; and
- no application-level retry that defeats producer idempotence without an
  explicit duplicate policy.

The producer API MUST define:

- whether cancellation stops waiting or can stop broker delivery;
- how ambiguous delivery is represented;
- retryable, permanent, authorization, fencing, oversized-record, timeout, and
  shutdown error categories;
- partition selection for keyed, unkeyed, and explicitly partitioned records;
- ordering scope and behavior across retries;
- callback ownership and panic handling;
- flush, abort, drain, and close semantics;
- concurrent method safety;
- behavior after close or fatal producer state; and
- how broker throttling and partial batch failures are surfaced.

Applications MUST be able to require non-empty keys when partition ordering is
an invariant. Unkeyed production MUST be an explicit policy, not an accidental
zero-value behavior.

## Consumer Groups

Provide a bounded consumer-group runner with explicit at-least-once semantics.
Automatic offset commits MUST be disabled for durable handlers.

The consumer MUST support:

- per-record and batch handler contracts with clearly different settlement;
- manual settlement of only contiguous successfully processed offsets per
  partition;
- cooperative balancing by default where compatible;
- explicit eager balancing when required;
- optional static membership and rack-aware assignment;
- earliest and latest reset policies plus explicit rejection of an unset
  policy;
- bounded polling, fetch bytes, per-partition fetch bytes, buffered records,
  handler concurrency, and handler duration;
- sequential processing within a partition;
- optional bounded parallelism across independent partitions;
- pause, resume, drain, and graceful shutdown;
- retry and poison-record hooks without hidden infinite loops;
- dead-letter publication through an injected narrow contract;
- lag and lifecycle observation; and
- fatal versus recoverable group error classification.

Handler success MUST precede offset settlement. Cancellation, handler error,
panic, timeout, process death, commit timeout, or rebalance ambiguity MUST have
documented redelivery consequences.

The consumer MUST NOT commit later records past a failed record in the same
partition. A failure in one partition SHOULD NOT force successful independent
partitions to remain permanently uncommitted when the API can preserve correct
contiguous settlement.

## Rebalance Safety

Rebalances are a primary correctness boundary, not an incidental callback.
Define and test:

- assignment and revocation ownership;
- when fetching stops;
- how in-flight handlers drain or are cancelled;
- which offsets may be committed during revocation;
- generation/member fencing;
- cooperative incremental assignment;
- eager assignment;
- session timeout, heartbeat interval, rebalance timeout, and maximum poll
  relationship;
- slow handlers and stalled partitions;
- repeated or overlapping lifecycle callbacks;
- shutdown during assignment, handling, commit, or revocation; and
- panic behavior in application and observer callbacks.

A consumer generation MUST NOT acknowledge a record after it loses ownership of
the partition. A rebalance callback MUST NOT block indefinitely.

## Retry And Dead-Letter Policy

Kafka retains records; it does not provide queue-style nack or per-record
visibility timeouts. Retry design MUST preserve that reality.

The package MUST support explicit strategies rather than one hidden default:

- stop and return the error without committing;
- bounded in-process retry with cancellation-aware backoff;
- publish to versioned retry topics;
- publish to a dead-letter topic after a reviewed terminal decision; and
- delegate failure handling to the application.

Retry and dead-letter publication MUST preserve original topic, partition,
offset, key, timestamp, headers, error classification, attempts, and correlation
metadata without disclosing sensitive payloads.

Publishing a dead-letter record and committing the source offset are separate
effects unless performed in one Kafka transaction. The package MUST document
and expose the duplicate/loss window for every strategy.

## Kafka Transactions

Kafka transactions MUST be supported as a Kafka-scoped capability, not as a
general database transaction abstraction.

Support:

- unique transactional IDs and producer fencing;
- bounded begin, produce, commit, abort, and close behavior;
- callback lifetime enforcement;
- panic and cancellation handling;
- abortable versus fatal transaction errors;
- unknown commit outcomes;
- read-committed consumer mode;
- consume-transform-produce with source offsets sent to the transaction;
- transaction ownership under concurrency; and
- recovery after process termination or a fenced producer.

Exactly-once claims MUST be restricted to the proven Kafka read-process-write
boundary with compatible isolation and transaction settings. Kafka producer
idempotence or transactions MUST NOT be described as atomic with PostgreSQL,
HTTP calls, object storage, email, webhooks, or any other external system.

## Replay

Provide safe replay that directly reads explicit topic partitions without
joining, committing, resetting, or deleting consumer-group offsets.

Replay MUST support:

- inclusive start and exclusive end offsets;
- ranges planned from explicit offsets or broker timestamps;
- multiple bounded partitions;
- stable ascending order within each partition;
- optional bounded parallelism across partitions;
- dry-run planning and inspection;
- resumable progress supplied by an explicit external checkpoint;
- retention gaps, compacted records, offset truncation, and out-of-range
  detection;
- cancellation and graceful stop;
- replay metadata propagation; and
- exact reporting of processed, skipped, failed, and incomplete ranges.

Replay MUST fail closed when an exact requested range cannot be satisfied. It
MUST NOT imply global order across partitions or exactly-once side effects.
Side-effect-capable replay consumers require explicit application opt-in.

## Read-Only Inspection And Health

Provide bounded, read-only inspection needed by applications and operations:

- broker connectivity;
- cluster identity and controller visibility where available;
- topic existence and partition metadata;
- leader, replica, ISR, and offline-replica state;
- beginning and end offsets;
- consumer-group state, assignments, committed offsets, and lag;
- topic configuration needed to evaluate durability, including replication and
  `min.insync.replicas`, where authorized; and
- transaction or producer health signals that can be observed safely.

Separate liveness, readiness, dependency health, and diagnostic inspection.
Readiness MUST be configurable so a temporary broker outage does not
automatically cause an unsafe restart storm.

Topic creation, deletion, partition expansion, ACL mutation, group deletion,
offset mutation, and broker configuration remain infrastructure/operator
responsibilities. Test utilities MAY provision isolated fixtures.

## Security And Authentication

Support:

- verified TLS with TLS 1.2 minimum and system or caller-provided roots;
- mutual TLS;
- SASL/PLAIN only over verified TLS;
- SCRAM-SHA-256 and SCRAM-SHA-512;
- OAUTHBEARER through a bounded refreshing token provider;
- optional Amazon MSK IAM authentication in an independently versioned adapter;
- credential refresh and rotation without process-wide mutable globals; and
- explicit development-only plaintext configuration that cannot be mistaken
  for the production default.

The core MUST NOT require the AWS SDK. The MSK IAM adapter SHOULD use AWS's
supported Go signer and normal credential chain while preserving cancellation,
expiry, refresh, and redaction.

Passwords, tokens, certificates, private keys, broker URLs containing secrets,
record values, and sensitive headers MUST NOT appear in errors, logs, traces,
metrics, test output, panic output, fixtures, or generated artifacts.

## Hooks, Logging, Metrics, And Tracing

Provide stable, synchronous-by-contract hooks or observers for material client
events without exposing mutable `franz-go` internals. Hook execution order,
panic behavior, blocking limits, reentrancy, and data ownership MUST be defined.

Expose enough information for:

- connection and authentication state;
- broker request latency, throttling, and errors;
- produce duration, batch size, record size, retries, and delivery outcome;
- consume fetch size, processing duration, commit outcome, and redelivery;
- consumer-group assignment, revocation, and rebalance duration;
- lag and paused partitions;
- transaction begin, abort, commit, fencing, and unknown outcome;
- replay progress and gaps; and
- shutdown and resource state.

Provide optional OpenTelemetry integration in a nested adapter. Follow the
currently selected messaging semantic-convention version and document its
stability policy. Topic and consumer-group cardinality MUST be controlled;
keys, payloads, credentials, and arbitrary headers MUST NOT become telemetry
attributes by default.

The package SHOULD integrate with `log/slog` through an adapter or narrow
logger interface without making logging required for correctness.

## Downstream Integrations

The Kafka module MUST remain independent while enabling these owned adapters:

- `outbox/adapters/gokafka` for durable outbox relay publication;
- `event-sourcing/adapters/gokafka` for event-message encoding, direct dispatch,
  consumption, and replay integration;
- `kafka/adapters/gotelemetry` for optional OpenTelemetry instrumentation;
- `kafka/adapters/mskiam` for optional Amazon MSK IAM authentication; and
- service readiness/health composition through public Kafka contracts.

Adapter-specific envelopes, schemas, topic routing, idempotency, and business
semantics belong to the adapter or application, not this package.

Any breaking pre-v1 correction MUST update all owned reverse dependencies in
the same coherent change. Passing Kafka's own tests while leaving outbox or
event-sourcing adapters semantically incorrect is not completion.

## Compatibility Matrix

Before declaring support, publish and test a matrix containing:

- minimum and current supported Go versions;
- minimum and current Apache Kafka broker versions;
- KRaft deployment mode;
- `franz-go` and `kadm` versions;
- Amazon MSK provisioned and/or serverless modes actually supported;
- Kafka-compatible services such as Redpanda or Confluent only when directly
  tested;
- TLS, mTLS, PLAIN, SCRAM, OAUTHBEARER, and MSK IAM combinations actually
  tested;
- producer, consumer-group, transaction, replay, and inspection capabilities;
  and
- operating systems and architectures covered by CI.

Protocol compatibility inferred from `franz-go` is not sufficient evidence for
an operational support claim. Untested compatible brokers MUST be described as
unverified, not supported.

## Documentation Deliverables

Documentation MUST let a new user adopt and operate the package without reading
its implementation:

- README with decision guide and five-minute producer/consumer quickstart;
- complete public API reference and package map;
- architecture, dependency, and ownership boundaries;
- configuration reference with defaults, validation, and safe examples;
- producer delivery, ordering, batching, retry, and shutdown guarantees;
- consumer processing, commit, redelivery, rebalance, and backpressure guide;
- retry-topic and dead-letter patterns with failure windows;
- Kafka transaction and Kafka-scoped exactly-once guidance;
- replay planning, safety, recovery, and side-effect guidance;
- TLS, mTLS, SASL, OAUTHBEARER, MSK IAM, rotation, and least-privilege guide;
- topic keying, partitioning, replication, ISR, retention, and compaction guide;
- AWS MSK and ECS deployment/credential guidance where supported;
- observability, dashboards, alerts, SLOs, and high-cardinality cautions;
- capacity planning and tuning;
- rolling deployment, graceful shutdown, incident recovery, and disaster
  recovery runbooks;
- compatibility, migration, deprecation, and upgrade documentation;
- troubleshooting, FAQ, glossary, security notes, and changelog; and
- runnable examples for synchronous, batch, asynchronous, consumer-group,
  transactional, replay, outbox, and event-sourcing workflows.

Documentation MUST distinguish Kafka guarantees, `franz-go` guarantees,
package policy, broker configuration, deployment policy, adapter behavior, and
application responsibility.

## Testing And Quality Standard

Meaningful exact 100% production statement coverage is mandatory. Every viable
mutant MUST be killed, with exact 100% mutation efficacy and mutant coverage.

Required verification includes:

- unit tests for every state transition, error, limit, and ownership rule;
- public conformance tests for producer, consumer, transaction, replay,
  inspector, auth-provider, and hook seams;
- real multi-broker Kafka integration tests;
- broker restart, leader election, ISR loss, partition, latency, throttling,
  retention, and authentication fault injection;
- consumer-group join, assignment, rebalance, fencing, commit, and restart
  tests across multiple processes;
- transaction fencing, abort, timeout, unknown outcome, and
  consume-transform-produce tests;
- replay gap, compaction, truncation, cancellation, and resume tests;
- race, stress, and goroutine/connection/timer leak tests;
- fuzzing for configuration, records, headers, error mapping, auth callbacks,
  replay ranges, and broker-controlled metadata;
- deterministic clocks and fault seams where needed;
- clean-consumer tests proving the published module works outside the monorepo;
- downstream adapter integration and compatibility tests; and
- reproducible allocation-reporting benchmarks.

Mocks MUST NOT substitute for broker evidence. Sleep-based tests MUST NOT
substitute for observable synchronization. Coverage MUST assert meaningful
delivery, settlement, ordering, cleanup, and failure outcomes rather than
merely executing lines.

## Performance And Benchmarking

Benchmark equivalent behavior against:

- raw `franz-go` as the overhead floor;
- `segmentio/kafka-go`;
- `IBM/sarama`; and
- the previous released `kafka` version once one exists.

At execution time, pin the latest reviewed versions. Current reference versions
are `franz-go` v1.21.5, `kafka-go` v0.4.51, and `sarama` v1.60.0.

Measure:

- synchronous single-record production;
- bounded asynchronous and batch production;
- keyed and unkeyed records;
- payload and batch-size distributions;
- compression modes;
- one and many partitions;
- consumer record and batch handling;
- sequential and cross-partition parallel handling;
- offset commit strategies and rebalance cost;
- transactions and consume-transform-produce;
- replay throughput and inspection/lag queries;
- steady-state and reconnect allocations;
- idle CPU, memory, goroutines, and connections; and
- package policy overhead separately from broker/network cost.

Comparisons MUST use equivalent acknowledgement, idempotence, durability,
compression, batching, partitioning, payload, TLS, and commit settings. Publish
hardware, network, broker topology/configuration, Go/client versions, corpus,
sample counts, distributions, confidence intervals, allocations, profiles, and
raw results. Do not claim superiority from non-equivalent defaults.

## Repository And Release Requirements

- Use the repository's current minimum Go version and module layout.
- Keep `franz-go` and optional dependencies current, reviewed, and pinned.
- Register every package, nested module, adapter, service, fixture, goal, and
  provenance source in repository manifests.
- Use only the root GitHub Actions workflow.
- Make every CI gate runnable locally through repository tooling.
- Enforce format, tidy, safety, vet, strict lint, Staticcheck, tests, race,
  exact coverage, exact mutation, fuzz smoke, vulnerability, secrets, licenses,
  SBOM, provenance, docs, API compatibility, conformance, interoperability,
  integration, clean-consumer, and benchmark gates.
- NilAway remains advisory but visible and no-regression.
- Pin broker/container images by immutable digest and verify actual versions at
  runtime.
- Maintain strict `CHANGELOG.md` entries for every user-visible change.
- Treat delivery semantics, defaults, record ownership, error identity,
  ordering, offset settlement, transaction behavior, replay, metrics, and
  configuration as SemVer-sensitive contracts.
- Do not release with skipped broker, auth, adapter, mutation, race, or
  compatibility evidence.

## Execution Plan

1. Inventory the current API, implementation, dependencies, downstream
   adapters, documentation, tests, and actual broker evidence.
2. Publish architecture, delivery, failure, ownership, compatibility, and
   security decision matrices before preserving or replacing the draft API.
3. Implement or correct bounded configuration, producer, record, lifecycle,
   security, error, and hook foundations.
4. Implement or correct consumer groups, rebalances, settlement, retry, dead
   letters, transactions, replay, inspection, and health.
5. Implement optional telemetry and MSK IAM adapters and align owned outbox and
   event-sourcing integrations.
6. Complete real broker, multi-process, chaos, auth, race, fuzz, mutation,
   compatibility, and performance hardening.
7. Complete documentation, migration guidance, operational runbooks, final API
   review, and release evidence.

## Non-Goals

- no Kafka broker, protocol, or cluster implementation;
- no generic queue API;
- no event-sourcing framework or event envelope;
- no transactional outbox store or relay;
- no application schema or business routing;
- no required schema registry, JSON, protobuf, or Avro convention;
- no service framework, dependency-injection container, or global client;
- no automatic topic, partition, ACL, group, or broker mutation in production;
- no cross-system transaction or end-to-end exactly-once claim;
- no hidden infinite retry or unbounded buffering;
- no unrestricted `franz-go` option passthrough; and
- no support claim inherited solely from an upstream client's feature list.

## Acceptance Criteria

The package is ready only when:

- its API adds clear safety and operational policy beyond raw `franz-go`;
- producer delivery, consumer settlement, rebalance, transaction, replay, and
  shutdown semantics are explicit and proven;
- exact meaningful coverage and mutation results are 100%;
- real multi-broker, failure, security, and compatibility matrices pass;
- Kafka-to-Kafka transaction claims are correctly scoped and no external
  atomicity is implied;
- every owned reverse-dependent adapter is aligned and verified;
- resource use is bounded and race/leak evidence is clean;
- documentation supports adoption, operation, failure recovery, and migration;
  and
- every affected repository and release gate passes without skips or weakened
  thresholds.
