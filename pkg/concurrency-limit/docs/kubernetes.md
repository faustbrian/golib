# Kubernetes lifecycle and HPA

Learning is process-local and pod-local. A new pod starts at `InitialLimit` and
does not know fleet capacity. Scale-up multiplies aggregate concurrency while
each new pod has a cold baseline. During a rolling update, algorithm versions
and limits may be mixed until old pods drain.

Size `MaxLimit` no higher than the downstream fleet's safe concurrency divided
by the maximum number of simultaneously active caller replicas, then reserve
headroom for other clients and rollout surge. Set `MinLimit` high enough to
preserve recovery probes but low enough that every maximum-replica pod can
safely reach it. Use a conservative `InitialLimit` between those values.

This release intentionally has no snapshot import. A future warm-start format
must carry a version, algorithm identity, bounds, age, and integrity validation;
invalid or stale snapshots must fall back to cold start. Never copy a learned
limit between unlike pods or infer fleet capacity from one pod.

For shutdown: stop routing, call `BeginDrain`, wait for in-flight work within a
bounded termination budget, classify shutdown cancellation as ignored, and
discard the process-local state. `Reset` is for an explicit new lifecycle and
invalidates old permits.

Local rejection can lower pod CPU. CPU-based HPA may then see less utilization
and fail to scale, or scale down while demand is being shed. Alert and scale on
request demand, local rejection, queue depth, current limit saturation, and
downstream signals in addition to CPU. Do not tune the limiter as an HPA.

The deterministic lifecycle tests cover independent cold scale-up, mixed
algorithm rollout, bounded scale-down drain, abrupt cold replacement, and a
CPU-only feedback model that scales from four replicas to one while rejection
increases. The model demonstrates the caveat; it is not an HPA configuration or
production sizing recommendation.

A distributed adaptive limit is not implemented: safe coordination requires a
coherent control plane with membership, leases, failure handling, propagation
delay, consistency, and partition semantics. A shared counter alone would turn
control-plane failure into admission failure.
