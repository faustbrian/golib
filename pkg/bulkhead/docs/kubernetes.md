# Kubernetes and sizing

## Scope

Bulkheads are in-memory and process-local. Capacity `C` on `R` simultaneously
serving pods permits at most `C * R` aggregate admitted weight, subject to
traffic distribution. It is not a cluster-wide guarantee and uneven routing
can saturate one pod while others are idle.

## Capacity equations

Let:

- `D` be safe downstream concurrent operations across the fleet;
- `P` be the usable connection-pool capacity per pod;
- `F` be maximum downstream fan-out per admitted unit;
- `Rmax` be the maximum simultaneously serving pods, including rollout surge;
- `H` be a reviewed headroom fraction in `(0, 1]`.

For unit weights, choose:

```text
C <= floor(P / F)
C <= floor(D * H / (Rmax * F))
C_safe = min(floor(P / F), floor(D * H / (Rmax * F)))
```

If weights represent connections or fan-out directly, define weight so
`active_weight` already measures those units and omit the extra `F` from the
corresponding bound. Queue memory is bounded by `MaxQueued * Rmax`; queued work
does not create downstream capacity.

Example: a dependency supports 600 safe concurrent calls, HPA can serve 12
pods during surge, each operation fans out to 2 calls, pod pools expose 80
usable connections, and `H=0.8`:

```text
pool bound       = floor(80 / 2) = 40
downstream bound = floor(600 * 0.8 / (12 * 2)) = 20
C_safe           = 20 operations per pod
```

## Scaling and rollout

Scale-up creates fresh independent capacity immediately; scale-down removes it
only after old work drains or the process exits. Use the maximum concurrent
old-plus-new pod count, not steady replicas, in sizing. Mixed policy revisions
temporarily provide:

```text
aggregate = old_pods * old_capacity + new_pods * new_capacity
```

Configuration changes do not resize existing permits. Construct new
bulkheads for the new revision and drain the old process or explicit
partition.

## Termination sequence

1. fail readiness or otherwise remove new external traffic;
2. call `Close` on every application-owned bulkhead;
3. queued callers return `ErrClosed`;
4. call `Drain` with a deadline shorter than
   `terminationGracePeriodSeconds`;
5. let admitted operations honor cancellation and release;
6. on `ErrDrainIncomplete`, report ambiguity and let the application choose
   whether to keep waiting or exit.

Kubernetes may still terminate the process at the grace deadline. The package
cannot report work completed after abrupt kill.

## Health and HPA

Dependency saturation must not fail liveness; restarting healthy processes can
amplify the outage. Readiness is an application rollout and traffic decision,
not an automatic response to any rejected dependency.

Rejection can lower CPU because less work executes. CPU-only HPA can therefore
scale down during saturation. Alert on bounded queue depth, rejection reason,
wait latency, drain duration, and downstream signals. Custom HPA signals need
stabilization and missing-series behavior that avoid positive feedback.
