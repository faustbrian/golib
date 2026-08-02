# Architecture and invariants

## Ownership

The `Bulkhead` mutex is the single synchronization owner for policy state:
active weight, FIFO waiter list, queue depth, counters, drain state, and
partition-local events. `golang.org/x/sync/semaphore` v0.22.0 owns low-level
weighted permit accounting. The package never duplicates the semaphore's
capacity algorithm; it adds resource identity, queue policy, lifecycle, and
observability around `TryAcquire`.

The registry has a separate mutex and never receives callbacks. Bulkheads do
not reference their registry, so lock order cannot cycle.

## Conservation

At every public snapshot:

```text
active_weight + available_weight = configured_capacity
0 <= active_weight <= configured_capacity
0 <= queue_depth <= configured_max_queue
```

A queued grant acquires underlying weight before it becomes active. Release
returns underlying weight once, updates active weight under the same policy
lock, and grants FIFO heads before unlocking. Cancellation and timeout remove
only queued nodes and never release unacquired weight.

## FIFO and head-of-line behavior

The mutex orders queue insertion. Only the head can test available capacity.
If its weight does not fit, lighter later requests remain queued. Removing a
canceled or expired head immediately retries the next waiter. New arrivals
cannot bypass any queue entry.

## Callbacks and timers

Clocks are sampled outside the state mutex. Observers run after state commits
and outside all locks. Observer error and panic are contained. Waiting uses one
caller-owned timer and no production goroutine. Every terminal path stops the
timer; the timer, context, and waiter node are not retained afterward.

## Bounds

- resource and revision: 128 metric-safe bytes each;
- capacity and weight: positive `int64`;
- queue: positive and at most 1,048,576 waiters;
- registry: positive and at most 1,048,576 explicitly created partitions;
- events: fixed fields with no context, operation value, or arbitrary error;
- snapshots: a fixed rejection-reason map.

Registries do not preallocate their maximum cardinality. Lookup never creates,
and eviction is never automatic.

## Specification decisions

- FIFO is strict, not best-effort.
- Weighted head-of-line blocking is preferred to bypass and starvation.
- Operations that ignore cancellation retain capacity.
- Same-bulkhead reentrancy is rejected when detectable through `Execute`.
- Drain does not invent cancellation or completion of admitted work.
- State is process-local; cluster-wide admission requires a distributed system
  with lease and fencing semantics.
