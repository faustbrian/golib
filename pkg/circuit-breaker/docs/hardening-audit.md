# Hardening audit report

This report records the 2026-07-15 hostile-concurrency audit. It complements
the normative state machine in [design.md](design.md), the policy truth tables
in [policies.md](policies.md), and the reproducible gates in
[verification.md](verification.md).

## Public surface and behavior ownership

The root package exports the breaker, permit, snapshot, generic `Execute`,
configuration and policy types, state/mode/outcome enums, transition events,
observer policies, typed errors, sentinel errors, and resource limits. The
`window` package exports bounded count/time windows, records, classes,
snapshots, constructors, and limits. The `breakertest` package exports a fake
clock/timer, transition recorder, and scripted classifier. The `make docs`
package-by-package `go doc` output is the authoritative symbol inventory and a
release gate.

| Surface | Exported inventory |
| --- | --- |
| Execution and lifecycle | `New`, `Breaker`, `Acquire`, `Execute`, `Permit`, `Complete`, `Cancel`, `Snapshot`, `SetMode`, `ForceOpen`, `Disable`, `Isolate`, `Release`, `Reset`, `Close`, `Shutdown` |
| State and outcome | `State` (`StateClosed`, `StateOpen`, `StateHalfOpen`), `Mode` (`ModeNormal`, `ModeForceOpen`, `ModeDisabled`, `ModeIsolated`), `Outcome` (`OutcomeSuccess`, `OutcomeFailure`, `OutcomeIgnored`) |
| Configuration | `Config`, `WindowConfig`, `CountWindow`, `TimeWindow`, `OpeningRules`, `RuleCombination`, `IgnoredConsecutiveBehavior`, `OpenDurationPolicy`, `FixedOpenDuration`, `ExponentialOpenDuration`, `HalfOpenPolicy`, `HalfOpenFailureAction`, `HalfOpenAdmissionPolicy`, `RejectExcessProbes`, `WaitForProbe`, `Clock`, `Timer`, `Random`, `Completion`, `Classifier` |
| Policy constants | `OpenWhenAny`, `OpenWhenAll`, `PreserveConsecutiveFailures`, `ResetConsecutiveFailures`, `ReopenImmediately`, `ReopenAfterSample` |
| Observability | `Snapshot` (identity, state/mode/generation/time, window/outcome, admission/probe, ratio/open-duration, and observer counters), `TransitionReason`, `TransitionEvent`, `Observer`, `EventDeliveryPolicy`, `SynchronousEvents`, `AsynchronousEvents`, `EventOverflowPolicy` |
| Transition/event constants | `ReasonPolicyOpened`, `ReasonOpenIntervalElapsed`, `ReasonHalfOpenRecovered`, `ReasonHalfOpenFailed`, `ReasonForceOpen`, `ReasonDisabled`, `ReasonIsolated`, `ReasonReleased`, `ReasonReset`, `DropNewestEvent`, `DropOldestEvent` |
| Errors and bounds | `ErrInvalidConfig`, `ErrOpen`, `ErrHalfOpenExhausted`, `ErrHalfOpenWaitTimeout`, `ErrPermitCompleted`, `ErrPermitCanceled`, `ErrPermitExpired`, `ErrForceOpen`, `ErrIsolated`, `ErrInvalidOutcome`, `InvalidConfigError`, `InvalidOutcomeError`, `RejectionError`, `MaxHalfOpenProbes`, `MaxEventBuffer`, `MaxNameBytes` |
| `window` package | `NewCount`, `Count.Add`, `Count.Snapshot`, `NewTime`, `Time.Add`, `Time.Snapshot`, `Class` (`Success`, `Failure`, `Ignored`), `Record`, `Snapshot`, `MaxCountSize`, `MaxBucketCount` |
| `breakertest` package | `NewClock`, `Clock.Now`, `Clock.NewTimer`, `Clock.Advance`, `Clock.Set`, `Clock.ActiveTimers`, `Timer.C`, `Timer.Stop`, `NewRecorder`, `Recorder.Observe`, `Recorder.Events`, `Recorder.Dropped`, `Recorder.Reset`, `NewScriptedClassifier`, `ScriptedClassifier.Classify`, `ScriptedClassifier.Calls`, `ScriptedClassifier.Remaining` |

| Behavior class | Owned behavior |
| --- | --- |
| Package guarantee | admission serialization, state/generation transitions, bounded windows and queues, exactly-once lifetime completion totals, generation-scoped health recording, immutable aggregate snapshots, typed rejection identity, no retained operation value/error |
| Configurable policy | thresholds, window, open schedule/jitter, half-open recovery, waiting, permit TTL, classification, observer delivery |
| Observer behavior | consume transition copies, tolerate concurrent calls when synchronous, handle bounded async loss, avoid sensitive labels |
| Integration responsibility | protocol classification, retry/breaker order, timeout and body/row/stream ownership, attempt versus logical-operation scope |
| Caller responsibility | concurrency-safe injected functions, complete/cancel permits, call `Shutdown` or `Close` for async observers, bound retries and work, choose stable low-cardinality names |

There is no registry and no untyped execution facade. Registry enumeration,
duplicate-name ownership, and typed/untyped reflection parity are therefore not
applicable.

## Admission and completion lifecycle

1. `Acquire` obtains time without `Breaker.mu`, then locks and rechecks
   cancellation before admission. Mode/state expiry, admission, rejection
   accounting, half-open capacity, permit identity, and generation binding
   linearize there. Clock/timer callbacks can reenter or panic without stranding
   the state mutex.
2. Waiting releases `Breaker.mu` and waits on the generation-change channel,
   caller cancellation, or one stopped-on-return timer bounded by the absolute
   `MaxWait` deadline. It reacquires the lock and restarts admission; no FIFO
   ordering is promised.
3. `Execute` calls protected work and the classifier without `Breaker.mu`.
   Elapsed time uses the configured clock and clamps backward movement to zero.
   Clock sampling is panic-safe after admission: a start-time panic cancels the
   uninvoked permit, while a finish/recovery panic records failure with a safe
   fallback timestamp and preserves the original protected-function panic.
4. `Permit.Complete` or `Cancel` samples injected collaborators before locking
   `Breaker.mu`. Terminal status assignment is the exactly-once linearization
   point. Every successful `Complete` increments one lifetime outcome total;
   stale or disabled permits cannot mutate current health. Every terminal
   half-open path releases capacity. Invalid classifier output cancels the
   permit before returning the typed error.
5. State and administrative transitions mutate state, mode, counters, time,
   and generation together under `Breaker.mu`, then signal waiters. Events are
   copied under the lock and dispatched after unlock.
6. `Snapshot` samples the clock, locks `Breaker.mu`, lazily expires
   permits/buckets, and copies one internally consistent aggregate. Observer
   failure/drop counters are atomic because the async worker updates them
   independently.
7. Async enqueue and shutdown serialize with `eventMu`; `eventClosed` publishes
   shutdown atomically and `eventCloseOnce` makes `Close` idempotent.

The generation is a `uint64`, starts nonzero, and changes once per committed
transition or mode change. Even an impossible sustained rate of one billion
generation changes per second takes about 584 years to wrap. The same bound
applies to lifetime admission, rejection, completion, outcome, observer, and
open counters. Count-window aggregates are bounded by configured capacity;
time-window bucket counters would require more than 584 years at one billion
records per second in one bucket to wrap. Permit identity also includes breaker
identity and unique permit state, and window slots are validated by
bucket/generation identity, so no practical ABA path remains.

## Threat model and findings

Severity describes impact before correction. Every correction was preceded by
a failing regression.

| Severity | Finding and reproduction | Impact | Disposition |
| --- | --- | --- | --- |
| high | cancel context during `Clock.Now` before admission | protected work could start after pre-admission cancellation | fixed by locked cancellation linearization; `TestAcquireDoesNotAdmitCancellationObservedBeforeLinearization` |
| high | delay fake timer delivery after the wait deadline | half-open admission could exceed `MaxWait` | fixed with an absolute deadline check; `TestHalfOpenWaitDeadlineWinsDelayedTimerDelivery` |
| high | custom classifier returns an invalid enum in half-open | active probe remained occupied | `Execute` now cancels before returning `InvalidOutcomeError`; regression proves replacement admission |
| high | map bucket quotients outside `int64` to saturated endpoints | distinct extreme timestamps aliased and retained expired data | signed wide bucket identity preserves ordering; `TestTimeDoesNotAliasDistinctBucketsBeyondUnixNanoRange` |
| high | call `Close` from an asynchronous observer | worker waited on its own completion channel and deadlocked | callback-safe nonblocking `Close`; owner-waiting `Shutdown(ctx)` and reentrancy regression |
| high | reenter or panic from `Clock`, `Timer`, or `Random` | deadlock, stranded half-open permit, replaced operation panic, or stranded mutex | all injected collaborator calls moved outside the state lock; `Execute` uses panic-safe terminal fallback timestamps; deterministic reentrancy, panic-precedence, and permit-release regressions |
| high | complete admitted permits after another completion changes generation | completed operations disappeared from all aggregates | lifetime completion/outcome totals record every successful `Complete` once while stale generations remain isolated from current health |
| medium | change administrative mode during a half-open sample | prior-generation completion/success counters appeared in the new generation | counters reset with generation; `TestAdministrativeGenerationStartsFreshHalfOpenSample` |
| medium | configure an unbounded name or count threshold larger than its count window | allocation/telemetry abuse or an impossible policy | `MaxNameBytes` and count-rule/window validation reject construction |
| high | use timestamps outside `UnixNano` range, `MinInt64`, or epoch-spanning buckets | bucket wrap, truncation, or underflow could corrupt time-window aggregates | floor-correct signed wide arithmetic and reference boundary tests |
| low | repeatedly stop fake-clock timers | test helper retained stopped timer objects | stop now unlinks and clears storage; repeated retention test |

No high or medium finding remains open. Residual operational threats are
caller-owned classifier mistakes, excessive retries, abandoned permits until
their finite TTL, synchronous observer latency, and protocol resource leaks.

## Requirement-to-evidence matrix

| Requirement | Executable/document evidence |
| --- | --- |
| all transitions, generations, impossible enums | transition-table, generation, administrative, model, and race tests; [design.md](design.md) |
| thresholds and windows | exhaustive threshold tests, independent count/time reference fuzzers, 16,384-operation deterministic time sequences, extreme-time regressions; [policies.md](policies.md) |
| admission and permits | cancellation/wait linearization, exact lifetime completion totals, contention, duplicate/stale/expired/abandoned permit tests and fuzz sequence |
| classifier and execution | typed nil, wrapped/joined/sentinel/context, slow matrix, panic, invalid enum, and error/result preservation tests |
| memory model and snapshots | repeated race suite, lock/atomic lifecycle above, immutable snapshot and reentrant callback tests |
| clocks and leaks | deterministic jumps/backward/equal time, stopped/reset timer, injected-callback panic/reentrancy, open-expiry operator races, repeated timer/goroutine leak gates |
| observers and telemetry | synchronous/async, reentrant/panic/overflow/shutdown tests; bounded names/events and aggregate-only data |
| integrations | executable HTTP ordering/body/retry harness; nested `database/sql` and published `jsonrpc` client suites; Valkey, queue, and storage classifier contracts |
| resources and performance | hard limits, 100% statements, cross-CPU benchmarks, CPU/memory/mutex profiles, no production `unsafe` |
| security and compatibility | gitleaks tree/history, govulncheck, safety scan, pinned actions, dependency review, apidiff gate |

The core HTTP harness tests the complete composition contract without importing
an adapter. On 2026-07-16, the now-concrete `http-client` implementation was
also pinned to core commit `7300e65` and exercised through its first-party
adapter. Downstream commit `28277d1` proves one logical completion across
retries, bounded half-open retry ownership, intermediate and final body
ownership, the complete cache-to-transport order, cache and validation short
circuits, and caller-versus-dependency cancellation classification. The
dependency remains acyclic: the HTTP client imports core, while core imports no
adapter and remains functional during telemetry or control-plane outages.

The nested `integration/consumers` module is a reproducible release gate rather
than a production dependency. Its `database/sql` suite executes the standard
client boundary through a deterministic driver, verifies dependency failure and
caller-owned rows closure, and proves open rejection skips the driver. Its
`jsonrpc` suite pins the published client, separates local method validation
from transport failure, preserves wrapped error identity, and proves rejection
skips the transport.

## Performance disposition

Full-range time arithmetic initially changed rollover from about 5.4 ns/op to
about 14 ns/op. A representable-range fast path reduced the first correction to
7.8 ns/op; preserving non-aliasing signed wide identities measured 10.4 ns/op.
All results use zero allocations. The regression exceeded the 20% investigation
trigger, was profiled and optimized, and establishes a revised M4 Max audit
budget of 12 ns/op for `TimeRollover` and 200 ns/op for `TimeSnapshot` (measured
at 9.874 and 179.5 ns/op); correctness takes priority over the old overflowing
implementation. Observer and window storage stay fixed by configuration, and
no request result, error, goroutine, timer, or history is retained per completed
execution.
