# Goal: Pragmatic Event Sourcing for Go

## Objective

Build `event-sourcing` as a serious open source Go library for event-sourced
domain models. Its conceptual model and developer experience MUST be inspired
by EventSauce 3.9.1, especially its pragmatic, composable architecture:

- https://eventsauce.io/docs/
- https://eventsauce.io/docs/architecture/
- https://eventsauce.io/docs/lifecycle/

The package MUST provide the EventSauce capabilities that are useful and valid
in Go while presenting an idiomatic Go API. It MUST NOT mechanically translate
PHP syntax, inheritance, reflection conventions, framework integration, or
object-hydration magic into Go.

The result should feel familiar to an experienced EventSauce user and natural
to an experienced Go user.

## Product Position

`event-sourcing` MUST be:

- focused on event sourcing rather than requiring CQRS;
- usable for one aggregate or bounded context without an application rewrite;
- pragmatic, explicit, composable, and easy to replace at every boundary;
- based on small contracts instead of inheritance or framework machinery;
- independent from queues, outboxes, service containers, command buses, query
  buses, web frameworks, and application business logic;
- safe for long-lived event histories and schema evolution;
- first-class with PostgreSQL while keeping the core storage-independent;
- suitable for both embedded application use and reusable infrastructure; and
- accompanied by excellent scenario-testing and operational tooling.

The package MUST NOT claim that event sourcing is simple or appropriate for
every domain. Adoption guidance MUST explain when conventional state
persistence is the better choice.

## Compatibility Definition

Feature compatibility means that each supported EventSauce user-facing
capability has one of:

1. an equivalent idiomatic Go capability;
2. an intentionally different Go design with equivalent outcomes and a
   documented migration path; or
3. a documented exclusion proving that the original capability depends on PHP
   behavior that should not be reproduced in Go.

Before implementation, verify the latest stable EventSauce release and create a
versioned feature matrix covering every section of its documentation. Version
3.9.1, tag commit `33ea9b97ec3ac56991caad03b791fee418a43e41`, is the minimum
baseline. The inventory MUST include:

- aggregate roots and aggregate identifiers;
- aggregate repositories;
- messages, headers, event payloads, and message identifiers;
- message repositories and storage schemas;
- message dispatchers, chains, and consumers;
- direct and durable asynchronous transport adapters;
- message decorators;
- payload and message serialization;
- event-name mapping and anti-corruption;
- clocks and deterministic time;
- synchronous dispatch;
- aggregate lifecycle and reconstitution;
- optimistic concurrency and sequential integrity;
- scenario-based aggregate testing;
- projections and read models;
- process managers;
- message replay;
- upcasting;
- snapshotting and snapshot versioning;
- transactional outbox integration;
- custom repositories and dispatchers;
- aggregate roots containing child aggregates;
- UUID and identifier encoding concerns; and
- code generation.

No capability may silently disappear. The matrix MUST identify the pinned
EventSauce source tag and page, expected behavior, Go design, tests,
documentation, and status.

Compatibility is conceptual and behavioral, not source or wire compatibility
unless a format is explicitly specified. EventSauce source MUST NOT be copied
without a license review and required attribution.

## Architectural Core

Preserve EventSauce's three replaceable responsibilities:

1. an aggregate repository that loads and persists aggregate roots;
2. an event/message store that appends and reads immutable event messages; and
3. a dispatcher that delivers persisted messages to consumers.

The Go API MAY use names that are clearer in Go, but the responsibilities MUST
remain separately replaceable. Interfaces MUST be small, consumer-oriented,
and accept `context.Context` for I/O. Constructors MUST validate dependencies
and options rather than deferring invalid state to runtime.

The core MUST use explicit composition. It MUST NOT use:

- global registries;
- hidden package initialization;
- service locators or dependency-injection containers;
- reflection-driven event-handler method discovery;
- mandatory code generation;
- goroutines with implicit ownership;
- hidden transactions; or
- framework lifecycle hooks.

Generics MAY improve type safety where they make the API clearer. They MUST NOT
produce an abstraction that is harder to use, mock, inspect, or extend than
small ordinary Go interfaces.

## Aggregate Model

The package MUST support:

- application-defined aggregate identifiers;
- aggregate creation and reconstitution from an ordered event stream;
- recording an event and applying it immediately;
- tracking committed and uncommitted versions;
- releasing pending events exactly once per successful persistence attempt;
- rejecting stale expected versions through optimistic concurrency;
- aggregates that record zero, one, or many events per command;
- child entities or child aggregates whose events retain root identity;
- deterministic replay without external side effects; and
- explicit errors for missing streams, corrupt histories, unknown event types,
  incompatible versions, and concurrency conflicts.

The library MAY provide an embeddable helper for version and pending-event
bookkeeping, but application aggregates MUST own their invariants and event
application. Embedding MUST NOT become inheritance disguised as Go.

Applying a known historical event MUST be deterministic and side-effect free.
Unknown, malformed, or incompatible stored events MUST fail reconstitution
cleanly rather than panic, partially construct an aggregate, or skip history.

The lifecycle MUST remain recognizable to EventSauce users:

1. load or create the aggregate;
2. perform domain behavior;
3. record and immediately apply events;
4. append the new messages with an expected stream version; and
5. dispatch persisted messages through an explicitly selected strategy.

## Event Message Model

Provide a stable immutable-by-contract envelope containing at least:

- message ID;
- aggregate root ID and aggregate type;
- stream version;
- event name and event schema version;
- event payload;
- metadata/headers;
- recorded-at timestamp;
- correlation and causation identifiers;
- tenant or partition metadata without prescribing multitenancy; and
- optional global position supplied by capable stores.

The API MUST define:

- which fields are application supplied and which are generated;
- canonical validation and size limits;
- defensive copying or ownership rules for bytes, maps, and slices;
- stable ordering and encoding where observable;
- reserved metadata keys and collision behavior;
- zero-value behavior;
- equality and comparison semantics used by tests; and
- redaction rules for diagnostics.

Event names and schema versions MUST be explicit stable data. Persisted event
identity MUST NOT depend on Go package paths, concrete type names, symbol
renames, reflection output, or compiler details.

## Serialization And Evolution

Provide separate payload-codec and message-codec contracts. The first-party
JSON codec MUST use explicit event registration and deterministic mappings.
Applications MUST be able to supply protobuf, MessagePack, or other codecs
without changing aggregate or store contracts.

Serialization MUST support:

- stable event-name registration;
- duplicate and ambiguous registration rejection;
- aliases for renamed events;
- unknown-event diagnostics;
- schema-version metadata;
- exact integer and time handling;
- optional strict unknown-field behavior;
- bounded payloads and metadata; and
- round-trip conformance tests.

Provide composable upcasters that can:

- transform metadata and payloads;
- rename event types;
- advance schema versions;
- drop an obsolete event only through an explicit reviewed policy;
- split one stored event into multiple logical events when required; and
- run as a deterministic ordered chain.

Upcasting MUST occur at the read boundary and MUST NOT silently rewrite stored
history. Every chain MUST prove termination, monotonic version progress,
determinism, and compatibility with snapshots and replay.

## Event Store

The core event-store contract MUST define:

- atomic append of a non-empty ordered batch to one stream;
- expected-version semantics, including new, existing, exact, and optional any
  version modes;
- stream reads by aggregate ID and version range;
- empty, missing, deleted, archived, and corrupt stream behavior;
- stable event order;
- cancellation and partial-read behavior;
- duplicate message-ID handling;
- optional global ordered reads for projections and replay;
- capability discovery where stores cannot provide every feature; and
- error categories that preserve causes and support `errors.Is`/`errors.As`.

Ship an in-memory implementation for tests and development. It MUST obey the
same concurrency and ownership contract as durable stores rather than being a
loose fake.

### PostgreSQL

Ship a first-class PostgreSQL implementation using `pgx`. Isolate it in an
independently versioned nested module when necessary so installing the core
module does not add `pgx` to the consumer's module graph. It MUST provide:

- documented schemas and indexes;
- caller-owned transaction support;
- atomic batch append with optimistic concurrency;
- globally ordered positions suitable for projection checkpoints;
- deterministic ordering under concurrent writers;
- clean-install and versioned migration artifacts compatible with
  `migrations`;
- bounded streaming reads;
- statement, lock, and transaction timeout guidance;
- partitioning, retention, archive, backup, and restore guidance; and
- PostgreSQL-version integration tests using real databases.

The package MUST NOT expose Goose or any migration engine through its public
application API.

## Dispatch And Consumers

Provide small synchronous dispatcher and consumer contracts plus explicit
composition helpers comparable to EventSauce's dispatcher chain.

Dispatch behavior MUST define:

- ordering;
- stop-on-error versus continue behavior;
- panic handling;
- cancellation;
- partial success;
- empty batches;
- duplicate consumers;
- reentrant dispatch;
- message filtering; and
- whether dispatch occurs before or after the persistence transaction commits.

The safe default MUST NOT dispatch externally before durable persistence. The
package MUST NOT claim exactly-once delivery.

Asynchronous transport belongs to `queue` adapters. Durable asynchronous
publication belongs to optional outbox integration. The core MUST remain useful
without either.

### Adapter Support

The first release MUST define and document this adapter matrix:

- synchronous in-process dispatch in the core;
- independently versioned `gokafka` support for Apache Kafka;
- independently versioned `goqueue` support for compatible `queue` backends;
- independently versioned `gooutbox` support for atomic event/outbox
  persistence;
- independently versioned `gotelemetry` instrumentation; and
- first-class PostgreSQL storage in its own nested module.

Kafka MUST be a dedicated adapter rather than merely passing through the
smallest generic queue interface. Kafka topics, partitions, record keys,
offsets, consumer groups, rebalances, retention, replay, producer
acknowledgements, idempotent production, and transactions are material
semantics and MUST remain observable.

Use `github.com/twmb/franz-go` as the initial Kafka client unless the execution
phase documents a better maintained choice. The Kafka adapter MUST support:

- mapping event messages to stable Kafka records and back;
- configurable topic resolution with bounded allowlists;
- aggregate-root ID as the safe default record key, preserving per-aggregate
  order within one topic;
- event name, message ID, aggregate type/version, event schema version,
  correlation ID, causation ID, recorded time, content type, and replay marker
  propagation;
- synchronous acknowledgement for direct dispatch;
- explicit `acks`, idempotent-producer, retry, timeout, compression, and batch
  policy;
- manual consumer offset settlement only after successful handling;
- consumer groups, cooperative rebalancing, graceful draining, and bounded
  polling;
- duplicate delivery, poison event, retry, dead-letter, and replay policy;
- TLS and SASL authentication without credential disclosure;
- OpenTelemetry propagation through the optional telemetry adapter; and
- real Kafka compatibility tests with pinned broker and client versions.

Direct PostgreSQL event-store to Kafka dispatch is not atomic. The recommended
durable production composition MUST be:

1. append event messages and outbox records in one PostgreSQL transaction
   through `gooutbox`;
2. publish committed outbox records through a Kafka publisher adapter owned by
   `outbox`; and
3. consume Kafka records idempotently with explicit offset settlement.

The event-sourcing goal MUST coordinate the required
`outbox/adapters/kafka` contract without moving generic Kafka publication into
the event-sourcing core. Kafka producer transactions and idempotence MUST NOT be
described as atomicity across PostgreSQL and Kafka or as end-to-end exactly
once.

The `goqueue` adapter MAY expose Kafka only after `queue` has a proven Kafka
backend and can preserve the required event-bus semantics. It MUST NOT erase
Kafka-specific capabilities or delivery guarantees.

## Outbox Independence

`event-sourcing` and `outbox` MUST remain independently usable and releasable.

- The event-sourcing core MUST NOT import `outbox`.
- The outbox core MUST NOT import `event-sourcing`.
- Event storage MUST work without an outbox.
- Outbox publication MUST work for non-event-sourced applications.
- Replay MUST NOT enqueue outbox records unless explicitly requested by a
  separately named operation.

Provide an optional adapter, preferably an independently versioned nested module
such as `event-sourcing/adapters/gooutbox`, that depends on both public
contracts. It MUST persist event messages and outbox envelopes atomically using
the same caller-owned PostgreSQL transaction.

The adapter MUST document crash behavior, at-least-once delivery, duplicate
possibility, serialization ownership, transaction ownership, commit ambiguity,
and recovery. Neither package may depend on the other's internal schema or
unexported behavior.

## Snapshots

Snapshotting MUST be optional and replaceable. Support:

- aggregate ID and type;
- aggregate version;
- snapshot schema version;
- encoded state and metadata;
- creation after complete reconstitution;
- restoration followed by events strictly after the snapshot version;
- stale, corrupt, incompatible, and missing snapshot fallback policy;
- atomicity expectations between event append and snapshot update; and
- background or threshold-based snapshot refresh without hidden goroutines.

Snapshots are derived acceleration data, never authoritative history.
Applications MUST be able to delete and rebuild them. Snapshot encoding and
version migrations MUST be explicit.

## Projections, Replay, And Process Managers

Provide reusable contracts and helpers without forcing CQRS:

- consumers for projections and read models;
- ordered global event iteration where the store supports it;
- durable projection checkpoints;
- atomic projection update plus checkpoint where supported;
- reset, rebuild, resume, pause, and status operations;
- bounded batching and backpressure;
- idempotent duplicate handling;
- poison-event policy;
- process managers that react to events and explicitly emit planned commands
  or messages; and
- replay filters by stream, aggregate type, event type, position, and time.

Live delivery and replay MUST be distinguishable in the API and metadata.
Replaying projections MUST NOT accidentally execute process managers, external
side effects, queue publication, or outbox insertion.

Projection and process-manager persistence MAY use optional packages. Their
concurrency, checkpoint, retry, and ownership semantics MUST remain explicit.

## Testing Experience

Ship a dedicated `eventtest` package inspired by EventSauce's scenario tests.
It MUST make these workflows concise:

- given no history, when behavior runs, then events are recorded;
- given historical events, when behavior runs, then events are recorded;
- then no event;
- then an expected error or panic policy;
- event payload and metadata assertions;
- fixed aggregate identifiers and clocks;
- expected version assertions;
- reconstitution-only assertions;
- serializer/upcaster round trips;
- snapshot equivalence;
- projection and process-manager scenarios; and
- reusable store and dispatcher conformance suites.

The API SHOULD use ordinary `testing.TB`, functions, and values. It MUST NOT
introduce a custom test runner, global state, assertion DSL that obscures
failures, or mandatory third-party assertion framework.

## Code Generation

Code generation MAY be provided for repetitive event declarations, codecs, or
registries when it materially improves safety. It MUST:

- use `go generate` or an explicit command;
- generate deterministic, formatted, reviewable Go;
- never be required for handwritten implementations;
- reject unknown or ambiguous schema input;
- preserve stable event names and versions;
- record generator version and source provenance; and
- have golden, compile, and stale-generation checks.

Do not reproduce PHP object-hydration conventions when ordinary Go structs,
explicit constructors, and generated codecs are clearer.

## Proposed Package Structure

Validate the structure through API design examples before implementation.
Prefer a shape similar to:

- root package: envelopes, aggregate lifecycle, repository, store, dispatcher,
  codecs, decorators, errors, and core options;
- `memory`: conformant in-memory event and snapshot stores;
- `projection`: projectors, checkpoints, runners, and replay control;
- `processmanager`: explicit process-manager contracts and runners;
- `snapshot`: snapshot repository composition;
- `eventtest`: scenarios and reusable conformance suites;
- independently versioned `postgres`: PostgreSQL event, snapshot, and
  checkpoint stores;
- independently versioned `codegen` and `cmd/golib-event-sourcing`: optional
  generation tooling; and
- independently versioned adapter modules for `gooutbox`, `goqueue`,
  `gokafka`, and `gotelemetry`.

Avoid package fragmentation when a subpackage does not isolate a dependency,
capability, or coherent public concept.

The public module path is
`github.com/faustbrian/golib/pkg/event-sourcing`; its idiomatic Go package
identifier SHOULD be `eventsourcing`.

## Go API Requirements

- All I/O MUST accept `context.Context`.
- APIs MUST return explicit errors and support standard error inspection.
- Caller and callee ownership of transactions, iterators, bytes, maps,
  goroutines, and shutdown MUST be documented.
- Iteration MUST be bounded and cancellation-aware.
- Options MUST be validated and immutable after construction.
- Clocks and ID generators MUST be injectable without global replacement.
- Zero values MUST be intentionally useful or explicitly invalid.
- Panics MUST be limited to documented programmer errors; stored or external
  input MUST never cause a panic.
- Reflection MAY be used only behind a measured, optional convenience layer.
- Public contracts MUST not expose internal PostgreSQL rows or framework types.
- No production `unsafe`, cgo, or `go:linkname`.

## Documentation Deliverables

Documentation MUST be detailed enough for a new user to adopt the package
without reading its implementation:

- README with decision guide and five-minute quickstart;
- architecture and lifecycle mapping from EventSauce to Go;
- complete API reference and package map;
- aggregate modeling and invariant guide;
- messages, metadata, correlation, causation, and decoration guide;
- serialization, event naming, schema evolution, and upcasting guide;
- PostgreSQL schema, migrations, operations, and transaction guide;
- dispatcher and consumer semantics;
- synchronous, queue, and optional outbox deployment examples;
- direct Kafka and PostgreSQL-outbox-to-Kafka deployment examples;
- snapshot design and versioning;
- projections, replay, checkpoints, and process-manager guide;
- scenario testing and conformance testing;
- EventSauce-to-Go migration guide with side-by-side examples;
- compatibility matrix and intentional differences;
- event-sourcing adoption guide and anti-patterns;
- security, privacy, retention, deletion, and compliance considerations;
- backup, restore, disaster recovery, and history-repair policy;
- performance and capacity planning;
- troubleshooting, FAQ, glossary, and release notes; and
- runnable examples for every primary workflow.

Documentation MUST clearly distinguish package guarantees, adapter guarantees,
application responsibilities, and unsupported exactly-once claims.

## Testing And Quality Standard

Meaningful exact 100% production statement coverage is mandatory. Every viable
mutant MUST be killed, with 100% mutation efficacy and mutant coverage.

Required verification includes:

- aggregate lifecycle, versioning, and optimistic-concurrency tests;
- store, snapshot, dispatcher, codec, decorator, projector, and process-manager
  conformance suites;
- real PostgreSQL transaction, contention, migration, failover, and recovery
  tests;
- model-based comparison of live execution and replay;
- property tests for deterministic serialization, upcasting, and reconstitution;
- fuzzing of envelopes, metadata, codecs, upcasters, snapshots, filters, and
  stored hostile input;
- race and stress tests for every concurrent implementation;
- leak and cancellation tests for iterators, runners, and workers;
- deterministic fault injection at persistence and dispatch boundaries;
- Kafka producer, consumer-group, rebalance, ordering, replay, and offset
  settlement tests;
- clean-consumer examples and generated-code checks;
- EventSauce feature-matrix traceability;
- interoperability tests for documented wire or schema formats; and
- allocation-reporting, statistically sound benchmarks.

Coverage MUST prove meaningful outcomes and invariants, not merely execute
lines. Invalid or equivalent mutants require the repository's narrow reviewed
exception process and MUST NOT become a route around weak tests.

## Performance And Benchmarking

Establish honest equivalent-work comparisons against maintained Go event
sourcing libraries, including at least:

- `looplab/eventhorizon` at https://github.com/looplab/eventhorizon;
- `hallgren/eventsourcing` at https://github.com/hallgren/eventsourcing;
- `thefabric-io/eventsourcing` at
  https://github.com/thefabric-io/eventsourcing; and
- direct application code over `pgx` as the baseline cost floor.

Benchmark:

- event recording and application;
- aggregate reconstitution at multiple stream lengths;
- append throughput and optimistic conflicts;
- serialization and upcasting chains;
- snapshot restoration break-even points;
- projection replay and checkpointing;
- concurrent independent streams and one hot stream;
- in-memory and PostgreSQL stores; and
- outbox adapter overhead separately from core persistence.

Comparisons MUST perform equivalent durability, serialization, validation,
transaction, and dispatch work. Publish Go version, dependency versions,
hardware, database settings, schema, corpus, sample counts, confidence
intervals, latency distributions, throughput, allocations, and raw results.
Do not optimize away correctness or compare durable writes to in-memory work.

## Repository And Release Requirements

- Use the repository's current minimum Go version and standard module layout.
- Register the module and packages in repository manifests when implementation
  begins.
- Use the single root GitHub Actions workflow; do not add package-local
  workflows.
- Every CI gate MUST be runnable locally through repository tooling.
- Enforce format, tidy, vet, strict lint, static analysis, tests, race,
  coverage, mutation, fuzz smoke, vulnerability, secrets, licenses, SBOM,
  provenance, docs, API compatibility, generated-code, integration, and
  clean-consumer gates.
- NilAway remains advisory but visible and no-regression.
- Maintain strict module `CHANGELOG.md` entries for every user-visible change.
- Treat event envelope, event names, serialized formats, schemas, ordering,
  errors, metrics, and repository contracts as SemVer-sensitive APIs.
- Pin official fixtures, external comparison tools, service images, and
  generated corpora with provenance and checksums.

## Execution Plan

1. Verify the latest stable EventSauce release, inventory at least version
   3.9.1, and publish the pinned feature/decision matrix.
2. Design representative Go APIs and migration examples before implementation.
3. Implement envelopes, aggregates, codecs, decorators, repositories,
   dispatchers, in-memory stores, and scenario testing.
4. Implement PostgreSQL storage, migrations, global reads, checkpoints,
   snapshots, projections, replay, and process managers.
5. Implement optional outbox, queue, Kafka, and telemetry adapters without
   coupling either core.
6. Complete code generation only where evidence shows it improves safety and
   maintainability.
7. Run conformance, hostile-input, concurrency, crash, migration, mutation,
   security, and performance hardening.
8. Complete documentation, EventSauce migration guidance, compatibility review,
   and release evidence.

## Non-Goals

- no required CQRS architecture;
- no mandatory command bus, query bus, or event bus;
- no workflow or saga framework hidden inside the core;
- no generic queue or broker implementation;
- no Kafka implementation in the core module;
- no embedded transactional outbox implementation;
- no distributed transaction or exactly-once claim;
- no application service container or framework lifecycle;
- no arbitrary object graph serialization;
- no automatic handler discovery by method name;
- no ORM;
- no business-specific events, aggregates, projections, or policies; and
- no mechanical PHP API clone that compromises Go clarity.

## Acceptance Criteria

The package is ready only when:

- the pinned EventSauce 3.9.1-or-newer feature matrix has no unexplained gaps;
- equivalent EventSauce workflows have concise, documented idiomatic Go APIs;
- the core source and module manifest have no outbox, queue, database,
  framework, telemetry, or generator dependency;
- PostgreSQL behavior and every atomicity claim have real-database evidence;
- replay, evolution, snapshots, projections, and process managers are safe;
- optional outbox integration proves same-transaction persistence without
  coupling the cores;
- Kafka works as a dedicated event bus with preserved per-aggregate ordering,
  explicit offset settlement, and no false cross-system atomicity claim;
- meaningful coverage and mutation results are exactly 100%;
- all affected repository and release gates pass; and
- documentation enables adoption, migration, operation, and recovery without
  implementation archaeology.
