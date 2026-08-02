# Changelog

## Unreleased

### Added

- Process-local positive weighted capacity with strict FIFO admission and
  explicitly bounded waiting.
- Context-aware acquisition, fair immediate attempts, typed saturation and
  lifecycle errors, and deterministic cancellation removal.
- Stable owned permits with concurrent duplicate-release protection.
- Idempotent close, queued-waiter rejection, and context-bounded drain.
- Immutable snapshots and bounded observer events outside synchronization.
- Result-, error-, and panic-preserving execution helpers.
- Kubernetes scope, migration, operations, security, API, FAQ, and performance
  guidance.
- Race, fuzz, conservation, lifecycle, benchmark, coverage, mutation, API,
  documentation, and clean-consumer gate definitions.
- Generated concurrent reference histories, deterministic cancellation and
  shutdown races, queue-node and permit-retention checks, and source-owned
  goroutine/timer/finalizer guards.
- Equivalent benchmark dimensions for strict FIFO handoff, cancellation queue
  depth, mixed weights, observer overhead, x/sync v0.22.0, and the actively
  released kit4go v0.9.0 semaphore, with semantic differences disclosed.
