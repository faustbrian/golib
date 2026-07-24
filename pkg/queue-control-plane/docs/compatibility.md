# Compatibility and current capability status

This document distinguishes implemented contracts from deployable end-to-end
capabilities. The public API can describe a command before its corresponding
data-plane transport exists; that does not make the operation operational.

## Current production wiring

| Capability | Status | Notes |
| --- | --- | --- |
| Health, readiness, version, capabilities | available | Public machine-readable endpoints. |
| Static API-key authentication and tenant ACL | available | Powered by `authentication` and `authorization`. |
| Durable command idempotency and outcomes | available | PostgreSQL-backed. |
| Desired pause/drain state | available | PostgreSQL-backed API and typed tenant reader feed the caller-supervised `queue` reconciler. |
| Tamper-evident audit history | available | Pagination, verification, and retention package APIs. |
| Worker fleet status | optional | Redis Streams and Valkey Streams workers expose native bounded status through authenticated `queue` HTTP when the management tenant document is configured. |
| Kubernetes Deployment visibility | optional | Requires tenant mapping and in-cluster access. |
| Kubernetes Deployment scaling | optional | Uses only the scale subresource. |
| Queue and worker lifecycle commands | available when managed | The configured tenant endpoint dispatches to a root `queue` with queue-owned pause, resume, drain, and terminate enforcement. |
| Failure and dead-letter workflows | available for Redis Streams and Valkey Streams | API, client, CLI, and tenant routing are wired with hidden-by-default payloads. Both backends supply native reads, retry, bounded bulk retry, allowlisted durable replay, delete, and record purge; queue purge remains unsupported. |
| HTTP and command telemetry | optional | Explicit secure OTLP configuration through `telemetry`. |
| Queue status | optional | API, client, CLI, and authenticated `queue` HTTP transport are wired. Redis 6.2 explicitly marks unavailable depth and lag unsupported; Valkey 9 reports native depth, lag, and owned counters. |
| Record retention | optional | Redis Streams and Valkey Streams advertise exact-count retention only when deliberately configured. Manual purge is separate; time and byte retention remain unsupported and absent. |
| Queue history export | not wired | Historical metrics must use `telemetry`, not custom unbounded storage. |
| Embedded web UI | optional | Enable the public-API-only console explicitly; unsupported backend routes still fail closed. |

## Worker protocol model

The fleet package models protocol versions, supported ranges, capability
intersection, worker version and start time, heartbeat observation time,
queues, concurrency, current jobs, drain state, backend identity, and explicit
`stale` and `unknown` worker states.

Compatibility must be negotiated from the control-plane supported range and a
worker's advertised version and capabilities. A missing capability stays
unsupported. A version outside the range stays incompatible. No backend or
worker state is inferred from absence.

## Rolling upgrade rules

For every rolling deployment:

1. Enable lifecycle mutations only for workers configured with
   `queue.WithWorkerLifecycle` and the root queue as HTTP controller.
2. Treat transport failures and unknown outcomes as inconclusive, not evidence
   that a backend operation did or did not run.
3. Upgrade the control plane independently; workers do not depend on it for
   normal delivery.
4. Keep Kubernetes scale permissions optional and namespace scoped.
5. Run desired-state reconciliation from an owned cancellable worker loop;
   never infer active state from a missing record.

The normal Go gate exercises the authenticated `queue` HTTP boundary with
older, current, and newer protocol reports, a failed partitioned read, and a
reconnected compatible worker. It also drives pause, resume, status, and
duplicate command handling through a real managed `queue.Queue`. Real Redis
6.2.22 and Valkey 9.1.0 CI services additionally prove status, pause, resume,
failure and dead-letter listing, hidden and revealed inspection, retry, bounded
bulk retry, replay and duplicate rejection, destination consumption, delete,
record purge, and exact-count retention capability negotiation through their
native `queue` adapters. Rolling-version
HTTP coverage proves that the additive `retention_count` capability is enabled
only for a compatible worker and remains disabled for older or newer workers.

## Required upstream contract

`queue` now provides stable versioned commands, terminal results, bounded
worker and queue status pages, native Redis Streams and Valkey Streams status
providers, queue-owned lifecycle enforcement, desired-state reconciliation,
failure/dead-letter record contracts, native Redis and Valkey record access, and
authenticated bounded status, record, and command transport. Exact-count
retention is independently reported through capabilities; time and byte modes,
runtime retention reconfiguration, and safe queue purge remain upstream work.
The control plane must not replace backend work with direct Redis or Valkey
access.

`queue-control retention status` exposes configured and negotiated modes from
that capability report. Numeric limits are explicitly unknown because the
current worker-status contract does not carry them. The
`retention_configure` permission is reserved but no mutation endpoint is
advertised or accepted.

The [hardening evidence matrix](hardening.md) records the safe behavior and
test owner for absent, stale, duplicate, reordered, malformed, partitioned,
older, newer, unavailable, and ambiguous protocol observations.
