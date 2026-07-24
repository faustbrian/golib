# Deterministic concurrency

`manual.Clock` protects state with one mutex and invokes neither callbacks nor
observers while that mutex is held. Concurrent `Now`, mark measurement,
factories, stop/reset, advancement, cancellation, jump, and shutdown are safe.

One advancement coordinator owns progress at a time. Concurrent and nested
advances register requests with a target and waiter. The coordinator processes
deadlines in heap order, starts callback-owned goroutines, and wakes when those
callbacks register work. A request completes only after its target work and
callbacks have completed. The configured active limit also bounds outstanding
advancement requests while the coordinator is occupied.

A callback may wait for same-instant work. For future work beyond the current
target, it must issue a nested `Advance` and wait on that request. Waiting for an
unadvanced future timer is intentionally not guessed or virtualized.
`testing/synctest` should be used when automatic whole-test quiescence is the
desired contract.

While callbacks are running, ordinary clock operations wake the coordinator to
process same-instant work but do not permit future monotonic progress. Calling
`Wait` on a pending nested or concurrent advancement explicitly permits progress
through that request's target. This keeps successive callback resets relative
to one deterministic instant without deadlocking callbacks that deliberately
advance and wait for future work.

The work budget bounds recursively registered same-instant work. On exhaustion,
all outstanding advancement requests receive `ErrWorkLimit`, already-started
cooperative callbacks are drained, and new nested advances fail immediately.
Arbitrary user callbacks that never return cannot be forcibly stopped by Go;
their termination remains callback-owner responsibility.

`Shutdown` is idempotent, releases scheduled objects, wakes sleepers with
`ErrClosed`, and fails outstanding advancement waiters. It is safe from inside
a callback.
