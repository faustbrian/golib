# Event Sourcing

`event-sourcing` is a pragmatic event-sourcing library for Go under active
development. It is designed around three independently replaceable
responsibilities:

1. an aggregate repository;
2. an immutable event store; and
3. a message dispatcher.

The design is inspired by EventSauce while using explicit Go composition,
small consumer-owned interfaces, and `context.Context` at I/O boundaries. It
does not require CQRS, a command bus, a query bus, a queue, an outbox, a
framework, reflection-based handler discovery, or code generation.

The package is in its early implementation phase and is not ready for
application use.

## Design documents

- [EventSauce 3.9.1 compatibility matrix](docs/compatibility/eventsauce-3.9.1.md)
- [Public API design](docs/design/public-api.md)
- [Package and adapter boundaries](docs/design/package-boundaries.md)
- [Aggregate scenario testing](docs/testing.md)
- [Snapshot storage](docs/snapshots.md)

## When to use event sourcing

Event sourcing can be a good fit when business transitions, auditability,
temporal reconstruction, or multiple derived views are central to the domain.
It introduces durable schema-evolution, replay, operational, privacy, and
consistency obligations. Conventional state persistence is usually the better
choice when only current state matters or the domain does not justify those
costs.

## Status

No compatibility or stability promise applies until the first release. Planned
work is tracked explicitly in the compatibility matrix; a planned row is not
an implemented capability.
