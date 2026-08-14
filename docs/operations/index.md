# Operations

Golib packages expose lifecycle and diagnostic contracts; the deployed service
owns infrastructure sizing, credentials, routing, and incident response.

## Process And Deployment

- Run one explicit role per process: API, worker, scheduler, migration, import,
  or another bounded command.
- Keep `/livez`, `/startupz`, and `/readyz` on a management listener. Liveness
  proves the process can run; dependency degradation belongs in readiness or a
  separate diagnostic.
- Stop admission on termination, withdraw readiness, drain accepted work within
  the total shutdown budget, flush delivery and telemetry, then close clients.
- Use non-root images, read-only root filesystems, bounded writable temporary
  storage, dropped capabilities, and explicit CPU, memory, process, and file
  descriptor limits.
- Run schema migrations as controlled jobs before workloads that require the
  new schema. Prefer forward-compatible expand and contract transitions.

The [platform reference fixture](../../pkg/service/integration/reference-platform/README.md)
provides executable local container evidence.

## Durability And Workers

- PostgreSQL is the source of truth for transactional business state.
- Use an outbox when state and publication must share one commit.
- Establish idempotency or fencing before externally visible work.
- Treat accepted, acknowledged, retriable, terminal, and unknown outcomes as
  different states.
- Monitor queue age, pending claims, retry rate, dead letters, reconciliation
  backlog, and drain duration; queue length alone is insufficient.
- Back up and restore both data and the metadata needed to resume ownership.

## Configuration And Secrets

Local development may load `.env`, JSON, YAML, or TOML through `config`.
Production Kubernetes deployments should inject configuration and Infisical
secrets at the platform boundary. Runtime refresh is appropriate only when the
provider and consuming client define atomic replacement, overlap, retirement,
and failure behavior. Never log values or place credentials in metrics.

## Observability

Propagate correlation and trace context at every accepted boundary. Logs,
metrics, and traces must use bounded labels and omit payloads, credentials,
tenant identifiers, and attacker-controlled values by default. Define SLOs
from user-visible outcomes, then alert on burn rate, dependency saturation,
queue age, reconciliation lag, and exhausted capacity.

Better Stack export is a deployment adapter, not a core-package dependency.
Sampling must preserve errors and critical lifecycle transitions while keeping
normal traffic volume bounded.

## Overload And Recovery

Apply finite request bodies, concurrency, queues, deadlines, attempts, hedges,
connections, and observer buffers. Local rejection is not a downstream
failure. Exercise dependency replacement, credential rotation, process death,
restore, reconciliation, and rollback before production adoption.

Current proven and missing production boundaries are recorded in
[operational assurance](../operational-assurance.md) and its
[requirement matrix](../assurance/requirement-matrix.md).

Return to the [documentation index](../index.md).
