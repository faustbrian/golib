# Package and adapter boundaries

## Decision

The first release uses a small core module and independently versioned nested
modules only where they isolate a material dependency or release boundary.
The table includes both implemented and remaining release boundaries.

| Package or module | Responsibility | Dependency rule |
| --- | --- | --- |
| root `eventsourcing` package | Messages, aggregate lifecycle, repository, store, dispatch, codecs, decorators, errors, clocks, and ID generation contracts | Standard library only |
| `memory` | Conformant in-memory event, snapshot, and projection-control stores | Core and projection contracts |
| `eventtest` | Aggregate scenarios and public conformance suites | Core and standard `testing` contracts only |
| `snapshot` | Snapshot composition and policies | Core only |
| `projection` | Global replay, checkpoints, projection runners, and replay control | Core only |
| `processmanager` | Side-effect-safe planning contracts and explicit runners | Core only |
| `postgres` nested module | PostgreSQL event, snapshot, and checkpoint stores plus migrations | Core, `pgx`, and public migration contracts |
| `adapters/gooutbox` nested module | Same-transaction conversion between event messages and public outbox envelopes | Core and public outbox contracts |
| `adapters/gokafka` nested module | Kafka-native producer and consumer semantics | Core and `franz-go` |
| `adapters/goqueue` nested module | Adapters for queue backends that preserve the required event semantics | Core and public queue contracts |
| `adapters/gotelemetry` nested module | OpenTelemetry spans, metrics, and propagation | Core and public telemetry contracts |
| `codegen` and `cmd/golib-event-sourcing` nested module | Optional deterministic registry and event declaration generation | Core plus generation-only dependencies |

Subpackages will not be created merely to hold one interface. The root package
owns coherent storage-independent contracts. Nested modules are required for
PostgreSQL, Kafka, telemetry, queue, outbox, and generator dependency
isolation.

## Dependency direction

```text
application aggregates
        |
        v
eventsourcing core <--- eventtest / snapshot / projection / processmanager
        ^                                      ^
        |                                      |
        +---------------- memory --------------+
        |
        +--- postgres
        +--- adapters/gokafka
        +--- adapters/goqueue
        +--- adapters/gotelemetry
        +--- adapters/gooutbox ---> outbox public API
```

The event-sourcing core never imports an adapter. The event-sourcing and outbox
cores never import one another. The optional `gooutbox` adapter is the only
component that depends on both public contracts.

## Transaction boundary

The core event-store contract treats a successful append as committed. A store
bound to an uncommitted caller transaction therefore cannot implement that
contract. The PostgreSQL module provides:

- a committed store whose explicitly documented append operation owns and
  commits one short PostgreSQL transaction; and
- a separately named staging API for a caller-owned `pgx.Tx`.

The staging API never acknowledges aggregate changes or dispatches messages.
After commit succeeds, the caller confirms the prepared save, which
acknowledges the aggregate and permits post-commit dispatch. Rollback leaves the
original change set pending. Commit ambiguity requires reconciliation by
message ID before the aggregate can be reused.

The outbox adapter provides a committed event store for the ordinary aggregate
repository and a separately named caller-owned transaction stager. Both write
event rows and outbox rows through one PostgreSQL transaction. The stager does
not commit or roll back; the committed store owns one short transaction. No
dispatcher runs before the committed store succeeds. A direct
event-store-to-Kafka dispatch is explicitly non-atomic.

## Replay boundary

Replay is a distinct delivery mode, not a metadata convention applied after
dispatch begins. Projection consumers may opt into live, replay, or both.
Process managers, external dispatchers, queue adapters, Kafka producers, and
outbox insertion reject replay unless the caller selects a separately named
operation that makes the side effect explicit.

## Goroutine ownership

The core starts no goroutines. Runners in optional packages expose blocking
`Run(context.Context) error` methods. The caller owns goroutine creation,
cancellation, waiting, and shutdown. Constructors never start work.
