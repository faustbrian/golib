# EventSauce 3.9.1 compatibility matrix

## Pin and scope

This matrix uses EventSauce `3.9.1`, Git tag commit
`33ea9b97ec3ac56991caad03b791fee418a43e41`, released on 2026-04-25.
The tag and commit were verified against the upstream Git repository on
2026-07-25. The live documentation changelog listed 3.9.1 as the latest stable
release on the same date.

The upstream source is MIT licensed. The license was reviewed before this
design inventory. No EventSauce source or documentation text is copied into
this module; the matrix records behavioral concepts and independent Go
decisions.

EventSauce's documentation site is not versioned in its URLs. Every page below
is therefore pinned by the 3.9.1 source tag and the documentation inventory
retrieved on 2026-07-25. A future baseline update must review every row rather
than silently replacing the target.

Status meanings:

- **implemented**: the row's named core capability and required focused tests
  exist, without implying module or release readiness;
- **partial**: a usable core portion exists but listed integration evidence or
  documentation remains incomplete;
- **designed**: the Go outcome is specified but not implemented;
- **planned**: the capability and boundary are inventoried but its detailed API
  remains to be proven;
- **excluded**: the behavior is deliberately not reproduced, with a migration
  path;
- **external**: application or adapter behavior documented by this module, not
  a core guarantee.

No status in this document means production-ready.

## Documentation-page inventory

| EventSauce page | Expected EventSauce behavior | Go decision | Required tests | Go documentation | Status |
| --- | --- | --- | --- | --- | --- |
| [Introduction](https://eventsauce.io/docs/) | Pragmatic, composable event sourcing without mandatory CQRS or an application rewrite | Preserve the product position and three replaceable boundaries with explicit Go composition | Quickstart compile test and dependency-boundary test | README, adoption guide, architecture | Designed |
| [Installation](https://eventsauce.io/docs/installation/) | Core, test utilities, code generation, persistence, and dispatch can be installed separately | Use independent Go modules for dependency-bearing adapters and optional generation | Clean-consumer module tests for core and every adapter | Installation and package map | Planned |
| [Event sourcing](https://eventsauce.io/docs/event-sourcing/) | Events model business transitions; adoption has meaningful costs and tradeoffs | Preserve the model and explicitly recommend conventional persistence when transitions do not justify the cost | Documentation assertions and runnable conventional-versus-event-sourced examples | Adoption guide and anti-patterns | Designed |
| [Learning material](https://eventsauce.io/docs/learning-material/) | Direct users to foundational event-sourcing material | Maintain a curated, source-verified learning section without presenting links as package guarantees | Link and docs checks | Learning resources | Planned |
| [Architecture](https://eventsauce.io/docs/architecture/) | Aggregate repository, message repository, and dispatcher are tiny replaceable responsibilities; decorators, serializers, inflectors, and clocks compose around them | Preserve repository, event store, and dispatcher; use explicit codecs, stable names, decorators, clocks, and ID generators | Interface conformance, dependency, and composition tests | Architecture and package map | Designed |
| [FAQ](https://eventsauce.io/docs/faq/) | Explain project choices and common EventSauce usage questions | Provide Go-specific answers about event sourcing, versioning, replay, transactions, and delivery guarantees | Docs checks | FAQ | Planned |
| [Lifecycle](https://eventsauce.io/docs/lifecycle/) | Retrieve, reconstitute, perform behavior, record and immediately apply, persist, then dispatch and react | Preserve the recognizable lifecycle; dispatch only after successful durable append by default | Model-based live/replay equivalence and repository lifecycle scenarios | Lifecycle guide and quickstart | Partial |
| [Changelog](https://eventsauce.io/docs/changelog/) | Publish versioned upstream behavior changes | Track the pinned upstream version here and maintain this module's own strict changelog | Matrix baseline check and changelog policy test | Compatibility matrix and CHANGELOG | Designed |
| [Create an aggregate root](https://eventsauce.io/docs/event-sourcing/create-an-aggregate-root/) | Aggregate behavior records events, reconstitutes history, applies known events, and uses application-defined IDs | Application aggregate owns invariants and an explicit type switch; optional lifecycle helper handles versions and pending events | Aggregate lifecycle, unknown event, typed ID, child entity, and corrupt-history tests | Aggregate modeling guide | Designed |
| [Create events and commands](https://eventsauce.io/docs/event-sourcing/create-events-and-commands/) | Read-only event payload objects support serialization; commands remain application concerns | Use ordinary Go values and explicit registered codecs; define no command abstraction in core | Codec round trips, immutability, exact-number, time, and malformed input tests | Events and codecs guide | Designed |
| [Configure persistence](https://eventsauce.io/docs/event-sourcing/configure-persistence/) | A message repository reconstitutes aggregates and a dispatcher reacts to persisted messages | Separate `EventStore` and `Dispatcher`; projections handle arbitrary queries | Store/dispatcher conformance and repository integration tests | Storage and dispatch setup | Partial |
| [Bootstrap](https://eventsauce.io/docs/event-sourcing/bootstrap/) | Construct a default aggregate repository from aggregate type, message repository, and dispatcher | Build a validated generic repository from factory, ID codec, lifecycle access, codecs, store, clock, ID generator, decorators, and dispatcher | Constructor matrix and five-minute compile example | Quickstart and API reference | Partial |
| [Object mapper serialization](https://eventsauce.io/docs/serialization/object-mapper/) | Object hydration can serialize payloads with little application code | Do not reproduce arbitrary object hydration; use typed explicit registrations, with optional generation for repetition | Registration, ambiguity, schema, and generated-code tests | EventSauce migration guide | Excluded |
| [Plain serialization](https://eventsauce.io/docs/serialization/plain-serialization/) | Payload values convert explicitly to and from serializable arrays | Provide explicit encoder/decoder functions through a payload-codec registry | Golden round trips, strict mode, bounds, Unicode, numbers, and time tests | Serialization guide | Designed |
| [Testing aggregates](https://eventsauce.io/docs/testing/) | Scenario tests stage history, execute behavior, and assert emitted events | `eventtest` uses ordinary functions and result values without a custom runner or global assertion DSL | Self-tests plus aggregate scenario conformance | Scenario-testing guide | Partial |
| [Testing preconditions](https://eventsauce.io/docs/testing/preconditions/) | Scenarios accept no history or specified historical events | Provide immutable `GivenNone`, consecutive `Given`, and explicit split-aware `GivenHistory` scenarios | No-history, history, reconstitution-only, and malformed-history scenarios | Scenario-testing guide | Partial |
| [Handling exceptions](https://eventsauce.io/docs/testing/handling-exceptions/) | Tests assert expected failures and define whether events recorded before failure remain | Return Go errors, propagate panics by default, and capture panics only through an explicitly named operation; pending events include completed `Record` calls | Error, panic, partial behavior, and no-event scenarios | Scenario-testing guide | Partial |
| [Asserting event payloads](https://eventsauce.io/docs/testing/asserting-the-payload-of-an-event/) | Tests assert event type and full or partial payload | Expose events as ordinary values and provide redacted identity, payload-predicate, and exact-metadata matchers | Typed equality, custom comparison, metadata, and diagnostic-redaction tests | Scenario-testing guide | Partial |
| [Testing with time](https://eventsauce.io/docs/testing/testing-with-time/) | A test clock controls recorded time | Inject core fixed or manual clocks and deterministic `eventtest` message-ID sequences without global replacement | Fixed, advanced, UTC, precision, ID exhaustion, and concurrency tests | Clock and testing guides | Partial |
| [About message storage](https://eventsauce.io/docs/message-storage/) | Message repositories persist aggregate messages; an outbox enables durable asynchronous relay | Core event store is independent; optional outbox adapter composes public contracts | Core isolation, append/read conformance, and adapter independence tests | Event-store and outbox guides | Designed |
| [Repository table schema](https://eventsauce.io/docs/message-storage/repository-table-schema/) | First-party SQL schemas store event ID, aggregate identity/version, time, payload, and headers | PostgreSQL module owns versioned schemas with explicit event/schema identities, content type, metadata, correlation, causation, tenant/partition, and global position | Clean install, migration, constraints, indexes, plans, backup and restore tests | PostgreSQL schema and operations guide | Planned |
| [Illuminate repository](https://eventsauce.io/docs/message-storage/illuminate/) | Laravel database integration provides a message repository | No framework adapter in core; Go applications use the PostgreSQL module or implement `EventStore` | Exclusion compile test and custom-store conformance example | Migration guide | Excluded |
| [Doctrine 3 repository](https://eventsauce.io/docs/message-storage/doctrine-3/) | Doctrine DBAL 3 provides schema-specific storage | Replace with the independently versioned pgx PostgreSQL store | Real PostgreSQL conformance and transaction tests | EventSauce migration and PostgreSQL guides | Excluded |
| [Doctrine 2 repository](https://eventsauce.io/docs/message-storage/doctrine-2/) | Doctrine DBAL 2 provides schema-specific storage | Replace with the independently versioned pgx PostgreSQL store | Real PostgreSQL conformance and transaction tests | EventSauce migration and PostgreSQL guides | Excluded |
| [UUID encoding](https://eventsauce.io/docs/message-storage/uuid-encoding/) | Repositories can choose binary or string UUID encoding | Treat IDs as validated canonical application strings in core; PostgreSQL schema documents optional UUID-native application choices without assuming UUIDs | Identifier codec, invalid encoding, round-trip, ordering, and migration tests | Identifier and PostgreSQL guides | Designed |
| [Setup consumers](https://eventsauce.io/docs/reacting-to-events/setup-consumers/) | Synchronous dispatch invokes registered consumers | Provide small context-aware consumer and dispatcher contracts | Ordering, cancellation, empty batch, duplicate, panic, reentrant, and partial success tests | Dispatcher semantics | Implemented |
| [Projections and read models](https://eventsauce.io/docs/reacting-to-events/projections-and-read-models/) | Consumers build query-oriented read models from events | Optional projection package provides bounded global iteration, checkpoints, rebuild, resume, and poison policy without requiring CQRS | Projection, checkpoint, idempotency, reset, replay, and fault-injection suites | Projection and replay guide | Partial |
| [Process managers](https://eventsauce.io/docs/reacting-to-events/process-managers/) | Consumers react to events and coordinate actions across aggregates | Pure planners emit explicit commands/messages; application-owned executors perform effects and replay is rejected by default | Planning, duplicate, retry, replay isolation, and process-state tests | Process-manager guide | Partial |
| [Clock](https://eventsauce.io/docs/utilities/clock/) | System and test clocks provide production and deterministic time | Define a tiny clock contract and explicit system/manual implementations without global replacement | UTC, monotonic stripping, precision, movement, and race tests | Clock API reference | Implemented |
| [Event dispatcher](https://eventsauce.io/docs/utilities/event-dispatcher/) | Non-aggregate events can be decorated, wrapped as messages, and dispatched | Offer a separately named publisher helper only if it preserves message validation and does not imply persistence | Decoration, message generation, error, and direct-dispatch tests | Dispatch guide | Planned |
| [Code generation](https://eventsauce.io/docs/code-generation/) | Optional generator creates repetitive payload objects and serialization code | Optional nested Go generator only if handwritten API evaluation proves a material safety benefit | Golden, deterministic, stale, version, provenance, and compile tests | Code-generation guide | Planned |
| [Code generation from YAML](https://eventsauce.io/docs/code-generation/from-yaml/) | YAML definitions generate commands/events with typed fields and serialization | Do not copy the PHP schema; any Go schema is explicit, deterministic, rejects ambiguity, and never required | Hostile schema, unknown type, deterministic output, and clean compile tests | Generator schema reference | Planned |
| [About the outbox](https://eventsauce.io/docs/message-outbox/) | Transactional outbox makes database persistence and asynchronous publication durable | Keep both cores independent; optional adapter writes event and outbox rows in one caller-owned PostgreSQL transaction and promises at-least-once only | Dependency, rollback, commit ambiguity, duplicate, and crash tests | Outbox guarantees and deployment guide | Planned |
| [Outbox setup and usage](https://eventsauce.io/docs/message-outbox/setup-and-usage/) | Configure repository, relay, backoff, and commit strategies around one database transaction | Provide explicit composition examples; relay ownership stays in outbox and no hidden transaction is introduced | Same-transaction identity, retry, relay, replay-isolation, and recovery tests | PostgreSQL-outbox-to-Kafka example | Planned |
| [Outbox table schema](https://eventsauce.io/docs/message-outbox/table-schema/) | Durable pending messages have a schema for claiming and publication | The outbox package owns its schema; adapter converts through public envelopes and never depends on internal rows | Clean install, compatibility, conversion, and schema-independence tests | Outbox adapter guide | External |
| [Illuminate outbox](https://eventsauce.io/docs/message-outbox/illuminate/) | Laravel transactions atomically write application data and outbox messages | No framework adapter; compose the PostgreSQL event store and public outbox API | Exclusion and same-transaction integration tests | Migration guide | Excluded |
| [Doctrine 3 outbox](https://eventsauce.io/docs/message-outbox/doctrine-3/) | Doctrine DBAL 3 transaction integrates outbox storage | Replace with caller-owned pgx transaction in the optional adapter | Real PostgreSQL rollback, commit, ambiguity, and crash tests | Migration and transaction guides | Excluded |
| [Doctrine 2 outbox](https://eventsauce.io/docs/message-outbox/doctrine-2/) | Doctrine DBAL 2 transaction integrates outbox storage | Replace with caller-owned pgx transaction in the optional adapter | Real PostgreSQL rollback, commit, ambiguity, and crash tests | Migration and transaction guides | Excluded |
| [Build an outbox](https://eventsauce.io/docs/message-outbox/build-your-own/) | Public contracts allow custom outbox persistence and relay | Document the public boundary and provide conformance tests without putting an outbox in core | Adapter conformance and non-event-sourced outbox tests | Custom adapter guide | Planned |
| [Snapshotting](https://eventsauce.io/docs/snapshotting/) | Snapshots accelerate aggregate restoration and carry a version | Treat snapshots as optional derived data with aggregate and snapshot schema versions | Full-history equivalence, stale, corrupt, incompatible, missing, and deletion tests | Snapshot design and versioning | Partial |
| [Snapshot setup](https://eventsauce.io/docs/snapshotting/setup/) | Aggregate and repository opt into snapshot restoration | Explicit snapshot codec/repository composition; constructors start no work | Setup example, fallback policy, events-after-version, and ownership tests | Snapshot quickstart | Implemented |
| [Updating snapshots](https://eventsauce.io/docs/snapshotting/updating-snapshots/) | Update from an existing snapshot or clean event stream | Provide explicit blocking refresh from full or restored state; no hidden background goroutine | Threshold, replacement, failure, concurrent update, and rebuild tests | Snapshot operations guide | Partial |
| [Anti-corruption layer](https://eventsauce.io/docs/advanced/anti-corruption-layer/) | Filters and translators adapt inbound/outbound messages and can drop messages | Use explicit filter/translator chains; event aliases handle persisted renames, while integration translation lives outside aggregate replay | Filter, translate, chain, empty result, ordering, and error tests | Event naming and anti-corruption guide | Planned |
| [Replaying messages](https://eventsauce.io/docs/advanced/replaying-messages/) | Paginated repository reads dispatch historical messages with before/after hooks | Use bounded global iteration, durable checkpoints, explicit filters and live/replay delivery modes | Pagination, filters, hooks, cancellation, resume, poison, and side-effect isolation tests | Replay guide | Partial |
| [Database structure](https://eventsauce.io/docs/advanced/database-structure/) | Documents shared table, per-type table, per-ID table, and document tradeoffs | First-party PostgreSQL defaults to a shared partitionable table; custom stores remain possible | Scale plans, partitioning, long-stream, hot-stream, archive, and restore tests | Capacity and PostgreSQL operations guide | Planned |
| [Message internals](https://eventsauce.io/docs/advanced/message-internals/) | Message envelope carries event payload and reserved headers; serializers and name inflectors make it persistent | Use dedicated typed envelope fields, string metadata, explicit stable event names, separate codecs, and no class-name inflection | Field validation, ownership, reserved prefix, canonical encoding, and redaction tests | Message and metadata guide | Designed |
| [Message decoration](https://eventsauce.io/docs/advanced/message-decoration/) | Decorators add recording time and custom headers before persistence | Immutable decorators return validated copies; clock and ID generation are explicit and ordered | Chain order, collision, bounds, error, immutability, clock, and ID tests | Decoration guide | Implemented |
| [Upcasting](https://eventsauce.io/docs/advanced/upcasting/) | Serializer decorators transform old payloads and compose in chains | Upcast encoded events at the read boundary; allow rename, metadata/payload change, split, and reviewed drop with bounded monotonic chains | Golden, determinism, cycle, progress, split/drop, hostile-input, snapshot, and replay tests | Schema evolution guide | Partial |
| [Custom repository](https://eventsauce.io/docs/advanced/custom-repository/) | Applications replace message persistence behind a tiny interface | Implement `EventStore` and run the public conformance suite; optional capabilities remain separate | Reusable store conformance suite and custom-store example | Custom store guide | Designed |
| [Custom dispatcher](https://eventsauce.io/docs/advanced/custom-dispatcher/) | Applications replace dispatch and compose chains | Implement the small dispatcher contract; queue, Kafka, and outbox remain optional adapters | Reusable dispatcher conformance and custom-dispatcher example | Custom dispatcher guide | Designed |
| [Aggregate root with aggregates](https://eventsauce.io/docs/advanced/aggregate-root-with-aggregates/) | Child aggregate/entity behavior records events through the root identity and lifecycle | Child entities receive an explicit root recorder; no nested independently persisted aggregate is hidden inside the root | Root identity, order, child lifecycle, reconstitution, and invariant tests | Aggregate modeling guide | Designed |
| [Upgrade to 0.6](https://eventsauce.io/docs/upgrading/to-0-6-0) | Historical PHP construction changes explain private constructors and reconstitution | Named Go factories make creation and reconstitution explicit; PHP trait migration is documentation-only | Factory and reconstitution compile examples | EventSauce migration guide | Excluded |
| [Upgrade to 0.7](https://eventsauce.io/docs/upgrading/to-0-7-0) | Repository reports aggregate versions and can retrieve after a version for snapshots | Store assigns versions and supports bounded reads after a version | Version-return, range, snapshot, empty and corrupt history tests | Migration and store guides | Designed |
| [Upgrade to 1.0](https://eventsauce.io/docs/upgrading/to-1-0-0) | Historical package extraction, renames, clock extraction, and PHP return types | Preserve independent Go modules and explicit types; PHP renames have no source compatibility target | Clean-consumer and module-boundary tests | EventSauce migration guide | Excluded |

## Adapter capability matrix

| Adapter | First-release outcome | Guarantee boundary | Status |
| --- | --- | --- | --- |
| Synchronous core dispatch | Ordered in-process delivery with explicit error and panic policy | No durability beyond the successful event-store append | Implemented |
| PostgreSQL | pgx event, snapshot, checkpoint, and global-read implementations in a nested module | PostgreSQL transaction and documented schema guarantees only | Planned |
| `gokafka` | Kafka-native mapping, topics, keys, acknowledgements, idempotent producer options, manual offsets, groups, rebalances, retries, dead letters, and replay | Kafka does not make PostgreSQL writes atomic and does not provide end-to-end exactly once | Planned |
| `goqueue` | Preserve event identity and explicit settlement for compatible queue backends | Only backend capabilities proven by the adapter | Planned |
| `gooutbox` | Same caller-owned PostgreSQL transaction for event and outbox rows | At-least-once publication with duplicate and commit-ambiguity handling | Planned |
| `gotelemetry` | Optional tracing, metrics, and propagation without payload or secret disclosure | Observability only; no persistence or delivery guarantee | Planned |

## Intentional Go differences

| EventSauce mechanism | Go outcome | Migration |
| --- | --- | --- |
| Traits and inheritance-like aggregate behavior | Explicit application composition with an optional lifecycle value | Replace trait methods with a local `apply` type switch and lifecycle delegation |
| `apply<EventName>` method discovery | Explicit event application | Use a type switch or application-owned map; unknown events return errors |
| PHP class-name inflection | Explicit stable event names and aliases | Register each persisted name and schema version |
| Object hydration | Explicit typed encoders and decoders | Write ordinary Go functions or opt into reviewed generation |
| Mixed PHP header values | Dedicated protocol fields plus bounded string metadata | Move correlation, causation, tenant, partition, time, and versions to typed fields; encode application metadata intentionally |
| Exceptions as primary failure assertions | Go errors with a narrow documented panic policy | Assert with `errors.Is`/`errors.As`; panics are only programmer-error scenarios |
| Framework-specific Doctrine/Illuminate storage | Independently versioned pgx module | Compose the PostgreSQL store or implement and conform a custom store |
| Framework-specific outbox wiring | Public-contract adapter with caller-owned transaction | Pass one pgx transaction to both event and outbox operations |
| Generator-defined PHP DTO conventions | Ordinary Go structs and optional deterministic Go generation | Handwrite values and codecs first; generation never becomes mandatory |

## Required traceability before release

Every designed or planned row must link to exported API, focused behavioral
tests, complete documentation, and its affected release-gate evidence before
the status can become implemented. Exclusions require compile examples or
migration documentation proving that the supported Go outcome remains usable.
