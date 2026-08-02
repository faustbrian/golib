# Kubernetes sizing and drain

Budgets and caller-owned latency history are process-local unless the caller
provides a distributed implementation. With `L` concurrent logical operations,
`H` maximum hedges, and a pod budget `B`, a pod has at most `L + min(L*H, B)`
active attempts. The additional-work ratio is at most `min(L*H, B) / L` when
`L > 0`.

At HPA maximum `R`, the fleet can add at most `R*B` simultaneous hedges and can
run at most `R*(L+B)` attempts under a per-pod logical load bound `L`. Size for
the maximum replica count, not the current count. Scale-up multiplies aggregate
budgets and begins with cold percentile data. Rolling updates can mix delay
policies. Scale-down cancels in-flight work only when pod shutdown cancels the
logical contexts.

Graceful shutdown order:

1. stop admitting new logical operations;
2. cancel their total contexts, which stops scheduled timers and cancels losers;
3. call each returned `Report.Wait` with the remaining drain deadline;
4. close caller-owned request and transport resources; and
5. report a `Wait` deadline as an uncooperative attempt, not successful cleanup.

Cancellation does not terminate code that ignores its context.
