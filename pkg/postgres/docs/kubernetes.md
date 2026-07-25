# Kubernetes deployment

## Service lifecycle

Use `StartupPing` so startup fails before the Pod becomes ready. A readiness
probe should call `Pool.Readiness` with a strict sub-second or low-second
deadline appropriate to the cluster. Liveness should call `Pool.Liveness` and
must not restart a healthy process merely because PostgreSQL is temporarily
unavailable.

Set `terminationGracePeriodSeconds` longer than the application's handler and
worker drain plus `ShutdownTimeout`. On `SIGTERM`, stop listeners and consumers,
cancel work, wait for owned goroutines, then close the pool. A timed-out close
indicates borrowed connections still exist; log bounded pool stats and allow
the grace period to finish.

## Connections

Calculate `MaxConns * maximum replicas` against the PostgreSQL connection
budget, including rollout surge. Horizontal scaling multiplies connections.
Keep administrative, migration, monitoring, and failover headroom. A pooler
such as PgBouncer changes session-state and prepared-statement assumptions;
test the deployed mode explicitly.

## Probes and disruption

Readiness failure should remove a Pod from traffic without killing it.
Configure PodDisruptionBudgets and rollout surge so PostgreSQL is not flooded
by simultaneous cold pools. Lifetime jitter prevents synchronized connection
recycling.

Run migrations as the dedicated Job in
[`examples/kubernetes/migration-job.yaml`](../examples/kubernetes/migration-job.yaml),
not in every application replica.
