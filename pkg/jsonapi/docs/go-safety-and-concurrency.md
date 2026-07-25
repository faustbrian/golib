# Go Safety And Concurrency

Baseline: `GO-SAFETY-1`

This document defines the shared safety engineering standard for the
`faustbrian/go-*` package family. RFC 2119 keywords are requirements.

## Guarantee Boundary

Go provides memory safety for ordinary Go code, but it does not provide Rust's
compile-time ownership and data-race proof. The race detector observes executed
paths only. These rules reduce the practical gap through constrained design,
static analysis, adversarial tests, and executable quality gates; they MUST NOT
be described as an equivalent type-system guarantee.

## Memory And Ownership

- Production code MUST NOT import `unsafe`, use cgo, or use `go:linkname`.
  An exception requires an explicit security review, package-local rationale,
  isolated API boundary, platform matrix, and dedicated tests before the source
  gate may be changed.
- Every mutable value shared across goroutines MUST have a documented owner and
  synchronization strategy.
- APIs MUST NOT expose mutable internal maps, slices, buffers, registries, or
  pointers unless ownership transfer or aliasing is an intentional, documented
  contract.
- Caller-owned mutable input MUST NOT be retained or mutated unless the public
  API explicitly documents that behavior.
- Copies, immutable snapshots, atomics, channels, or mutexes SHOULD be chosen
  according to the invariant being protected, not as interchangeable style.
- Pooling and zero-copy techniques MUST NOT weaken ownership clarity, retain
  secrets unexpectedly, or allow values to outlive their backing storage.

## Goroutine And Resource Lifecycle

- Every goroutine MUST have an identifiable owner, bounded lifetime, and
  deterministic stop condition.
- Library code MUST NOT start hidden background work without a documented
  shutdown or cancellation mechanism.
- Request-scoped work MUST propagate the caller's `context.Context`; it MUST
  NOT replace that context with `context.Background()`.
- Concurrency, queues, retries, buffers, payloads, recursion, and fan-out MUST
  be bounded at every untrusted or operational boundary.
- The goroutine that owns a channel's send lifecycle SHOULD close it. Receivers
  MUST NOT close channels they do not own.
- Locks MUST NOT be held while calling user callbacks, performing network I/O,
  or waiting on unrelated goroutines.
- Multiple-lock code MUST document and test a stable lock order.
- Cancellation, timeout, startup failure, partial initialization, panic, and
  repeated shutdown MUST each release timers, goroutines, connections, files,
  transactions, and other owned resources exactly once.

## Required Verification Layers

### Static And Source Analysis

- `make safety` MUST reject forbidden low-level features and run vet, lint,
  race, and fuzz gates.
- `go vet`, Staticcheck, security linting, error checks, context checks, and
  HTTP-body ownership checks MUST remain enabled through the repository lint
  configuration.
- New linter suppressions MUST be narrow, justified next to the suppression,
  and covered by tests where behavior is affected.

### Behavioral Tests

- Tests MUST prove observable contracts, invariants, errors, and ownership
  behavior rather than merely execute lines.
- Meaningful 100% production statement coverage remains required, but coverage
  MUST NOT be treated as evidence of race freedom, resource bounds, or protocol
  conformance by itself.
- Cancellation, timeout, panic, partial failure, retry, and shutdown paths MUST
  be tested when the package owns those behaviors.
- Tests for mutable inputs and outputs MUST check aliasing and post-call
  mutation where ownership could be ambiguous.

### Concurrency And Race Tests

- The full package suite MUST pass under `go test -race ./...`.
- Every concurrency bug fix MUST include a regression that exercises the
  relevant interleaving under the race detector.
- Concurrency tests MUST use barriers, channels, hooks, wait groups, or other
  deterministic coordination. Sleeps MUST NOT be used as synchronization;
  they MAY be used only when elapsed time is the behavior under test.
- Critical state machines SHOULD be stress-tested with repeated execution and
  varied scheduling. A passing stress run is supporting evidence, not a
  substitute for a deterministic regression.
- Tests MUST cover concurrent use of every API documented as concurrency-safe.

### Fuzz And Adversarial Tests

- Every parser, decoder, protocol boundary, query parser, queue envelope, and
  other attacker-controlled byte or text boundary MUST have a fuzz target.
- Fuzz corpora MUST include valid examples, malformed inputs, boundary values,
  previously discovered regressions, and inputs near configured resource
  limits.
- Fuzz properties MUST include no panic, no hang, bounded resource behavior,
  deterministic classification, and no invalid caller-state mutation.
- Round-trip properties MUST be used only where the format and API promise a
  lossless round trip.
- Every fuzz-discovered defect MUST be retained as a deterministic regression
  and, where useful, as a permanent fuzz seed.

### Leak And Resource-Bound Tests

- Components that create goroutines, timers, files, sockets, subscriptions, or
  transactions MUST prove cleanup after success, failure, cancellation, panic,
  and repeated close.
- Leak tests SHOULD use observable lifecycle hooks or owned-resource counters.
  Global goroutine counts alone SHOULD NOT be used as exact assertions because
  unrelated runtime goroutines make them unstable.
- Hostile-reader and declared-size tests MUST prove that byte, item, depth,
  retry, and expansion limits stop work at the documented boundary.
- Queue backends MUST test acknowledgement, retry, redelivery, poison-message,
  reconnect, and shutdown behavior against real backends where semantics
  cannot be proved hermetically.

### Benchmarks And Regression Discipline

- Hot paths MUST have representative benchmarks reporting allocations with
  `-benchmem`.
- Security-sensitive parsers and state machines SHOULD include adversarial
  benchmarks near supported limits.
- Performance changes MUST compare multiple before-and-after samples with
  `benchstat` or an equivalent statistical method and retain the commands,
  environment, and result.
- Shared CI MUST NOT fail on small wall-clock benchmark variance. Stable
  allocation counts, explicit resource ceilings, and statistically supported
  material regressions MAY be gated.
- Optimization MUST follow profiling evidence and MUST NOT remove validation,
  bounds, cancellation, or ownership clarity without equivalent protection.

## Review And Change Control

- Public concurrency guarantees, aliasing behavior, limits, and lifecycle
  semantics are SemVer-governed contracts.
- Changes touching shared state, goroutines, callbacks, retries, settlement,
  parsing, or resource limits MUST include an explicit safety review in the
  pull request.
- Updates to this baseline MUST bump its identifier and be propagated across
  `jsonapi`, `jsonrpc`, `queue`, `wire`, `tabular`,
  and the monorepo Go standard.
