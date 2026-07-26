# Architecture

The library preserves EventSauce's pragmatic separation of three replaceable
responsibilities while making their ownership explicit in Go:

| Responsibility | Go contract | Owns | Does not own |
| --- | --- | --- | --- |
| Aggregate repository | `Repository[ID, Aggregate]` | Loading, reconstitution, preparing one change set, optimistic append, acknowledgement, and selected post-commit dispatch | Domain invariants, database transactions exposed by adapters, queues, or retries |
| Event store | `EventStore` | Atomic ordered append to one stream and bounded ordered reads | Aggregate construction, payload decoding, dispatch, projections, or background work |
| Dispatcher | `Dispatcher` | Delivery of already persisted messages through the selected strategy | Persistence, transaction commit, queue settlement, or exactly-once guarantees |

The root package provides the reference `AggregateRepository`, but each
responsibility can be replaced independently. Applications can use the event
store without the reference repository, use a custom repository with the
first-party store, or select a different dispatcher without changing aggregate
behavior.

## Composition

```text
domain behavior
      |
      v
AggregateRepository
      | prepare and encode
      v
EventStore.Append ---- durable commit boundary
      | committed messages
      v
Dispatcher ---- Consumer(s)
```

The safe repository path dispatches only after the event store reports a
committed append. A failed, conflicting, or unknown append outcome never
becomes an external dispatch. Caller-owned PostgreSQL transaction staging is a
separately named adapter operation because it has not committed yet. The core
repository exposes an explicit prepare, confirm, unknown-outcome, and
post-commit dispatch lifecycle without owning the transaction.

No core constructor starts a goroutine or opens a transaction. The caller owns
concurrency, cancellation, transaction commit or rollback, runner lifetimes,
and shutdown.

## Aggregate boundary

Application aggregates own:

- identifiers and their canonical encoding;
- command methods and invariants;
- stable event names and schema versions;
- the explicit deterministic event-application switch; and
- the state represented by an optional snapshot codec.

`Lifecycle` is an embeddable bookkeeping value, not a base class. It tracks
committed and pending versions, applies recorded events immediately, and
acknowledges the exact persisted change set. It does not discover handlers,
serialize an object graph, or define domain behavior.

See [aggregate modeling](aggregates.md) and the
[lifecycle guide](lifecycle.md) for creation, reconstitution, child entities,
and failure semantics.

## Message and serialization boundaries

`Message` is the immutable-by-contract persistence envelope. It carries stable
stream and event identity, versions, encoded payload, metadata, recording time,
correlation and causation, optional tenant or partition data, and an optional
store-assigned global position.

Payload codecs map explicit application event values to encoded payloads.
Stores persist already encoded messages and never depend on Go concrete type
names. Message codecs used by transport adapters remain separate so changing a
queue wire format does not change aggregate persistence or payload decoding.

Upcasters run at the read boundary before application decoding. They never
rewrite stored history. Anti-corruption translators operate on deliveries
outside aggregate reconstitution so integration mapping cannot change domain
history implicitly.

## Dispatch and asynchronous boundaries

The core synchronous dispatcher has explicit ordering, filtering, cancellation,
panic, and partial-success semantics. Asynchronous delivery is not a hidden
dispatcher mode:

- `adapters/goqueue` preserves the compatible queue contract;
- `adapters/gokafka` exposes Kafka-native producer and consumer semantics; and
- `adapters/gooutbox` stages event and outbox rows in one PostgreSQL
  transaction through public contracts.

Direct PostgreSQL-to-Kafka dispatch is not atomic. Kafka idempotent production
or Kafka transactions do not create a distributed transaction with PostgreSQL.
Durable asynchronous publication is at-least-once and requires idempotent
consumers. Direct dispatch provides no stronger end-to-end guarantee.

## Derived-state boundaries

Snapshots, projections, checkpoints, and process-manager state are derived or
application-owned data. Event history remains authoritative.

- `snapshot` restores optional encoded state and then reads events strictly
  after the snapshot version.
- `projection` performs bounded global replay with explicit checkpoints,
  filters, poison policy, hooks, pause, resume, and rebuild control.
- `processmanager` plans bounded application commands without executing side
  effects or storing hidden workflow state.

Replay deliveries are explicitly marked. Side-effecting consumers, process
managers, queue publication, Kafka publication, and outbox insertion must reject
replay unless a separately named operation intentionally enables it.

## Dependency direction

The core module uses the standard library only. Optional databases, brokers,
outboxes, and telemetry dependencies live in independently versioned nested
modules. The core never imports an adapter. The event-sourcing and outbox cores
never import one another; only the optional bridge imports both public APIs.

See [package and adapter boundaries](design/package-boundaries.md) for the full
module graph and transaction ownership rules.

## EventSauce mapping

| EventSauce concept | Idiomatic Go outcome |
| --- | --- |
| `AggregateRootRepository` | Small generic `Repository` contract and explicit reference composition |
| `MessageRepository` | Storage-independent `EventStore` plus optional `GlobalReader` capability |
| `MessageDispatcher` and chains | Small synchronous `Dispatcher` with explicit consumer composition; broker adapters remain separate |
| Aggregate traits | Application struct plus optional `Lifecycle` bookkeeping value |
| Class-name inflection | Explicit stable event registration and aliases |
| Message serializer | Adapter-specific message codec, separate from payload persistence |
| Payload serializer | Explicit `PayloadCodec` registrations |
| Serializer decoration for upcasting | Ordered bounded `UpcasterChain` at the read boundary |
| Clock replacement | Injected `Clock`, including fixed test clocks |
| Framework bootstrap | Ordinary constructors and explicit dependency composition |

This is behavioral and conceptual compatibility. PHP inheritance, traits,
service containers, method-name handler discovery, arbitrary hydration, and
framework lifecycle hooks are intentionally not reproduced.
