# Hardening evidence and resource budgets

This report records the scheduler's bounded inputs, distributed failure model,
test evidence, and residual risks. It is not an exactly-once claim. A release
is blocked by an unbounded wait, deadlock, data race, panic, goroutine growth
beyond a configured capacity, ownership or overlap violation, incorrect civil
time result, public API drift, coverage regression, or failed backend matrix.

## Resource budgets

| Input or operation | Limit | Failure behavior |
|---|---:|---|
| compiled schedules | 10,000 | startup returns `ErrResourceLimit` |
| schedule name, version, task, or time zone | 255 bytes | definition fails |
| cron expression | 1,024 bytes | definition fails |
| encoded parameters | 64 KiB | definition fails |
| metadata | 128 entries and 64 KiB | definition fails |
| environments | 64 entries, 255 bytes each | definition fails |
| conditions | 32 per schedule | definition fails |
| lifecycle hooks | six fixed fields per schedule | no dynamic hook list |
| runner observers | 128 | construction returns `ErrInvalidRunner` |
| catch-up dispatches | 1,000 per decision | definition fails |
| occurrence scan | 10,000 candidates | `ErrOccurrenceLimit` |
| HTTP recovery body | 4 KiB | request is rejected |
| operational history | 1 to 1,000,000 events | oldest event is evicted |
| task execution wait | schedule `RunTimeout` | context is canceled and tick returns |
| managed executions | 128 by default | `ErrExecutionCapacity` |
| lease backend operation | 5 seconds by default | backend context error |
| condition, hook, or observer | 1 second by default | condition fails; lifecycle callback returns |
| managed callbacks | 128 by default | condition fails; lifecycle callback is omitted |
| drain | caller deadline | returns the context error while work remains tracked |
| retry count | no core retry loop | durable queue or worker owns a finite policy |
| task output | no core output capture | worker owns storage and truncation |

Execution, callback, and lease deadlines are configurable runner options. A
task that ignores cancellation continues in one tracked execution slot, and
its overlap lease remains heartbeated until it returns. This prevents a local
execution timeout from authorizing a concurrent replacement. `Drain` waits for
tracked work or returns at its caller deadline. Go cannot safely terminate an
arbitrary goroutine, so durable queue dispatch remains the isolation boundary
for untrusted or long-running work.

## Time conformance matrix

| Scenario | Contract | Executable evidence |
|---|---|---|
| DST spring gap | nonexistent local instants do not run | `TestTimeConformanceCorpus`, `TestRegistryCalculatesDSTGapAndFoldDeterministically` |
| DST autumn fold | both physical instants run | `TestDSTFoldsReturnBothPhysicalInstants` |
| unusual offsets | half-hour gaps and quarter-hour zones remain exact | `TestTimeConformanceCorpus` |
| leap century | 2096 advances to 2104 because 2100 is not leap | `TestCompileSearchesTheCompleteGregorianCycle` |
| month and year ends | invalid dates are skipped and rollovers are exact | `TestCalendarBoundaryCorpus`, `TestTimeConformanceCorpus` |
| delayed tick | catch-up retains only the configured newest tail | `TestRunnerBoundsDelayedTicksAndDoesNotReplayAfterBackwardJump` |
| backward jump | an already-advanced cursor does not replay | `TestRunnerBoundsDelayedTicksAndDoesNotReplayAfterBackwardJump` |
| long downtime | scan aborts after 10,000 candidates | `TestDueRejectsUnboundedDowntimeScan` |
| parser behavior | wrapper agrees with pinned parser for accepted and rejected expressions | `TestParserDifferentialCorpus` |
| long-range determinism | two registries agree over 4,096 boundaries | `TestLongRangeTimeCalculationIsDeterministicAndIncreasing` |

Calendar search covers a complete 400-year Gregorian cycle before reporting
no future occurrence. Time-zone results depend on the IANA data shipped by the
Go runtime or host. Roll all scheduler replicas together when that data
changes.

## Multi-replica state matrix

| Initial state | Transition | Required result |
|---|---|---|
| no occurrence owner | 32 replicas acquire together | one winner; all others receive `ErrHeld` |
| active occurrence owner | another replica ticks | skip without dispatch |
| expired occurrence owner | replacement ticks | dispatch with a larger fence |
| no task owner | 32 replicas execute together | one active task; all others emit overlap |
| active task owner | heartbeat succeeds | same owner and fence, later expiry |
| active task owner | executor ignores cancellation | tick returns; lease remains heartbeated |
| active task owner | heartbeat fails | execution context cancels; result fails |
| expired task owner | another replica acquires | higher fence; stale mutations fail |
| observed current owner | manual recovery with current token | lease becomes inactive |
| observed stale owner | heartbeat, release, or recovery | `ErrStaleOwner` and current lease unchanged |
| replacement requested | store lacks safe cancellation transfer | runner construction fails |

The shared conformance suite applies these acquisition, expiry, heartbeat,
release, recovery, cancellation, and fencing rules to memory, PostgreSQL, and
Valkey. `TestConcurrentReplicasDispatchOnePhysicalOccurrence` and
`TestConcurrentReplicasRespectActiveTaskOverlap` exercise runner-level races.

## Crash-point matrix

| Crash point | Observable result | Duplicate or miss control |
|---|---|---|
| before lease acquire | no backend mutation | another replica proceeds |
| after acquire, before dispatch | lease blocks peers until expiry | takeover uses a larger fence |
| during queue submission | outcome may be unknown | same occurrence key is retried or inspected |
| after submission, before idempotency completion | queue item may exist while ownership is in progress | retry is suppressed until ownership expiry; duplicate remains possible after takeover |
| after idempotency completion | retry observes replayed outcome | no second wrapper dispatch |
| during task execution | overlap lease heartbeats while process lives | process death permits fenced takeover after expiry |
| during completion or release | stale record may remain until expiry or recovery | compare-and-delete prevents an old owner deleting a new lease |
| during shutdown | readiness stops and `Drain` waits to its deadline | unfinished durable work remains queue-owned |

`TestExpiredPreDispatchOwnerIsTakenOverWithHigherFence` and
`TestCrashAfterDispatchIsSuppressedOnlyUntilOwnershipExpires` protect the two
largest crash windows. Consumers still must deduplicate the envelope occurrence
key. Ownership-sensitive downstream writes must reject a lower fencing token.

## Backend failure matrix

| Failure | PostgreSQL | Valkey 9 | Required caller behavior |
|---|---|---|---|
| outage or closed client | operation returns backend error | operation returns backend error | fail the decision; never classify as overlap |
| reconnect | new pool reads durable row and fence | new client reads lease and fence | resume through the same namespace |
| excessive latency | context cancels blocked statement with no row | context cancels; the Lua outcome may be committed or absent | treat timeout as unknown and inspect or retry the same fenced key |
| partial or malformed response | scan or token validation fails | reply shape and token validation fail | do not infer ownership |
| replica clock disagreement | `clock_timestamp()` is authoritative | server `TIME` is authoritative | ignore caller wall time for persistent leases |
| lost lease key or TTL expiry | inactive row retains its token | persistent same-slot counter retains its token | next owner receives a larger fence |
| stale mutation after takeover | compare owner and token | atomic Lua compares owner and token | fail closed |
| connection failover | pgx reconnects to the configured durable service | valkey-go reconnects to configured endpoints | backend operator must preserve a single authoritative data set |

Unit suites inject transport errors, malformed replies, missing rows, stale
owners, and canceled contexts. Tagged live suites add 32-way contention,
blocked writes, server-clock disagreement, expiry, closed clients, and
reconnect. CI and release workflows run them under the race detector against
PostgreSQL 14 through 18 and Valkey 9. Deployment-specific database
replication, promotion, and split-brain behavior remains the responsibility of
the configured backend service and must be tested in that service's own
failover drills.

## Rolling deployment matrix

| Old and new definitions | Coordination result | Deployment action |
|---|---|---|
| version changes only | same physical occurrence and task lease keys | safe to overlap replicas |
| cron or time zone changes, boundary matches | matching physical instant deduplicates | verify boundary corpus before rollout |
| cron, time zone, or jitter changes, boundaries differ | each distinct physical instant may dispatch | stage or feature-gate the timing change |
| parameters change | coordination identity changes | treat as a separate logical task |
| name or task changes | coordination identity changes | drain old replicas or accept parallel identities |
| schedule removed | old replicas can still dispatch until drained | remove only after old readiness is stopped |
| lease backend or namespace changes | replicas no longer coordinate | never split a rollout across backends or prefixes |
| IANA data changes | replicas may calculate different boundaries | roll the runtime and scheduler definitions together |

`CoordinationID` uses name, task, and canonical parameters. Revision `Identity`
also includes version, cron, time zone, and jitter. Occurrence keys combine the
coordination identity with a physical UTC instant. Queue envelopes carry both
identities so operators can distinguish rollout revision from distributed
coordination.

## Findings and residual risks

Resolved findings:

- rolling revisions previously used revision identity for distributed keys;
  they now use rollout-stable coordination identity;
- a non-cooperative executor previously defeated `RunTimeout`; it is now
  tracked behind bounded capacity while its overlap lease stays live;
- lease calls and application callbacks previously had no runner-owned
  deadline; both are bounded and drain-tracked;
- cron calculation previously returned zero across the 2096 to 2104 leap-day
  gap; calculation now searches a full Gregorian cycle;
- observer registration and active callback goroutines are now independently
  bounded;
- backend conformance previously lacked concurrent acquisition and live fault
  recovery cases; those paths now gate release.

Accepted residual risks:

- queue submission plus idempotency completion cannot be atomic across two
  independent systems, so at-least-once delivery remains possible;
- an expired owner can continue non-fenced side effects, so downstream fencing
  and idempotency are mandatory;
- a Valkey or network timeout can have an ambiguous committed outcome; retrying
  a different key is unsafe;
- `MissedRunSkip` deliberately drops late boundaries and bounded catch-up
  deliberately omits older occurrences;
- forced manual recovery can create concurrency unless the observed owner is
  isolated first;
- arbitrary Go callbacks cannot be killed; capacity prevents unbounded growth,
  and process termination is the final isolation boundary.

The security assumptions behind these controls are in the
[threat model](threat-model.md). Performance evidence and review thresholds are
in the [benchmark baseline](benchmark-baseline.md).
