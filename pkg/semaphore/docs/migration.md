# Migration

## From a buffered channel

A channel send/receive can model fixed unit-weight concurrency. Migrate only
when explicit ownership, weighted requests, bounded FIFO waiting, typed
shutdown, drain, or snapshots are needed.

- Channel capacity becomes `Config.Capacity`.
- A send becomes `Acquire(ctx, 1)` and returns a permit.
- The matching receive becomes `permit.Release()`.
- Close the semaphore only to stop admission; do not use channel-close rules
  for permit release.
- Set `MaxWaiters` deliberately. Zero is fail-fast when capacity is occupied.

Keep the channel when it also carries work. This package is not a task queue or
worker pool.

## From golang.org/x/sync/semaphore

- Replace `NewWeighted(n)` with validated `New(Config{Capacity: n, ...})`.
- Replace `Acquire(ctx, n)` plus `Release(n)` with an owned permit.
- Change zero weights to a positive domain value; zero is rejected.
- Handle oversized weight immediately as `ErrOversize` instead of relying on a
  context deadline.
- Choose a finite queue bound and handle `ErrQueueFull`.
- Use `Close` then `Wait` for shutdown instead of acquiring all capacity as a
  drain convention.
- Do not assume cancellation wins after a grant has linearized; release every
  returned permit.

The packages share strict weighted head-of-line behavior, but they do not have
drop-in-compatible lifecycle or error semantics.
