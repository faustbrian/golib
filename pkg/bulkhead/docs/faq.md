# FAQ

## Why not use only a semaphore?

A semaphore counts permits. This package adds stable resource identity,
bounded FIFO wait policy, queue saturation, lifecycle observations, typed
admission outcomes, execution cleanup, explicit partitions, and drain.

## Is capacity shared across pods?

No. It is per process. Aggregate capacity changes with serving replica count
and traffic distribution.

## Does context cancellation stop my callback?

Only if the callback honors context. The package never reports false
termination or reclaims capacity while the callback still runs.

## Can a lighter waiter bypass a heavy FIFO head?

No. Strict FIFO deliberately permits weighted head-of-line blocking. Use
separate partitions for independent service levels.

## Can I resize a live policy?

No. Config is immutable. Create a new revision and drain the old policy. A
registry requires close, drain, remove, and explicit create.

## Should bulkhead rejection open a circuit breaker?

No. Rejection means local admission did not execute the downstream operation.
Do not classify it as downstream failure.

## Should rejection fail liveness?

No. Dependency saturation does not imply the process is unhealthy. Treat
liveness, readiness, alerts, and traffic routing as separate decisions.

## Is reentrancy always detected?

It is detected when the acquisition receives a context derived from
`Execute`. Arbitrary standalone acquisition cannot safely infer goroutine
ownership; document and avoid recursive permit acquisition.
