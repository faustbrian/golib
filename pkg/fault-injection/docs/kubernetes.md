# Kubernetes semantics

An injector is process-local. In Kubernetes that normally means one container
in one pod. It has no knowledge of Deployments, ReplicaSets, Services, nodes,
zones, disruption budgets, or other replicas.

## Replica selection and percentages

Independent random streams in every pod do not implement a fleet-wide
percentage. Load balancing, uneven traffic, retries, locality, cold replicas,
and different pod lifetimes bias the observed population. An external
orchestrator must select exact pods, own the blast radius, distribute explicit
configuration, and collect fleet evidence.

## Rolling updates and ephemeral state

Counters, generations, budgets, duplicate buffers, and seeded call positions
are memory-local and disappear when a container restarts. A rolling update can
therefore mix versions, seeds, generations, and campaign progress. Treat a new
pod as a new experiment participant unless the external orchestrator records
and deliberately reconstructs its inputs.

## Termination and readiness

On pod termination, SIGTERM and the pod grace period still govern application
shutdown. Injected delay must be bounded by caller contexts and leave enough
time for resource cleanup. Runtime disable should be part of the application's
drain path when runtime experiments are enabled.

Injected latency or errors can fail readiness probes or application-level
health dependencies. An unready pod is removed from normal Service traffic,
which changes replica traffic distribution and the campaign sample. Do not use
injected dependency failure as a liveness signal unless restarting the process
is the intended experiment.

## Autoscaling

HPA inputs such as CPU or application metrics may react to injected latency,
errors, retries, and queue growth. Scaling changes traffic and the number of
local random streams, so observed percentages cannot be interpreted as the
configured per-pod fraction.

See the official Kubernetes documentation for
[pod lifecycle](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/),
[readiness](https://kubernetes.io/docs/concepts/workloads/pods/probes/), and
[disruptions](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/).
