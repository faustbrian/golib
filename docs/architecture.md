# Architecture

## Repository Boundaries

`golib` is a multi-module monorepo, not one framework module. `pkg/` groups
public modules; `cmd/` contains repository commands; `internal/` is private;
`scripts/` is the shared local and CI command surface.

Each public module owns one coherent concern and can be consumed independently.
Nested adapter modules isolate optional dependencies and release cadence. Core
modules MUST NOT import adapters, applications, or benchmark/interoperability
harnesses.

## Dependency Direction

Owned dependencies are explicit in `modules.json`. The graph must remain
acyclic. Lower-level value/protocol packages may be used by runtime packages;
runtime packages must not force business policy into lower-level modules.

Cross-cutting behavior is composed through narrow interfaces. Shared code is
not moved to `internal/` merely because two packages contain similar syntax;
it must represent one stable semantic owner.

## Runtime Principles

Libraries expose explicit constructors and immutable configuration where
practical. Callers own process lifecycle, dependency injection, and deployment.
Packages do not provide a service container or hidden global registry.

Concurrency, cancellation, shutdown, retry, idempotency, durability, and
observability are contract behavior and must be testable without production
access.
