# API and configuration reference

`New(Config)` validates the entire configuration before traffic is admitted and
copies all value configuration into immutable internal state. A classifier,
clock, random source, and observer are caller-owned functions/interfaces and
must remain concurrency-safe.

`Name` is UTF-8-unaware opaque identity bounded to `MaxNameBytes` bytes. Window,
probe, and event allocations have exported hard limits. Count-window failure
and slow-count rules cannot exceed the window size because they could never be
satisfied.

## Defaults

Only `Name` is required.

| Setting | Default |
| --- | --- |
| Window | last 100 classified outcomes |
| Minimum throughput | 10 |
| Opening rule | failure ratio at least 0.5 |
| Slow-call duration | 30 seconds |
| Open duration | fixed 30 seconds |
| Half-open | 10 probes, all 10 successes, reopen immediately |
| Permit TTL | 5 minutes |
| Excess half-open admission | reject immediately |
| Classifier | nil error succeeds; non-nil error fails |
| Observer delivery | bounded async buffer 64, drop newest |
| Jitter | none |

Supplying an observer without `EventDelivery` selects the observer default.
Without an observer, no worker is created. `SynchronousEvents` explicitly runs
the observer in the transitioning caller after the state lock is released.

## Windows and opening rules

`CountWindow{Size}` retains the newest classified outcomes. Ignored outcomes
increment the ignored diagnostic count but do not evict health samples.

`TimeWindow{BucketDuration, BucketCount}` retains bounded bucket aggregates for
`BucketDuration * BucketCount`. Idle gaps and backward clock movement are
handled deterministically.

`OpeningRules` enables any combination of consecutive failures, failure count,
failure ratio, slow count, and slow ratio. Zero disables a rule. `OpenWhenAny`
or `OpenWhenAll` composes enabled rules. Every rule waits for
`MinimumThroughput`; ratio comparisons are inclusive.

## Open and half-open policy

`FixedOpenDuration` uses one interval. `ExponentialOpenDuration` multiplies the
interval after each failed recovery, caps it at `Maximum`, and resets escalation
after recovery. `OpenDurationJitter` is a downward fraction in `[0,1)` and uses
the configured `Random` source.

`HalfOpenPolicy` limits the complete recovery sample with `MaxProbes`. Select
exactly one of `RequiredSuccesses` or `SuccessRatio`. `ReopenImmediately` reacts
to the first classified failure; `ReopenAfterSample` waits for the bounded
sample. Ignored probes release active capacity and may be replaced.

`RejectExcessProbes` fails fast. `WaitForProbe{MaxWait}` waits for capacity or a
state change until the caller context or maximum wait ends. Waiting never
consumes a permit, has no FIFO fairness guarantee, and is bounded by an absolute
deadline even if timer delivery is delayed.

## Execution and permits

`Execute[T]` calls `Acquire`, times the operation with the configured clock,
classifies the result, and completes the permit. Rejection does not call the
operation. Operation errors are returned unchanged. An operation or classifier
panic is recorded as a failure and re-panicked with the same value.
The classifier's ephemeral `Completion.Context` is the caller context used for
admission and execution. It lets integration policy distinguish caller
cancellation from a dependency that independently returns a cancellation-shaped
error. Classifiers must not retain the context, result, or error.
Clock panics after admission cannot strand a permit or replace an existing
operation/classifier panic; terminal cleanup uses the last safe admission or
start timestamp before re-panicking.
An invalid classifier enum returns `InvalidOutcomeError` after canceling the
permit, so half-open capacity is not stranded. Cancellation is checked after
the clock read while admission is serialized; a context canceled before that
linearization point cannot invoke protected work.

`Acquire` returns a generation-bound `Permit`. Call exactly one of:

- `Complete(outcome, slow)` for finished caller-owned work;
- `Cancel()` for work that will not complete.

Permit completion is duplicate-safe. Expired, canceled, and already-completed
permits return stable sentinel errors. A stale permit records its lifetime
outcome exactly once but is a no-op against the new generation's health window
and transition policy. Permit expiry is reclaimed on permit use, admission, or
snapshot; core does not run a background reaper.
Permit TTL bounds abandoned two-step permits. It does not discard a terminal
`Execute` outcome: a long-running convenience call records exactly one lifetime
completion after TTL while preserving its original result, error, or panic.

## Administrative control and lifecycle

`ForceOpen` and `Isolate` reject. `Disable` admits without health-policy
recording while retaining lifetime completion totals. `Release` returns to
normal policy operation. `Reset` creates a new closed generation, normal mode,
and empty window. `SetMode` is the common typed API.

`Close` idempotently requests asynchronous observer shutdown without waiting,
so it is safe inside that callback. `Shutdown(ctx)` requests shutdown and waits
for the bounded queue and callback to finish; the owner should use it for
deterministic cleanup and must not invoke it from the async callback. Neither
method closes admission. A breaker without async observation needs no cleanup.

## Snapshots, events, and errors

`Snapshot` contains state, mode, generation, transition timing, bounded window
aggregates, admissions, rejections, lifetime completion/outcome totals,
half-open progress, ratio definedness, open timing, observer failures, and
dropped events. It contains no result or error.

Use `errors.Is` with `ErrOpen`, `ErrHalfOpenExhausted`,
`ErrHalfOpenWaitTimeout`, `ErrForceOpen`, `ErrIsolated`, permit sentinels,
`ErrInvalidOutcome`, and `ErrInvalidConfig`. Use `errors.As` for
`RejectionError`, `InvalidConfigError`, or `InvalidOutcomeError`. A rejection
contains only breaker identity, state, mode, generation, and retry time.

The `window` package exposes bounded `Count` and `Time` structures. The
`breakertest` package exposes a deterministic clock/timer, bounded transition
recorder, and scripted classifier.
