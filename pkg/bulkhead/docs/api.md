# API

## Construction

`New(Config)` constructs one immutable policy. `Resource` must be a non-secret,
metric-safe identifier of at most 128 bytes. `Capacity` is a positive `int64`.
`PolicyRevision`, when present, follows the same label restrictions.

`Admission` is either:

- nil or `RejectImmediately{}` for immediate capacity rejection; or
- `Wait{MaxQueued, MaxWait}` for strict FIFO bounded waiting.

`Clock` owns timestamps and wait timers. Implementations must be concurrency
safe and return a live, stoppable timer. `Observer`, `Clock`, and policy values
reject typed nils. Config is copied; mutable dependency implementations retain
their own synchronization responsibility.

## Admission

`Acquire(ctx, weight)` has these linearization points:

- immediate admission: the successful underlying weighted-semaphore
  `TryAcquire` while the bulkhead mutex owns policy state;
- queued admission: the successful FIFO head grant under the same mutex;
- cancellation, timeout, saturation, and shutdown: removal or rejection under
  that mutex.

Positive weights must not exceed total capacity. Queue bounds count every
enqueued waiter deterministically. Cancellation racing a grant observes the
lock-ordered winner: a grant returns a permit; an earlier removal returns its
typed terminal error.

## Permit

`Permit.Weight` and `Permit.Resource` are stable diagnostic metadata.
`Release` uses an atomic exactly-once transition. The first release returns
capacity; concurrent or later releases return `ErrPermitReleased`. A permit
does not expire and has no finalizer. Abandoning it is a caller bug because
reclaiming capacity while work might still execute would violate isolation.

## Execution

`Execute[T]` acquires, invokes `func(context.Context) (T, error)`, records
execution duration, and releases on success, error, cancellation return, or
panic. It re-panics with the original value after cleanup. It never terminates
the callback on the caller's behalf. The callback must honor its context when
bounded termination is required.

The execution context records a private stack of active bulkheads. Recursive
acquisition of the same instance returns `ErrReentrant`; nested acquisition of
different instances is allowed. Standalone `Acquire` cannot safely detect
reentrancy unless it receives an `Execute`-derived context.

## Observations

`Snapshot` defensively copies rejection counters and exposes capacity, active
and available weight, queue depth, lifetime admissions, rejections by reason,
caller cancellations, executions, aggregate wait and execution durations,
drain state, resource, and policy revision.

`Event` reports admitted, queued, rejected, caller-canceled, released,
executed, draining, and drained transitions. Observers run synchronously,
outside all internal locks.
Their errors and panics are ignored. A slow observer is explicitly part of
request latency and can affect maximum-wait outcomes.

## Shutdown

`Close` stops admission, marks every queued waiter with `ErrClosed`, and leaves
admitted permits valid. `Drain(ctx)` also waits until active weight and queued
callers reach zero. Context termination returns `ErrDrainIncomplete` joined
with the context error; live capacity is never reclaimed speculatively.

## Registry

`NewRegistry(FixedPartitions{Maximum: N})` creates an application-owned bounded
registry. `Create` is the only creation operation. `Lookup` never creates.
`Remove` requires a closed and drained partition. `Snapshots` returns
resource-sorted defensive snapshots.
