# Kubernetes architecture

Run the scheduler as a small Deployment with two or more replicas. Give every
pod a unique owner value, connect all replicas to one persistent lease backend,
use the same lease namespace and IANA data, and dispatch durable work to a
separate worker Deployment. Readiness must remain false until registry
compilation, migrations, and backend safety checks succeed.

```text
Scheduler Deployment replicas
        | fenced lease decisions
        v
PostgreSQL or Valkey 9 ----> durable queue backend ----> Worker Deployment
```

Use a Kubernetes CronJob for infrastructure backups, isolated migrations, and
commands that do not benefit from application schedule registration. This
library does not watch Kubernetes resources and does not replace the CronJob
controller.

On rollout, new and old versions may coexist. Schedule identity includes the
declared version and timing contract, while the stable coordination identity
keeps matching physical occurrences and task leases shared across revisions.
Timing changes can still create non-matching old and new boundaries, so
coordinate those rollouts deliberately. Termination should cancel `Run`, stop
readiness, call `Drain` with a deadline, then exit. Set the pod termination
grace period longer than that deadline. A task that ignores cancellation stays
tracked and retains its overlap lease until it returns or the process exits;
long work therefore belongs in queue workers.

Never split a rollout across lease backends or namespaces. Changing a schedule
name, task, or parameters changes its coordination identity. Timing changes can
produce old-only and new-only boundaries. Review the complete
[rolling deployment matrix](hardening.md#rolling-deployment-matrix) before
deploying those changes.
