# Laravel Horizon migration matrix

This project targets Horizon-equivalent operational outcomes, not Horizon's
process-manager architecture. Kubernetes supervises processes, `queue`
supervises worker concurrency and delivery, and the control plane remains an
independent administrative system.

| Horizon scenario | Intended control-plane equivalent | Current status |
| --- | --- | --- |
| Worker and supervisor visibility | Tenant fleet status with heartbeat, queues, concurrency, jobs, drain, version, backend, and compatibility | Redis Streams and Valkey Streams workers can publish native status through the optional authenticated transport. |
| Queue depth and wait | Honest backend measurements exported through `queue` and `telemetry` | Native current status is implemented; Redis 6.2 reports pending and oldest age but marks unavailable depth and lag unsupported, while Valkey 9 reports them. History export remains incomplete. |
| Throughput and runtime | Platform metrics and dashboards, not a custom unbounded time-series database | Instruments partly modeled; production export absent. |
| Failed jobs list and inspect | Bounded tenant API with payload hidden by default | API, client, CLI, and authenticated record transport are wired to native Redis Streams and Valkey Streams readers. |
| Retry one or many failures | Authorized, confirmed where bulk, idempotent, audited `queue` command | Redis Streams and Valkey Streams enforce retry and bounded bulk retry with backend-specific ambiguity guarantees. |
| Delete or purge failures | Confirmed, authorized, bounded `queue` command | Redis Streams and Valkey Streams enforce record delete and failure/dead-letter collection purge; queue purge remains unsupported. |
| Pause and resume queues | Durable desired state consumed and acknowledged by workers | API, typed reader, command transport, and queue-owned enforcement are available when the worker owns a reconciliation loop. |
| Graceful termination | Drain or terminate request with acknowledgement and timeout state | Managed root queues stop admission, await settlement, and preserve acknowledged or timed-out outcomes. |
| Balancing | HPA/KEDA plus `queue` concurrency policy | Intentionally outside the control plane. |
| Process supervision | Kubernetes Deployment restart policy | Intentional architectural difference. |
| Tags and search | Bounded filters over published job metadata | Bounded failure search implemented; tag model absent. |
| Notifications | Platform alerts from queue and worker telemetry | Bounded threshold evaluator implemented; production snapshot collection, export, and notification ownership remain external. |
| Dashboard | Optional embedded UI using only the public API | Explicitly enabled console covers current status, history, and command surfaces; historical charts remain external. |
| Multi-tenancy | Explicit tenant in persistence, API, authorization, and Kubernetes mapping | Implemented for current surfaces. |
| Audit trail | Tenant-local tamper-evident command history | Implemented with one-shot audit and safe terminal-command retention. |

## Intentional divergence report

Process supervision belongs to Kubernetes; balancing belongs to HPA, KEDA, or
the queue worker; time-series history and notification delivery belong to the
telemetry platform. Generic Horizon tags are not part of the current bounded
record model. Redis and Valkey failure management remain delegated to
`queue`; native queue purge reports unsupported until that package can
implement it without bypassing delivery invariants. These are explicit ownership choices,
not partially implemented control-plane behavior.

## Migration guidance

Do not replace Horizon operational workflows until every row needed by your
runbook is available end to end. In particular, a command durably admitted by
the HTTP API is not evidence that a worker can enforce it; inspect its command
state and capability acknowledgement.

Inventory existing Horizon supervisors, queues, balancing rules, retry and
purge runbooks, tags, metrics, alert thresholds, retention, and privileged
payload access. Assign each item either to Kubernetes/HPA/KEDA, `queue`, the
control plane, or the telemetry platform. Intentional gaps must have an
external owner before cutover.

During coexistence, keep Horizon and `queue` on separate queue ownership
unless the data-plane migration explicitly proves compatible delivery and
settlement semantics. The control plane must never inspect or mutate Horizon's
Redis data directly.
