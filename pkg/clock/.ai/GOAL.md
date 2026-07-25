# Goal: Deterministic Clock And Timer Foundation

## Objective

Build a production-grade open-source clock package for obtaining wall time,
measuring elapsed time, creating timers and tickers, and testing time-dependent
and concurrent Go code deterministically.

The package MUST replace duplicated clock abstractions across the owned Go
libraries without wrapping calendar arithmetic, interval algebra, scheduling,
or every function in the standard `time` package.

## Product Principles

- Standard-library `time.Time` and `time.Duration` remain the public value types.
- Small capability interfaces are preferred over one mandatory broad clock.
- Wall time, monotonic elapsed time, timers, tickers, and sleeping have distinct
  semantics and interfaces.
- Production behavior delegates predictably to the standard library.
- Tests can control time without real sleeps, scheduler guesses, or global
  mutable `SetTestNow` state.
- Advancing fake time MUST provide deterministic synchronization with triggered
  work.
- Every timer, ticker, callback, goroutine, and waiter has explicit ownership.

## Core Interfaces

- `Clock` exposes only `Now() time.Time`.
- `ElapsedClock` provides monotonic-safe `Since` and elapsed measurement.
- `Sleeper` provides context-aware bounded sleep.
- `TimerFactory` creates owned timers through a package timer interface.
- `TickerFactory` creates owned tickers through a package ticker interface.
- `CallbackClock` MAY expose `AfterFunc` with explicit callback lifecycle.
- Consumers depend on the narrowest interface they require.
- Composite interfaces MAY exist as conveniences but MUST NOT be required by
  packages that only need `Now`.

## System Clock

- `System` delegates to `time.Now`, `time.NewTimer`, `time.NewTicker`, and
  related standard-library behavior.
- Returned `time.Time` values preserve monotonic readings where the standard
  library provides them.
- UTC conversion is explicit; `Now` MUST NOT silently discard location or
  monotonic data.
- Timer and ticker reset, stop, drain, callback, and channel semantics are
  documented against the supported Go version.
- Context-aware sleep MUST stop and release timers on cancellation.

## Fixed And Manual Clocks

- Immutable fixed clock for deterministic timestamps.
- Concurrency-safe manual clock with explicit starting time.
- Forward advancement to a target or by a duration.
- Optional wall-clock jump simulation independent of monotonic advancement.
- Scheduled timers, tickers, sleepers, and callbacks fire in deterministic
  timestamp and registration order.
- Advancement returns a waiter/result that can synchronize all triggered work.
- Callback reentrancy MUST not deadlock or run while internal locks are held.
- Same-instant events, timer reset, stop races, ticker backpressure, and newly
  scheduled work during callbacks have explicit semantics.
- Backward wall-clock movement MUST be modeled separately and MUST NOT
  accidentally reverse monotonic elapsed time.

## Go testing/synctest Integration

- Document `testing/synctest` as the preferred standard-library mechanism when
  whole-test time virtualization is sufficient.
- Provide helpers or examples that compose with `testing/synctest` without
  replacing or fighting its fake-time bubble.
- Explain when dependency-injected clocks remain necessary: explicit business
  timestamps, selected wall-clock scenarios, cross-package contracts, clock
  jumps, and code not wholly contained in a synctest bubble.
- Do not duplicate synctest's goroutine quiescence model in core without a
  demonstrated semantic requirement.

## Wall Time And Elapsed Time

- Public documentation distinguishes civil/wall time from monotonic elapsed
  measurement.
- Helpers MUST not strip monotonic readings before elapsed comparisons unless
  explicitly required for persistence or wire encoding.
- Persisted and serialized timestamps are wall-clock values and cannot retain
  Go's process-local monotonic component.
- Clock rollback, NTP adjustment, suspend/resume, and large jumps are supported
  as explicit test scenarios.
- Distributed ordering and correctness MUST NOT rely on synchronized clocks;
  fencing and version semantics belong to `lease` and related packages.

## Instrumentation And Diagnostics

- Optional hooks report timer kind, outcome, requested duration, elapsed
  duration, and bounded tags without callback payloads or sensitive values.
- Instrumentation callbacks are isolated, bounded, and never hold internal
  locks.
- No package-global registry, background telemetry exporter, or hidden goroutine.
- Debug snapshots are bounded and disabled by default in production.

## Integration Targets

- Standardize clock seams in `cache`, `circuit-breaker`,
  `authentication`, `authorization`, `http-client`, `idempotency`,
  `lease`, `rate-limit`, `scheduler`, and `service`.
- Packages that only need timestamps consume `Clock`, not timer APIs.
- `calendar` consumes wall-time values but owns civil-calendar arithmetic.
- `temporal` consumes timestamps and durations but owns interval algebra.
- Adoption MUST be incremental and MUST preserve each package's public SemVer
  contract until a planned major release permits change.

## Security And Resource Bounds

- Bound active timers, tickers, callbacks, waiters, tags, diagnostics, and work
  processed by one manual advancement.
- Detect or reject duration overflow and impossible advancement.
- Manual clocks MUST not leak goroutines, channels, timers, or callback state.
- Callback panics have an explicit policy and MUST not corrupt clock state.
- Production code MUST NOT use `unsafe`, cgo, `go:linkname`, or runtime patching.
- Fake clocks MUST NOT mutate the process-wide standard-library clock.

## Non-Goals

- No Carbon-style fluent date API, natural-language parsing, date-only type,
  calendar arithmetic, holiday calendar, timezone database wrapper, scheduler,
  cron engine, period algebra, distributed clock, or timestamp oracle.
- No global `SetTestNow` or monkey patching.
- No claim that wall clocks are monotonic or synchronized across processes.
- No mandatory dependency from simple applications that can use `time` and
  `testing/synctest` directly.

## Package Shape

- Root: capability interfaces, system implementation, errors, and observations.
- `manual`: fixed/manual clock, timers, tickers, callbacks, advancement waiters.
- `clocktest`: deterministic assertions, fixtures, and synctest helpers.
- Optional integration packages only when they avoid dependency cycles.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Tests MUST prove
timing, ordering, ownership, and concurrency semantics rather than only execute
lines.

Required evidence includes:

- state-machine tests for timer and ticker create, fire, stop, reset, and drain
- deterministic same-instant ordering and callback-scheduled work
- race tests for concurrent advance, reset, stop, wait, callback, and shutdown
- wall rollback, monotonic progress, overflow, negative, zero, and huge duration
  properties
- callback panic, reentrancy, cancellation, and resource-limit fuzzing
- differential system behavior against the supported standard library
- `testing/synctest` interoperability suites
- goroutine and timer leak tests
- benchmarks for system calls, manual scheduling, large timer heaps, contention,
  allocations, and advancement fan-out

## Documentation Deliverables

- Five-minute system, fixed-clock, manual-clock, and synctest quickstarts.
- Complete interface, timer, ticker, callback, advancement, and error API docs.
- Guides for wall versus monotonic time, cancellation, reset/stop, deterministic
  concurrency tests, clock jumps, observations, and package integration.
- Migration guide for existing package-local clock abstractions.
- Security model, performance guide, FAQ, troubleshooting, compatibility,
  examples, contribution guide, and maintained changelog.
- Every exported API and realistic user-facing scenario MUST be documented.

## Automation And Release

Use the latest stable Go release as the minimum at implementation time. Pin all
tools and dependencies. GitHub Actions MUST run formatting, vet, Staticcheck,
strict golangci-lint, advisory NilAway, tests, meaningful 100% coverage, race,
fuzz smoke, mutation checks, leak tests, supported-platform matrices,
vulnerability scans, benchmarks, docs, API compatibility, and releases. Every
blocking command MUST be reproducible locally through documented `make` targets.

## Execution Plan

1. Specify capability interfaces, system behavior, lifecycle, and error model.
2. Implement fixed/manual time and deterministic timer/ticker state machines.
3. Implement callback synchronization, clock jumps, limits, and observations.
4. Prove synctest composition and migrate two representative owned packages.
5. Complete race, fuzz, mutation, leak, and performance hardening.
6. Publish complete adoption documentation and release v1.

## Acceptance Criteria

- Consumers can depend on narrow interfaces without a broad framework.
- Manual time advancement and triggered work are deterministic and race-safe.
- Wall and monotonic semantics remain explicit and correct.
- System behavior follows the supported standard library without hidden state.
- At least two existing packages adopt the library without semantic regression.
- Meaningful 100% coverage and every required GitHub Actions gate pass.
