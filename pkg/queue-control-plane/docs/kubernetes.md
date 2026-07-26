# Kubernetes, HPA, and KEDA

Kubernetes Deployments supervise control-plane and worker containers.
Kubernetes restarts failed processes. `queue` owns worker goroutines and
graceful drain. HPA or KEDA owns automatic pod scaling from exported metrics.

The control plane's v1 Kubernetes adapter is deliberately smaller than an
operator. It maps each tenant to one namespace, lists Deployments, reads the
scale subresource, and performs explicitly authorized scale updates. It has no
reconcile loop and does not create, delete, patch, or watch workloads.

## Recommended deployment

- Run at least two control-plane replicas only after coordinating migration
  ownership and accounting for the process-local rate limiter.
- Run audit retention in a separate one-shot CronJob; never add its flags to a
  serving Deployment.
- Use a PodDisruptionBudget and topology spread for availability.
- Mount the access and tenant mapping documents read-only from Secrets.
- Run as user and group 65532, read-only root filesystem, no privilege
  escalation, seccomp `RuntimeDefault`, and drop all capabilities.
- Terminate TLS at a trusted ingress and apply namespace NetworkPolicies for
  ingress, PostgreSQL egress, and Kubernetes API egress.
- Use liveness and readiness endpoints with conservative failure thresholds.

## HPA and KEDA ownership

Do not build an autoscaler inside the control plane. Export honest queue
measurements through `queue` and the platform telemetry pipeline, then let
HPA or KEDA reconcile Deployment replicas.

Manual `scale` is an incident or maintenance action. If an HPA or KEDA
ScaledObject owns the same Deployment, it may overwrite the requested replica
count on its next reconciliation. Operators should pause or constrain the
autoscaler according to its own documented workflow before relying on a manual
value.

Queue metrics, example HPA/KEDA resources, and their integration tests are not
yet available because the data-plane metrics contract is not integrated.

## RBAC

Bind the control plane only in mapped tenant namespaces. Required permissions
are `get` and `list` for Deployments plus `get` and `update` for the Deployment
scale subresource. Avoid cluster-wide bindings.

Tenant mappings are immutable until process restart. Duplicate tenant IDs,
invalid namespace names, unknown fields, oversized input, and trailing JSON
fail startup. Removing a tenant requires a new mapping and a rolling restart.
