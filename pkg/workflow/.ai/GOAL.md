# Goal: Durable Workflows And Sagas

## Objective

Build `workflow` as an explicit durable execution engine for multi-step
business workflows and sagas that require persisted progress, retries,
timeouts, compensation, signals, recovery, and operator intervention.

It MUST compose with `state-machine`, `scheduler`, `sequencer`, `outbox`,
`queue`, `lease`, `idempotency`, and `event-sourcing` without duplicating or
requiring them. The package MUST NOT hide distributed-systems limitations or
claim exactly-once side effects.

## Execution Model

Define durable workflow definitions, versions, instances, steps, attempts,
timers, signals, results, failures, compensation state, and terminal outcomes.
The model MUST distinguish orchestration from choreography and support both
without one implicit global event bus.

Every transition MUST be persisted before externally observable progression
according to an explicit transaction/outbox strategy. Replaying an instance
MUST be deterministic with respect to persisted decisions. External side
effects MUST occur through activities with explicit idempotency and unknown-
outcome semantics.

## Definitions And Versioning

- Definitions MUST have stable names and immutable versions.
- Running instances MUST remain pinned to compatible behavior.
- Code upgrades MUST not reinterpret historical state silently.
- Support explicit migration, continue-as-new, deprecation, and termination.
- Definition registration MUST reject duplicates and incompatible changes.
- Reflection-driven method discovery and hidden package initialization are
  forbidden.

## Activities And Compensation

Activities MUST accept context, deadlines, attempt metadata, idempotency keys,
and bounded inputs. Define retryability, backoff, cancellation, heartbeat,
timeouts, unknown outcomes, and result size. Compensation MUST be explicit,
ordered by policy, independently retryable, observable, and capable of manual
resolution. Compensation failure MUST never be reported as successful rollback.

Support parallel branches, joins, races, child workflows, bounded fan-out,
human approvals, external signals, timers, deadlines, cancellation, and
continue-as-new without unbounded histories or goroutine ownership.

## Durable Store And Workers

Provide small contracts for instance state, event/history append, due work,
leases, timers, signals, and operator actions. PostgreSQL MUST be the first
durable adapter. It MUST support optimistic concurrency, leasing/fencing,
atomic claims, crash recovery, stable pagination, archival, and reconciliation.

Workers MUST provide bounded concurrency, fair admission, graceful shutdown,
lease renewal, stale-owner rejection, poison-work handling, dead letters, and
safe retries. Kubernetes/ECS owns process supervision; the package owns durable
execution semantics.

## Messaging And Transactions

Queue, Kafka, outbox, and CloudEvents integrations MUST be optional adapters.
Inbound messages require deduplication and tenant/correlation propagation.
Outbound effects require durable publication or explicit accepted-loss policy.
No adapter may acknowledge work before the durable transition it represents.

## Operator Surface

Provide APIs, not a mandatory UI, for inspect, list, pause, resume, retry,
cancel, terminate, signal, compensate, replay diagnostics, dead-letter
resolution, and history export. Operator commands MUST be idempotent, audited,
authorized by callers, and safe under concurrent execution.

## Observability

Expose bounded metrics, traces, logs, and lifecycle hooks for workflow latency,
activity attempts, timers, retries, compensations, stuck instances, lease loss,
dead letters, and recovery. Workflow/tenant IDs MUST not be unbounded labels.

## Verification

Use deterministic clocks and model-based tests for every state transition.
Inject process death between persistence, publication, acknowledgement, lease,
activity, and compensation boundaries. Prove duplicate delivery, unknown
outcomes, rolling upgrades, definition migration, timer recovery, cancellation,
and reconciliation. Run PostgreSQL integration, race, fuzz, leak, stress, soak,
and resource-bound tests. Exact 100% statement coverage and 100% viable mutant
kills are REQUIRED.

## Documentation And Delivery

Document saga theory, guarantees, orchestration/choreography choices,
idempotency, compensation, versioning, operations, schemas, recovery, deployment,
capacity, security, FAQ, and end-to-end examples. Add manifests, CI, benchmarks,
changelog, adapters, and clean-consumer proof.

## Non-Goals

- distributed ACID transactions or exactly-once external effects;
- replacing queues, schedulers, state machines, or business application code;
- hidden controller/model binding or service-container orchestration;
- executing arbitrary untrusted code.
