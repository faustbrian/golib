# API reference

## Engine

- `New(Config) (*Injector, error)` copies and validates all rules.
- `(*Injector).Decide(Metadata) Decision` atomically assigns an evaluation
  sequence and selects faults.
- `(*Injector).Snapshot() Snapshot` returns bounded aggregate and per-rule
  counters in precedence order.
- `(*Injector).Reset() uint64` clears counters and advances the generation.
- `Run[T]` applies function-boundary phases and returns zero `T` for an injected
  error.

The zero `Injector`, a nil `*Injector`, and adapters given nil injectors are
disabled. `New(Config{})` is explicitly constructed but contains no rules.

## Rules

Every `Rule` declares:

- safe `ID` and exact `Scope`;
- `Active` or `Inactive` activation;
- nonzero `Maximum` activations;
- `Continue` or `Stop` composition;
- integer `Order` with identity tie-breaking;
- `Observe` or `Suppress` event policy;
- `Nth`, `Every`, `Sequence`, or seeded `Probability` schedule;
- optional deterministic concurrency-safe `Predicate`; and
- a nonempty ordered `Faults` list.

`Metadata` contains only a boundary, numeric operation identity, and numeric
attempt. Applications must map their domain operations to safe numeric values;
they must not add request or tenant data to rule identities.

## Fault constructors

- `ErrorFault`
- `LatencyFault`
- `CancelFault`
- `DeadlineFault`
- `PanicFault`
- `ByteFault`

The corresponding `Kind` constants cover drop, truncate, duplicate, reorder,
corrupt, short read/write, temporary/permanent network failure, reset,
half-close, and interruption. Adapters ignore fault kinds for which they do not
define valid semantics.

## Dependencies and observations

`Clock` controls safe event and runtime expiry time. `Sleeper` controls injected
latency. Seed values control probabilistic streams. `Observer` receives one
bounded `Event` for every emitted selected fault after the state lock is
released. Observer and clock panics cannot change a selected decision.

## Runtime gate

`NewRuntime(RuntimeConfig)` requires an injector, `Authorizer`, exact allowlist,
expiry, maximum evaluation budget, `Auditor`, and optional injected clock.
`Decide` fails closed and emits an `AuditEvent`; `Disable` is terminal.

## Adapters

- `NewRoundTripper`
- `WrapReader` / `WrapWriter`
- `WrapConn` / `WrapDialer` / `WrapListener`
- `WrapFS`
- `WrapSleeper` / `WrapTimerFactory`

See [adapters.md](adapters.md) for ownership and partial results.
