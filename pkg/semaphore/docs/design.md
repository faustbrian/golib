# Design and semantics

## Ownership and synchronization

One mutex owns capacity accounting, the FIFO queue, counters, shutdown state,
drain-channel generations, and each permit's exactly-once release decision.
Unlocking the accounting mutex synchronizes the state transition with later
lock acquisitions under the Go memory model.
Per-waiter channel close publishes the granted permit or terminal shutdown
error. No callback executes while the mutex is held.

The core creates no goroutines or timers. Callers own goroutines and context
deadlines. An abandoned permit is a caller lifecycle bug; there is no finalizer
or lease because expiry cannot prove that the protected work has stopped.

## FIFO admission

Immediate admission requires enough available weight and an empty queue.
Otherwise the waiter is appended while holding the mutex. Release and
cancellation inspect only the queue head and grant consecutive heads that fit.
Smaller followers never bypass a large head, including through `TryAcquire`.
This produces intentional weighted head-of-line blocking and prevents a stream
of small acquisitions from starving an older large request.

Queue bounds count linked waiters under the same mutex. `MaxWaiters: 0`
disables waiting. Values above `MaxWaiters` are rejected at construction so the
queue is always explicitly bounded.

## Cancellation race

An already-done context fails before admission. A queued caller selects between
its ready channel and `ctx.Done()`, then resolves state under the mutex. If the
grant has linearized, it returns the permit; otherwise cancellation removes the
waiter, increments cancellation accounting, and attempts to admit the next
head. This rule cannot consume capacity without returning ownership and cannot
lose a wake-up.

## Shutdown and drain

The first `Close` marks the semaphore closed, removes all queued waiters, and
publishes a stable closed error. Later closes are no-ops. Existing permits keep
their weight and may release normally. `Wait` observes a generation channel
that closes only when acquired weight reaches zero after all grants in the
current transition. It respects its caller context and does not imply that
work ignoring cancellation has stopped.

Recommended termination order:

1. stop routing or producing new work;
2. call `Close` to reject local admission and wake queued callers;
3. call `Wait` with a deadline derived from the termination grace period;
4. report a deadline honestly instead of assuming in-flight work stopped.

## Consulted references

Recorded on 2026-08-02:

- Go 1.26.5 `context`, synchronization, timer, race detector, fuzzing, and the
  Go memory model (memory-model text version 2022-06-06);
- `golang.org/x/sync/semaphore` v0.22.0, commit
  `1eb64d4bc0cde6da1bb8ebc7f178bb577508e5d0`;
- Shopify Semian v0.28.2, commit
  `8835b3da31b31c45970cf229ee4a8e8a61e3ce51`.

`x/sync/semaphore` informed the strict head-of-line behavior. It accepts zero
weight, panics on negative weight, waits for context cancellation on an
oversized request, and releases raw weight without permit ownership. It has no
queue bound, snapshot, observer, or close/drain lifecycle.

Semian's bulkhead is resource-identified Ruby policy backed by host-wide SysV
semaphore tickets or worker quotas, timed acquisition, and `SEM_UNDO`. This
module is deliberately process-local Go state, supports weighted owned permits,
and does not own resource identity, circuit state, cross-process coordination,
or automatic crash recovery.

Primary references:

- <https://go.dev/ref/mem>
- <https://pkg.go.dev/context>
- <https://go.dev/doc/articles/race_detector>
- <https://go.dev/doc/security/fuzz/>
- <https://pkg.go.dev/golang.org/x/sync/semaphore@v0.22.0>
- <https://github.com/Shopify/semian/tree/v0.28.2>
