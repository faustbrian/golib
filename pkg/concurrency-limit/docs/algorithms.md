# Algorithms and equations

The limiter invokes one algorithm after a recent window satisfies both minimum
duration and minimum sample count. Every requested limit is then clamped by the
configured per-update step and absolute bounds.
All gains, queue terms, tolerance, and long-window sizes are validated against
the package arithmetic bounds before an algorithm is constructed.

## Fixed

`L_next = L`. Use it for migration, control groups, or proving the admission
surface without adaptive movement. It is not a reusable fixed bulkhead.

## AIMD

On explicit overload or a configured latency threshold breach:

`L_next = floor(L * decreaseFactor)`

When observed in-flight work reaches at least half of `L`:

`L_next = L + increase`

Otherwise it holds. Additive increase and multiplicative decrease follow the
convergence family analyzed by Chiu and Jain, while the overload and
application-limited classifications are service-concurrency adaptations.

## Vegas-style

The bounded queue estimate is:

`Q = ceil(L * (1 - baselineRTT / recentRTT))`

It increases when `Q < alpha`, decreases when `Q > beta`, and holds between the
thresholds. Explicit overload decreases immediately. Rising in-flight work
with non-increasing throughput also decreases, preventing latency-only upward
drift. Application-limited windows hold. This is named “Vegas-style” because
it applies the Vegas delay signal to RPC concurrency rather than implementing a
TCP sender.

The default profile uses `alpha=2`, `beta=4`, and one-unit changes. It was
selected after the deterministic constant, bursty, bimodal, heavy-tail,
sparse, capacity-collapse, and recovery simulations and the equivalent
algorithm microbenchmarks in this module.

## Gradient2

The long RTT is an exponential average with `alpha = 2/(LongWindow+1)`. For an
utilized window:

`gradient = clamp(tolerance * longRTT / recentRTT, MinGradient, 1)`

`target = L * gradient + QueueSize`

`L_next = L * (1 - smoothing) + target * smoothing`

The implementation includes Netflix's recovery correction when long RTT is
more than twice short RTT. Explicit overload and throughput stall prevent an
increase. This algorithm is appropriate for bursty latency distributions when
a minimum-RTT baseline is too biased.

## References

- [Netflix concurrency-limits](https://github.com/Netflix/concurrency-limits),
  including its Vegas and Gradient2 implementations.
- [Failsafe-Go adaptive limiter](https://failsafe-go.dev/adaptive-limiter/).
- Chiu and Jain, [Analysis of the Increase and Decrease Algorithms for
  Congestion Avoidance](https://doi.org/10.1016/0169-7552(89)90019-6).
- Brakmo, O'Malley, and Peterson, [TCP Vegas: New Techniques for Congestion
  Detection and Avoidance](https://doi.org/10.1145/190314.190317).

Algorithm names, state bounds, reset behavior, and reference cases are public
contracts. Changing an equation requires a compatibility and migration review.
