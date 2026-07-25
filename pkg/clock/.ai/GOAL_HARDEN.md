# Hardening Goal: Deterministic Clock And Timer Foundation

## Objective

Prove that `clock` remains deterministic, race-free, leak-free, bounded, and
semantically aligned with Go time behavior under concurrent advancement, timer
reset/stop races, callback reentrancy, clock jumps, cancellation, and shutdown.

## Required Audits

### Timer And Ticker State Audit

- Model create, active, fired, stopped, reset, drained, canceled, and released
  states for every timer and ticker API.
- Exhaust zero, negative, minimum, maximum, overflow, same-instant, and repeated
  reset/stop behavior.
- Mutation-test every transition and return value affecting whether work fires.
- Differential-test system wrappers against the supported Go standard library.

### Manual Advancement Audit

- Prove deterministic ordering for timers, tickers, sleepers, and callbacks at
  equal and different timestamps.
- Exercise callbacks that create, stop, reset, or wait on additional clock work.
- Verify advancement waiters observe all contractually triggered work without
  guessing scheduler progress.
- Bound recursively scheduled same-instant work and fail predictably on limits.

### Concurrency And Lifecycle Audit

- Race/stress concurrent `Now`, advance, jump, stop, reset, wait, callback,
  cancellation, and shutdown.
- Prove no internal lock is held while invoking user callbacks or observation.
- Detect goroutine, timer, ticker, waiter, channel, and callback leaks.
- Exercise callback panic and verify clock state remains coherent.

### Wall And Monotonic Audit

- Inject rollback, forward jump, suspend-like pause, frozen wall time, and
  independent monotonic progress.
- Verify elapsed measurement never silently uses rollback-prone wall arithmetic.
- Prove persistence/serialization examples intentionally lose monotonic data.
- Test `testing/synctest` composition, quiescence, and fake-time behavior.

### Resource And Security Audit

- Enforce active-object, fan-out, recursion, tag, diagnostic, and advancement
  budgets.
- Fuzz durations, operations, lifecycle sequences, callbacks, and limit edges.
- Verify no runtime patching, global clock mutation, unsafe, cgo, or `go:linkname`.
- Ensure observations cannot disclose callback values or create unbounded labels.

## Required Deliverables

- Formal timer/ticker/manual-clock state and ordering tables.
- Wall/monotonic semantic guide and synctest compatibility matrix.
- Race, leak, fuzz, mutation, callback, and differential evidence.
- Resource budgets and cold/contended benchmark baselines.
- Updated API, testing, migration, security, FAQ, and troubleshooting docs.

## Release Blockers

- Any missed, duplicate, or wrongly ordered firing; timer/ticker contract drift;
  race; deadlock; leak; corrupted state; unbounded callback loop; or panic from
  package-owned behavior.
- Any elapsed-time result silently corrupted by wall-clock rollback.
- Any global process clock mutation or runtime patching.
- Missing meaningful 100% coverage, mutation evidence, or green blocking CI.

## Completion Criteria

- Timer, ticker, advancement, callback, jump, and synctest suites pass.
- Concurrency and resource budgets survive stress and race execution.
- Fuzz, mutation, leak, vulnerability, compatibility, and performance gates pass.
- NilAway runs visibly as advisory without blocking findings.
- No release blocker remains and the changelog is current.
