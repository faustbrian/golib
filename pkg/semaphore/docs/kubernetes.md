# Kubernetes semantics

The semaphore is memory in one process. It is pod-local when one application
process runs per pod. Capacity `N` across `R` replicas permits up to `N * R`
aggregate in-flight weight when traffic reaches all replicas. Uneven routing
changes utilization, not the upper bound.

## Scaling and replacement

- Scale-up adds a fresh capacity `N` per new replica immediately after it
  starts accepting traffic. It carries no queue, history, or permits from
  existing pods.
- Scale-down must stop admission on the departing pod and drain its existing
  permits. Abrupt loss abandons process memory and does not prove external work
  stopped.
- A rolling replacement temporarily runs old and new pods together, so total
  capacity follows the maximum simultaneously ready replica count. Mixed
  revisions may have different capacities; calculate the sum explicitly.
- Readiness may stop new traffic during drain. Dependency health should not be
  coupled to liveness, because restart loops can amplify an outage.

On `SIGTERM`, applications should stop producers and readiness, call `Close`,
then call `Wait` with a deadline shorter than `terminationGracePeriodSeconds`.
A `Wait` deadline is evidence that local ownership did not drain in time, not
evidence that work was canceled.

For a global concurrency constraint, use a distributed owner with durable
leases and fencing. Do not divide a desired global limit by the current replica
count and call it global: scaling, skew, crashes, and rolling overlap make that
best-effort arithmetic unsafe.

HPA metrics should expose demand, queue saturation, and rejection separately.
CPU can fall when work is rejected, creating a harmful feedback loop if it is
the only scaling signal. Event labels are intentionally low-cardinality and
contain no tenant or request identity.
