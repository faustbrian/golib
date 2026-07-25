# Goal: `circuit-breaker`

## Objective

Build a production-grade open source Go circuit-breaker module for protecting
calls to unreliable or overloaded dependencies with deterministic admission,
bounded state, explicit outcome classification, safe half-open probing, and
low-overhead concurrent operation.

The package MUST be protocol-neutral and suitable for HTTP, RPC, databases,
queues, object storage, filesystem gateways, and arbitrary Go functions. It
MUST preserve caller control over contexts, errors, fallbacks, retries, and
timeouts rather than becoming a general resilience framework.

## Product Position

`circuit-breaker` owns:

- closed, open, and half-open state transitions
- execution admission and rejection
- count-based and time-based rolling outcome windows
- consecutive-failure, failure-rate, and slow-call policies
- half-open probe concurrency and recovery decisions
- outcome classification and ignored outcomes
- immutable state and metrics snapshots
- transition events and observability integration points
- deterministic clocks and test support

It MUST NOT own:

- retries, backoff, request replay, or idempotency
- operation timeouts, hedging, fallbacks, or bulkheads
- HTTP status policy, RPC code policy, or vendor business rules
- rate limiting, queues, durable scheduling, or workflow orchestration
- service discovery, load balancing, health checking, or Kubernetes lifecycle
- a global singleton registry or hidden process-wide breaker state

These boundaries allow `http-client`, `service`, and concrete vendor
clients to compose the breaker without duplicating its state machine.

## State Model

### Closed

- Eligible executions are admitted.
- Classified outcomes update the configured rolling window and counters.
- The breaker opens only when minimum throughput and a configured opening rule
  are satisfied.
- Ignored outcomes do not affect success, failure, slow-call, or throughput
  counts unless a caller explicitly defines otherwise.

### Open

- New executions fail fast without invoking the protected operation.
- The rejection error exposes breaker identity, state, generation, and safe
  timing metadata while preserving `errors.Is` support.
- After the configured open interval, exactly the permitted number of callers
  may transition into half-open probing.
- Open duration MAY use a bounded schedule, including fixed or exponential
  durations, with explicit reset behavior after recovery.

### Half-Open

- Probe concurrency is strictly bounded.
- Excess callers fail fast or wait only under an explicit context-aware policy.
- Recovery requires a configured number or ratio of classified successful
  probes.
- A configured failure condition reopens the breaker immediately or after the
  required probe sample according to explicit policy.
- Completions from a previous state generation MUST NOT corrupt current state.
- Closing starts a fresh closed-state window unless another documented policy is
  selected explicitly.

## Administrative Modes

The package SHOULD support explicit operational controls without overloading
normal state:

- force-open: reject all new work until released
- disabled: admit work without recording outcomes
- isolated: reject work for operator-controlled maintenance
- reset: return to a new closed generation with empty counters

Administrative transitions MUST be explicit API calls, observable, reversible,
and distinguishable from policy-driven transitions. They MUST NOT be driven by
package-global environment variables or hidden background configuration.

## Opening Policies

### Consecutive Failures

- Open after a configured number of consecutive classified failures.
- A classified success resets the consecutive-failure count.
- Ignored outcomes have explicit reset-or-preserve behavior, defaulting to
  preserve.

### Count-Based Window

- Track the last bounded number of classified outcomes.
- Open based on failure count, failure ratio, slow-call count, slow-call ratio,
  or an explicit composition of those rules.
- Enforce a minimum sample size before ratio-based opening.
- Use fixed memory independent of lifetime execution count.

### Time-Based Window

- Track outcomes in fixed-duration buckets over a bounded rolling interval.
- Bucket rollover MUST be deterministic under idle periods and clock jumps.
- Memory MUST be bounded by configured bucket count, not request volume.
- Expired outcomes MUST stop affecting decisions and metrics predictably.

Opening rules MUST be validated at construction. Ambiguous combinations,
impossible thresholds, zero windows, overflow, and unbounded settings MUST fail
before the breaker accepts traffic.

## Outcome Classification

Every completed admitted execution is classified as exactly one of:

- success
- failure
- ignored

Slow is an orthogonal property that may accompany success or failure. The
classifier receives the result, error, and elapsed duration where applicable.

- Default classification MUST be conservative and documented.
- `nil` error defaults to success and non-`nil` error defaults to failure.
- Context cancellation and deadline errors MUST have caller-selectable policy;
  the package cannot infer whether the dependency caused them.
- Panic handling MUST be explicit. The convenience executor SHOULD record the
  panic as failure and re-panic with the original value and stack behavior.
- Classifier panic MUST NOT deadlock or corrupt breaker state.
- Protocol-specific packages define HTTP status, RPC code, SQL, queue, and
  vendor error classification.
- Classifiers MUST NOT retain large results or secret-bearing errors.

## Slow-Call Detection

- A configurable duration threshold classifies admitted completions as slow.
- Slow-success and slow-failure counts remain distinguishable.
- Slow-call rate MAY independently open the breaker after minimum throughput.
- Timing uses a monotonic-capable clock abstraction where available.
- The breaker observes duration but MUST NOT enforce an operation timeout.

## Execution APIs

### Convenience Execution

- Generic `Execute` APIs accept context-aware functions and preserve typed
  results and original errors.
- Rejection MUST NOT invoke the protected function.
- The breaker MUST record every admitted completion exactly once.
- Caller cancellation before admission MUST not consume a probe or count as a
  dependency failure.
- Cancellation after admission is classified by explicit policy.

### Two-Step Admission

A low-level permit API MUST support operations whose lifecycle cannot fit a
single callback:

1. request a permit
2. execute caller-owned work if admitted
3. report exactly one classified completion

- Permits MUST be bound to breaker generation and admission state.
- Completion MUST be idempotence-safe or reject duplicate completion without
  double counting.
- Abandoned permits MUST have explicit expiry or cancellation handling.
- Half-open capacity MUST be released after completion, cancellation, or bounded
  expiry.
- A stale permit MUST never mutate a newer state generation.

## Concurrency And Correctness

- All exported operations MUST be safe for concurrent use.
- Admission and transitions MUST be linearizable at documented points.
- At most the configured number of half-open probes may be active.
- Opening under contention MUST create one transition generation and one event.
- Callback, classifier, listener, logger, and metric code MUST never execute
  while internal state locks are held.
- Slow listeners MUST not block admission unless synchronous delivery is an
  explicitly selected policy.
- Event queues, if provided, MUST be bounded with documented overflow behavior.
- No goroutine is created per execution.
- Core SHOULD require no permanent background goroutine.
- Snapshot reads SHOULD avoid contending with protected-operation latency.

## Configuration

Configuration MUST be immutable after construction and include explicit values
for:

- stable breaker name or caller-provided identity
- opening policy and rolling-window parameters
- minimum throughput
- failure and slow-call thresholds
- slow-call duration
- open-duration policy and bounds
- half-open maximum probes and recovery policy
- optional half-open waiting and maximum wait
- outcome classifier
- clock and randomness where duration jitter is enabled
- transition observer and event-delivery policy

Defaults MUST be safe, finite, documented, and versioned under SemVer. The API
SHOULD use typed builders or options that make invalid combinations difficult,
while construction always performs complete validation.

## Errors

Provide stable typed errors for:

- open rejection
- force-open or isolated rejection
- half-open probe exhaustion
- canceled or expired permit
- duplicate permit completion
- invalid configuration
- internal observer failure where surfaced

Errors MUST support `errors.Is`/`errors.As`, preserve safe causes, avoid secret or
result formatting, and remain distinguishable from the protected operation's
own error.

## Metrics And State Snapshots

Immutable snapshots MUST expose bounded operational state:

- current state, administrative mode, and generation
- transition count and last transition time
- current window size and minimum throughput
- success, failure, ignored, slow-success, and slow-failure counts
- admitted, rejected, and active half-open probe counts
- failure and slow-call ratios when defined
- current open duration and next eligible probe time

Snapshots MUST be internally consistent and safe for concurrent readers. They
MUST NOT expose retained operation values, error messages, secret-bearing labels,
or unbounded per-call history.

## Observability

- Transition observers receive immutable before/after state, reason, generation,
  timestamp, and bounded aggregate metrics.
- Optional adapters MAY integrate `log/slog`, OpenTelemetry, and `telemetry`.
- Core MUST NOT require a logger, tracer, metrics backend, or global registry.
- Metric labels MUST be low-cardinality and caller controlled.
- Repeated open rejections MUST not produce unbounded logs or spans by default.
- Observer panic or failure MUST not corrupt breaker state or protected results.
- An explicit registry MAY aggregate caller-owned breakers for dashboards and
  health reporting without owning their lifecycle.

## Composition Rules

The package documentation MUST explain policy order rather than pretending
composition is commutative.

- Retries normally execute inside one breaker-observed logical operation when
  the desired signal is final operation failure.
- A breaker may instead observe each attempt only when callers intentionally
  want attempt-level dependency health; nested breaker recording is forbidden.
- Rate limiting usually occurs before breaker admission so rejected traffic does
  not consume probes.
- Bulkhead rejection is normally ignored by a dependency breaker unless it
  represents dependency saturation by explicit policy.
- Cache hits and local fallbacks should not consume breaker permits.
- Timeouts and caller cancellation require explicit outcome classification.
- Multiple breakers MAY protect distinct dependency boundaries, but one failure
  MUST NOT be recorded into the same breaker more than once.

`http-client` owns the canonical HTTP composition and status classifier.
Other integration packages own equivalent protocol-specific adapters.

## Distributed Systems Position

Breakers are process-local by default. A Kubernetes replica SHOULD make local
admission decisions based on its own observed dependency path.

- Core MUST NOT require Redis, Valkey, PostgreSQL, NATS, or another coordinator.
- A shared distributed breaker can become a latency dependency and correlated
  failure source, so it is not a v1 core feature.
- Aggregated metrics and control-plane visibility MUST remain separate from
  synchronous admission decisions.
- Future distributed adapters require a dedicated consistency model, fencing,
  split-brain behavior, latency budget, and failure-mode goal before acceptance.

## Package Shape

- root package: breaker, configuration, state, outcomes, policies, permits,
  snapshots, errors, and observers
- `window`: count- and time-based bounded rolling data structures
- `breakerhttp`: optional HTTP adapter only if dependency direction remains
  acyclic; otherwise HTTP integration remains in `http-client`
- `breakergrpc`: optional future gRPC adapter
- `breakertest`: fake clock, scripted classifier, transition recorder, model
  assertions, and deterministic concurrency helpers
- `otel`: optional OpenTelemetry adapter

Core SHOULD be standard-library-only. Optional integrations MUST not inflate the
core dependency graph.

## Non-Goals

- No all-in-one resilience policy executor.
- No hidden retries, timeouts, fallback, hedging, rate limiting, or concurrency
  limiting.
- No distributed state backend in v1.
- No automatic dependency discovery or global breaker creation by string name.
- No persistence of transient window state across process restarts.
- No arbitrary execution history or high-cardinality per-call metrics.
- No background health polling of protected dependencies.
- No protocol-specific classification in core.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Tests MUST prove
state-machine and concurrency behavior rather than merely execute lines.

Required verification includes:

- exhaustive closed/open/half-open/admin transition tables
- consecutive, count-window, and time-window policy truth tables
- minimum-throughput and exact threshold boundary tests
- failure-rate and slow-call-rate combinations
- permit generation, duplicate completion, abandonment, expiry, and cancellation
- fake-clock tests for bucket rollover, idle gaps, clock movement, open duration,
  jitter, and half-open eligibility
- classifier success/failure/ignored/slow/panic matrices
- model-based and property tests comparing implementation with a simple reference
  state machine
- race tests with highly contended admission, completion, transition, reset,
  force-open, snapshots, and observers
- deterministic scheduler-assisted interleaving tests where practical
- fuzzing for configurations, outcome sequences, durations, timestamps, and
  permit operations
- leak tests for goroutines, timers, events, permits, and observer delivery
- benchmarks for closed admission, open rejection, window rollover, snapshots,
  half-open contention, and transition observers
- integration tests through `http-client` and representative non-HTTP calls

## Documentation Deliverables

- README and five-minute quickstart
- complete public API and configuration reference
- state-machine diagram and exact transition tables
- consecutive, count-window, time-window, failure-rate, and slow-call recipes
- outcome-classification and context-cancellation guide
- convenience execution and two-step permit guides
- HTTP, RPC, PostgreSQL, Valkey, queue, and object-storage examples
- retry, rate-limit, timeout, bulkhead, cache, and fallback composition guide
- Kubernetes and multi-replica operational guidance
- metrics, tracing, logging, dashboards, and alerting guide
- manual control and incident-response runbook
- tuning, performance, security, troubleshooting, migration, compatibility, and
  FAQ documentation
- runnable examples for every user-facing API and policy
- maintained `CHANGELOG.md` for every user-visible or compatibility change

## Repository And Release Requirements

- GitHub Actions for format, vet, lint, unit/integration tests, meaningful 100%
  coverage, race, fuzz smoke tests, leak tests, benchmarks, docs, examples,
  `govulncheck`, dependency review, API compatibility, and tagged releases
- `make safety` and `make check` commands matching CI
- `GO-SAFETY-1`, with no production `unsafe`, cgo, or `go:linkname`
- complete OSS governance, security, contribution, attribution, and release
  files
- strict `CHANGELOG.md` maintenance for every implementation task
- SemVer treatment of exported APIs, defaults, state transitions, timing,
  classifiers, snapshots, and errors

## Execution Plan

1. Freeze state, outcome, generation, transition, timing, and error contracts.
2. Implement configuration validation and deterministic clock/test seams.
3. Implement consecutive, count-based, and time-based policies against a
   reference model.
4. Implement execution, permits, administrative modes, snapshots, and observers.
5. Prove linearizability, half-open bounds, stale-generation safety, and resource
   ownership under race and model testing.
6. Integrate with `http-client` and representative non-HTTP clients.
7. Complete fuzzing, leak testing, benchmarks, compatibility, and security audit.
8. Publish complete API, adoption, composition, operations, and tuning docs.
9. Release `v1` only after every acceptance and hardening criterion passes.

## Acceptance Criteria

- Every state transition and rejection has deterministic executable evidence.
- Consecutive, count-based, time-based, failure-rate, and slow-call policies are
  bounded, validated, and concurrency-safe.
- Half-open admission never exceeds its configured bound and stale completions
  cannot corrupt a later generation.
- Both convenience execution and two-step permits preserve caller results,
  errors, contexts, and resource ownership.
- Core remains protocol-neutral and standard-library-first.
- `http-client` composes the package without duplicating breaker state.
- Metrics and diagnostics expose useful bounded state without operation data or
  secrets.
- Meaningful 100% coverage and every CI, race, fuzz, leak, benchmark, security,
  compatibility, documentation, and release gate pass.
- Documentation enables correct adoption and operations without source reading.
- `CHANGELOG.md` and the compatibility policy are current.
