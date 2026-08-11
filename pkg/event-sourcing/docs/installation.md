# Installation and package map

Install only the modules needed by one aggregate or bounded context. The core
module has no database, queue, outbox, Kafka, telemetry, framework, or
generator dependency:

```sh
go get github.com/faustbrian/golib/pkg/event-sourcing
```

Optional integrations are independently versioned modules:

```sh
go get github.com/faustbrian/golib/pkg/event-sourcing/postgres
go get github.com/faustbrian/golib/pkg/event-sourcing/adapters/gokafka
go get github.com/faustbrian/golib/pkg/event-sourcing/adapters/outbox
go get github.com/faustbrian/golib/pkg/event-sourcing/adapters/queue
go get github.com/faustbrian/golib/pkg/event-sourcing/adapters/gotelemetry
```

Before the first tagged release, these commands describe the stable module
boundaries but consumers must use a reviewed source revision. Do not add
permanent `replace` directives or local paths to a releasable application.

## Core packages

| Package | Responsibility |
| --- | --- |
| `eventsourcing` | Envelopes, aggregate lifecycle, repository composition, stores, dispatchers, codecs, decorators, clocks, identifiers, upcasting, and errors |
| `memory` | Conformant in-memory event and snapshot stores for tests and development |
| `eventtest` | Scenario tests and deterministic fixtures using ordinary `testing.TB`, functions, and values |
| `snapshot` | Optional snapshot restoration and explicit refresh composition |
| `projection` | Bounded global replay, filters, checkpoints, poison policy, rebuild, pause, resume, and status |
| `processmanager` | Pure event-to-command planning with explicit replay policy |

## Optional modules

| Module | Dependency boundary and guarantee |
| --- | --- |
| `postgres` | Adds `pgx`; owns PostgreSQL schemas, migrations, event storage, global reads, snapshots, and checkpoints |
| `adapters/gokafka` | Adds the repository Kafka contract; preserves topics, partitions, keys, offsets, acknowledgements, groups, replay, and failure policy |
| `adapters/outbox` | Depends on the public event-sourcing, outbox, and PostgreSQL contracts for caller-owned same-transaction staging |
| `adapters/queue` | Depends on the compatible queue contract; backend-specific durability and settlement guarantees remain observable |
| `adapters/gotelemetry` | Adds OpenTelemetry instrumentation and propagation without changing core behavior |

Kafka is intentionally not routed through `eventqueue`. The event-sourcing core
does not import any optional module. The outbox core and event-sourcing core do
not import each other.

## Go version and module ownership

Use the Go version declared by the selected module. Every I/O operation accepts
`context.Context`, and constructors validate dependencies before returning.
Applications own database transactions, runner goroutines, cancellation,
shutdown, queue workers, Kafka clients, and telemetry providers unless an
adapter explicitly documents a narrower rule.

Start with the core and in-memory store. Add PostgreSQL only when persistence
semantics are understood, and add asynchronous publication only after
duplicate delivery, recovery, poison events, and replay isolation have
application policies.
