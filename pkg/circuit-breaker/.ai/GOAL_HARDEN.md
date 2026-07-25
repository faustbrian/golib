# Goal Harden: `circuit-breaker`

## Mission

Perform an evidence-driven state-machine, concurrency, timing, API, security,
observability, integration, and resource-ownership audit of
`circuit-breaker`, then implement every justified correction required for
hostile production concurrency and dependency failure.

Hardening MUST prove more than race-detector silence. Admission, transition,
generation, permit, window, and snapshot behavior require executable models and
documented linearization points.

## Authoritative Inputs

- `.ai/GOAL.md`, public APIs, defaults, docs, examples, tests, fuzzers,
  benchmarks, dependencies, workflows, and changelog
- Go memory model, `context`, `errors`, `sync`, `sync/atomic`, monotonic time, and
  timer contracts used by the implementation
- circuit-breaker state-machine literature and documented behavior from mature
  Go and cross-language implementations, used as comparison rather than copied
  semantics
- integration contracts from `http-client` and representative RPC,
  PostgreSQL, Valkey, queue, and storage consumers

Every behavior MUST be classified as package guarantee, configurable policy,
observer behavior, integration responsibility, or caller responsibility.

## Phase 1: Baseline And Threat Model

1. Inventory every exported type, option, default, state, administrative mode,
   transition, classifier, window, counter, permit, snapshot, observer, error,
   test helper, and integration seam.
2. Draw the exact admission and completion lifecycle for convenience and
   two-step APIs, including every lock and atomic operation.
3. Identify and document linearization points for admission, open, half-open,
   close, reset, force-open, permit completion, and snapshot capture.
4. Run format, vet, lint, tests, exact coverage, race, fuzz, leak, benchmark,
   docs, vulnerability, compatibility, and workflow gates before modification.
5. Threat-model dependency outage, latency collapse, retry storms, thundering
   probes, scheduler pauses, clock movement, callback panic, abandoned permits,
   stale completions, operator races, and telemetry overload.
6. Require a failing regression or model counterexample before every behavior
   correction.

## State-Machine Audit

- prove every valid closed/open/half-open/admin transition and reject every
  impossible transition
- verify exactly one transition generation and event under concurrent threshold
  crossings
- verify open-duration schedules, reset behavior, and half-open eligibility
- prove force-open, isolated, disabled, reset, and policy transitions cannot
  leave contradictory state or counters
- verify close and reopen window-reset semantics
- test rapid oscillation without event loss, duplicate events, or stale state
- prove state reads never observe impossible combinations of state, generation,
  probe count, transition time, or counters

## Window And Threshold Audit

- consecutive-failure reset/preserve behavior for success, failure, ignored, and
  slow outcomes
- count-window insertion, eviction, wraparound, exact capacity, and aggregate
  consistency
- time-window bucket selection, rollover, expiry, idle gaps, boundary timestamps,
  and full-window replacement
- minimum-throughput boundaries and exact failure/slow ratio comparisons
- simultaneous failure and slow-call opening rules with deterministic reason
- duration, count, ratio, multiplication, and timestamp overflow/underflow
- bounded memory and constant or documented complexity independent of lifetime
  request count
- reference-model comparison for long randomized outcome and time sequences

## Admission And Permit Audit

- closed admission concurrent with opening and administrative transitions
- exact half-open active-probe maximum under extreme contention
- fail-fast versus optional waiting behavior, fairness, deadlines, and
  cancellation
- permit binding to breaker identity, state generation, and unique completion
- duplicate, late, stale, canceled, expired, abandoned, and never-completed
  permits
- release of half-open capacity on every terminal path
- no operation invocation after rejection or pre-admission cancellation
- exactly-once recording for every admitted terminal completion
- no caller-owned operation result or error retained in breaker state

## Classifier And Execution Audit

- default and custom success, failure, ignored, slow-success, and slow-failure
  classification
- nil result/error, typed nil errors, wrapped errors, joined errors, cancellation,
  deadline, and caller-defined sentinel handling
- classifier panic before and after partial work without lock, state, or permit
  corruption
- protected-function panic recording followed by re-panic of the original value
- elapsed timing around success, failure, panic, and cancellation
- typed result and exact error preservation through execution
- classifier replacement or configuration mutation after construction is
  impossible

## Concurrency And Memory-Model Audit

- race-test admission, completion, transitions, reset, administrative controls,
  snapshot reads, observers, and shutdown in all combinations
- prove atomic publication and synchronization of state generations and immutable
  snapshots
- identify ABA risks around generations, permits, and ring-buffer slots
- test integer wrap strategy or prove practical non-overflow with explicit types
- ensure callbacks never execute under state locks
- test reentrant observer calls into snapshots and administrative APIs
- test scheduler starvation, long stop-the-world pauses, and highly skewed
  goroutine execution
- use model checking or deterministic interleaving harnesses for critical small
  state spaces where practical

## Time And Clock Audit

- monotonic versus wall-clock use and serialization boundaries
- fake-clock advancement, large jumps, backwards wall time, equal timestamps,
  timer reset, and stopped timers
- open expiry races with cancellation, reset, force-open, and concurrent callers
- time-window behavior after long idle periods without per-bucket loops
- bounded jitter generation and deterministic seeded tests
- no real sleeps in unit tests
- no leaked timers or background goroutines

## Snapshot, Metrics, And Observer Audit

- snapshots are immutable, internally consistent, bounded, and generation-aware
- aggregate counters reconcile with active window data where promised
- undefined ratios and empty windows have explicit representations
- event before/after state and transition reason match committed state
- synchronous and asynchronous observers, slow consumers, queue overflow,
  cancellation, panic, and shutdown
- low-cardinality metric names and labels with no result, error, endpoint, user,
  or secret leakage
- repeated rejection does not create log, metric, trace, or event amplification
- registry addition/removal, duplicate names, lifecycle ownership, and concurrent
  enumeration if an explicit registry exists

## Error And API Audit

- stable `errors.Is` and `errors.As` behavior for every rejection and permit
  failure
- original protected-operation errors remain unmodified and distinguishable
- no formatter, wrapper, snapshot, observer, or telemetry adapter renders
  secret-bearing operation values or errors unexpectedly
- option ordering, duplicate options, invalid combinations, defaults, zero
  values, and future-compatible extension behavior
- generic typed and untyped API consistency without reflection-based ambiguity
- no accidental source, binary, or semantic compatibility breaks

## Composition And Integration Audit

- `http-client` cache, limiter, breaker, retry, authentication, signing,
  telemetry, and transport order
- logical-operation versus per-attempt recording, with proof that one outcome is
  never recorded twice accidentally
- HTTP body closure and breaker classification without body leaks or double reads
- timeout, cancellation, bulkhead rejection, fallback, cache hit, and local
  validation classification
- PostgreSQL, Valkey, queue, RPC, and storage adapters define protocol outcomes
  outside core
- dependency graphs remain acyclic and no integration inflates core dependencies
- process-local state remains functional during telemetry and control-plane
  outages

## Performance And Resource Audit

- allocation and latency benchmarks for closed admission, open rejection,
  half-open probes, count windows, time windows, snapshots, and observers
- uncontended and highly contended benchmarks across representative CPU counts
- fixed-memory evidence for rolling windows and event buffers
- no goroutine per call, unbounded channel, unbounded history, timer per execution,
  or retained operation result
- profiles for lock contention, cache-line contention, allocation, and scheduler
  pressure
- performance budgets documented from reproducible benchmark environments
- optimization MUST NOT replace correctness evidence or introduce `unsafe`

## Fuzzing And Model Evidence

Required fuzz targets include:

- configuration combinations and threshold boundaries
- arbitrary outcome and duration sequences
- arbitrary timestamp advances and clock jumps
- admission, completion, cancel, reset, force-open, disable, isolate, and snapshot
  operation sequences
- permit duplication, abandonment, expiry, and stale generations
- observer panic and reentrant operation sequences

The reference model MUST favor clarity over performance. Divergence between the
model and optimized implementation is a release blocker until explained and
proved intentional.

## Security And Supply-Chain Audit

- no secret retention or accidental rendering through errors and observers
- denial-of-service bounds for callers, windows, events, names, and snapshots
- dependency minimization, checksums, licenses, advisories, and provenance
- no production `unsafe`, cgo, `go:linkname`, finalizers, or hidden runtime hooks
- GitHub Actions permissions, pinned actions, untrusted pull-request isolation,
  release signing/provenance, and secret scanning

## Mandatory Hardening Evidence

- meaningful 100% production statement coverage with behavior review
- exhaustive state/transition and threshold tables
- reference-model and property-test reports
- full race suite under repeated high-contention runs
- deterministic clock and interleaving evidence
- fuzz corpus for configuration, sequences, time, permits, and observers
- goroutine, timer, memory, event, and permit leak evidence
- reproducible benchmark and profile report with regression budgets
- HTTP and at least two non-HTTP integration suites
- threat model, compatibility report, and dependency audit

## Required Deliverables

1. State-machine specification with linearization points and transition tables.
2. Policy, threshold, window, timing, and classification truth tables.
3. Threat model and findings report with severity, reproduction, impact, and
   disposition.
4. Reference model, regression tests, fuzz corpus, race/leak evidence, and
   benchmark report.
5. Updated API, adoption, composition, tuning, observability, operations,
   incident-response, migration, security, FAQ, and troubleshooting docs.
6. Final release verdict with exact command evidence and remaining risks.

## Release Blockers

- impossible or nondeterministic state transition
- half-open probe count exceeding its bound
- stale or duplicate permit completion mutating current state
- admitted execution not recorded exactly once
- rejected execution invoking protected work
- threshold, minimum-throughput, ratio, or window boundary error
- mutable or internally inconsistent snapshot
- deadlock, race, panic corruption, ABA bug, or lock-held callback
- leaked goroutine, timer, event, permit, result, or error
- unbounded memory, event amplification, or per-execution background work
- secret or operation-data disclosure through diagnostics or telemetry
- duplicated breaker state machine in an integration package
- model divergence without documented proof
- missing meaningful 100% coverage or any red quality/security gate

## Completion Criteria

Hardening is complete only when every transition, policy, window, threshold,
permit, classifier, timing, snapshot, observer, and integration decision has
deterministic executable evidence; all high and medium findings are fixed or
rejected with proof; race, fuzz, leak, benchmark, compatibility, documentation,
and security gates pass without unexplained skips; and no release blocker
remains.
