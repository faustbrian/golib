# State-machine specification and threat model

## Transition table

| From | Condition | To | Window | Generation |
| --- | --- | --- | --- | --- |
| closed | opening policy true | open | retained for diagnosis | increment |
| open | interval elapsed during admission | half-open | retained | increment |
| half-open | recovery threshold true | closed | fresh empty window | increment |
| half-open | recovery failure true | open | retained until recovery | increment |
| any | reset | closed/normal | fresh empty window | increment |

Administrative mode changes do not rewrite policy state, but increment the
generation and invalidate permits for health-policy mutation. Force-open and
isolated reject; disabled admits without health recording; normal restores
policy admission. Every successful `Complete` still contributes once to the
lifetime completion totals. Impossible enum values are rejected.

### Exhaustive policy-state transitions

| Current state | Trigger | Next state | Generation/event |
| --- | --- | --- | --- |
| closed | classified completion below opening policy | closed | unchanged/none |
| closed | classified completion satisfies opening policy | open | one increment/`policy-opened` |
| open | admission before `NextProbeAt` | open | unchanged/none; reject |
| open | first admission at or after `NextProbeAt` | half-open | one increment/`open-interval-elapsed` |
| half-open | ignored or incomplete recovery sample | half-open | unchanged/none |
| half-open | configured recovery threshold succeeds | closed | one increment/`half-open-recovered`; fresh window |
| half-open | immediate failure or failed full sample | open | one increment/`half-open-failed` |
| any | `Reset` | closed/normal | one increment/`reset`; fresh window |

### Administrative transition and admission matrix

Changing to a different administrative mode retains the policy state, creates
one new generation/event, invalidates active permits, and resets half-open
sample counters. Setting the current mode is a no-op. `Release` is
`SetMode(ModeNormal)`; `Reset` is the distinct fresh-state transition above.

| Target mode | Helper | Event reason | Policy state |
| --- | --- | --- | --- |
| normal | `Release` | `released` | retained |
| force-open | `ForceOpen` | `force-open` | retained |
| disabled | `Disable` | `disabled` | retained |
| isolated | `Isolate` | `isolated` | retained |

| Mode/state | Admission result |
| --- | --- |
| force-open / any | reject with `ErrForceOpen` |
| isolated / any | reject with `ErrIsolated` |
| disabled / any | admit without health-policy recording |
| normal / closed | admit a recording permit |
| normal / open before expiry | reject with `ErrOpen` |
| normal / open after expiry | transition once, then apply half-open capacity |
| normal / half-open with capacity | admit one generation-bound probe |
| normal / half-open at capacity | `ErrHalfOpenExhausted` or finite `WaitForProbe` |

### Permit terminal matrix

| Permit condition/action | State mutation |
| --- | --- |
| active current `Complete` | terminal exactly once; record lifetime and current health outcome |
| active current `Cancel` | terminal exactly once; release probe only |
| active two-step permit past TTL | expire and release probe; record nothing |
| `Execute` completion past TTL | record lifetime outcome once; update current health only if its permit was not already expired or made stale |
| completed/canceled/expired repeated action | stable sentinel; record nothing |
| active stale generation | record lifetime outcome once; mutate no current health aggregates |
| disabled completion | record lifetime outcome once; mutate no health state |
| invalid classifier through `Execute` | cancel permit and release capacity |

## Linearization points

Caller-injected clock, timer, random, classifier, operation, and observer code
runs without `Breaker.mu`. Clock and optional jitter samples are obtained before
locking; their associated state changes linearize later under the lock. All
state-machine linearization occurs while `Breaker.mu` is held:

| Operation | Linearization point |
| --- | --- |
| Admit/reject | permit creation or rejection-counter increment |
| Open/half-open/close | state and generation assignment in transition |
| Permit complete/cancel/expire | permit terminal status assignment |
| Administrative mode | mode and generation assignment |
| Reset | closed transition and new-window assignment |
| Snapshot | aggregate capture after current expiry reclamation |

The protected operation and classifier run without the lock. Transition events
are built from before/after snapshots while locked and delivered after unlock.
Async enqueue/close serialization uses a separate event lock. Snapshot values
are copied, so callers never observe mutable internal structures.

## Resource ownership and complexity

- Count windows allocate at most `MaxCountSize` records; time windows allocate
  at most `MaxBucketCount` aggregates.
- Half-open retained permits are bounded by `MaxHalfOpenProbes`.
- Async observer queues are bounded by `MaxEventBuffer` and one worker.
- No operation result or error is retained. There is no per-call goroutine,
  timer, history, finalizer, cgo, or unsafe code in production.
- Waiting admission owns one finite timer per waiting caller and stops it on
  every return path. Core otherwise has no permanent goroutine.

## Threat model and findings

| Threat | Impact | Disposition/evidence |
| --- | --- | --- |
| Threshold crossing contention | duplicate transitions/probes | serialized transition; high-contention and race tests |
| Stale/duplicate completion | corrupt new generation | generation binding and terminal permit status tests |
| Abandoned probe | exhausted half-open capacity | finite TTL, cancellation, lazy expiry, leak tests |
| Dependency latency collapse | caller pile-up | slow-call rules; timeout remains caller responsibility |
| Retry storm | amplified failure | documented ordering and one logical recording |
| Clock jump/scheduler pause | early/late recovery or stale buckets | deterministic clock, boundary, idle-gap, backward-jump tests |
| Callback/classifier/observer panic | deadlock/corruption | all injected collaborators outside lock; clock/timer panic and reentrancy tests; observer isolation |
| Telemetry overload | admission latency/memory growth | bounded queue/drop policy; no rejection events |
| Secret/result disclosure | sensitive diagnostics | aggregate-only snapshots/events/errors |
| Operator race | contradictory state | generation invalidation and race tests |
| Coordinated replica probes | recovery surge | caller-configured bounded downward jitter |
| Allocation denial of service | memory exhaustion | validated hard maxima for windows/probes/events |

The finding reproductions and dispositions are recorded in
[hardening-audit.md](hardening-audit.md). No high- or medium-severity finding
remains open. Operational risks that cannot
be solved by core—incorrect classification, excessive caller retries, failure
to close protocol resources, and abandoned permits until TTL—are explicit
caller responsibilities.

## State invariants

- Generation is nonzero and increments exactly once per committed transition.
- Active half-open probes are never negative or above `MaxProbes`.
- Every successful two-step `Complete` and every terminal `Execute` increments
  exactly one lifetime outcome total. Duplicate, canceled, and abandoned
  two-step permits do not.
- Only a current-generation recording permit mutates the health window or
  transition policy; stale and disabled permits cannot.
- `Completed` equals `TotalSuccesses + TotalFailures + TotalIgnored` and never
  exceeds `Admitted`.
- Classified count equals successes plus failures; ignored is separate.
- Slow successes/failures are subsets of their corresponding outcome counts.
- Open timing is populated only after policy opening and reset on recovery.
- Observer code never runs under the state lock.
