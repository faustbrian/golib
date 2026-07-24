# Goal: Queue Control Plane

## Objective

Build a production-grade open-source control plane for `queue` that provides
the operational visibility and control expected from Laravel Horizon while
preserving a strict data-plane/control-plane split.

`queue` remains the data plane and owns delivery, worker semantics, native
backend adapters, retries, acknowledgement, pending recovery, and enforcement of
control commands. `queue-control-plane` owns fleet visibility, desired
operational state, administrative workflows, historical presentation, API, CLI,
and optional web UI.

## Architecture

### Data Plane Contract

- Depend only on stable management, event, worker-status, failure, dead-letter,
  and control contracts exported by `queue`.
- MUST NOT independently issue raw Redis or Valkey queue commands.
- MUST NOT reimplement delivery, retry, reclaim, acknowledgement, or queue
  serialization semantics.
- Version and negotiate the worker/control-plane protocol for rolling upgrades.
- Degrade safely when workers are older, newer, partitioned, or unavailable.

### Kubernetes Boundary

- Kubernetes Deployments supervise worker and control-plane containers.
- Kubernetes restarts failed processes.
- HPA/KEDA manage pod scaling from exported metrics.
- `queue` manages goroutine concurrency and graceful worker drain.
- The control plane may integrate with Kubernetes for read-only workload status
  and explicitly authorized scaling actions, but MUST NOT become a general
  Kubernetes operator in v1.
- The control plane MUST NOT spawn or supervise OS worker processes.

## First-Version Capabilities

### Fleet And Queue Visibility

- Worker identity, version, start time, heartbeat, queues, concurrency, state,
  current jobs, and drain status.
- Queue depth, lag, pending count, oldest age, throughput, runtime, success,
  failure, retry, reclaim, dead-letter, and settlement-error metrics where the
  backend supports honest measurement.
- Explicit stale-worker and unknown-state representation.
- Queue/backend capability and compatibility display.

### Operational Control

- Pause/resume queue or worker group through durable desired state.
- Graceful drain and terminate-request workflows with acknowledgement and
  timeout state.
- Failed-job and dead-letter list, inspect, retry, bulk retry, delete, and purge.
- Safe replay requiring explicit destination, confirmation, authorization, and
  idempotency policy.
- Queue purge/clear only where `queue` exposes safe backend semantics.
- Every mutation has an idempotency key, actor, reason, audit event, and result.

### Interface Surfaces

- Versioned administrative HTTP API.
- CLI covering every mutating and diagnostic operation.
- Optional embedded web UI consuming only the public administrative API.
- Machine-readable health, readiness, version, and capability endpoints.
- Pagination, filtering, sorting, and bounded search for jobs and workers.

### Security

- Authentication through `authentication`.
- Authorization through `authorization` with explicit permissions for view,
  pause, resume, drain, retry, delete, purge, and scaling actions.
- Tamper-evident or append-only compatible audit event contract.
- Payloads hidden by default with configurable redaction and privileged access.
- CSRF, CORS, origin, cookie, token, rate-limit, and request-size protections.

### Metrics And History

- Current operational state may use bounded control-plane persistence.
- Historical metrics SHOULD be exported through `telemetry` to the platform
  metrics backend rather than recreated as an unbounded custom time-series DB.
- Audit and command history use explicit retention and pagination.
- Support queue wait alerts, failure alerts, stale workers, dead letters, and
  control-command failures.

## Persistence

- PostgreSQL-first control, audit, and metadata storage through `postgres`
  with migrations through `migrations`.
- Valkey may carry ephemeral worker heartbeat and desired-state coordination
  through a native adapter where the `queue` protocol requires it.
- Control state MUST remain separate from evictable cache data.
- Schema, retention, cleanup, and disaster recovery are explicit.

## Non-Goals

- No queue data-plane implementation.
- No OS process supervisor or Horizon-style child-process manager.
- No replacement for Kubernetes, HPA, KEDA, Prometheus, Grafana, or an
  OpenTelemetry Collector.
- No general workflow engine.
- No exactly-once replay claim.
- No backend-specific control operation bypassing `queue` contracts.

## Package And Binary Shape

- Root package: API models, service contracts, commands, permissions, errors.
- `apihttp`: versioned administrative API.
- `control`: desired-state and command orchestration.
- `fleet`: worker heartbeat and compatibility model.
- `history`: bounded audit and operational history.
- `postgres`: persistence adapter.
- `authz`: `authentication` and `authorization` integration.
- `client`: typed API client for CLI and automation.
- `cmd/queue-control-plane`: deployable API/UI binary.
- `cmd/queue-control`: administrative CLI.
- `ui`: optional embedded web application with generated assets isolated from
  the reusable Go packages.

## Testing And Quality Standard

Meaningful 100% production Go coverage is mandatory. UI behavior requires
meaningful component and end-to-end coverage. Tests MUST prove protocol,
authorization, mutation, replay, audit, stale-state, and failure behavior rather
than merely execute lines.

Required verification includes:

- fake and real `queue` data-plane conformance suites
- Valkey 9 and Redis integration through `queue`, never duplicate clients
- worker/control-plane rolling-version compatibility tests
- disconnect, stale heartbeat, delayed command, duplicate command, timeout,
  partial result, restart, and backend failure tests
- authorization matrix and mutation testing for every administrative action
- API fuzzing, browser security testing, race tests, and leak tests
- PostgreSQL migration, retention, audit, and disaster-recovery tests
- load benchmarks for large worker fleets, queue counts, failures, and history

## Documentation Deliverables

- Complete architecture and data-plane/control-plane boundary documentation.
- API reference, CLI reference, deployment guide, and five-minute quickstart.
- Horizon migration matrix covering workers, metrics, failures, retry, pause,
  resume, termination, balancing, tags, notifications, and intentional gaps.
- Kubernetes, HPA/KEDA, auth, authorization, audit, privacy, retention, payload,
  replay, incident response, upgrade, backup, and recovery guides.
- User guide for every UI and CLI workflow with examples and FAQ.
- Security, compatibility, troubleshooting, contribution, release, and
  maintained `CHANGELOG.md` documentation.

## Automation And Release

GitHub Actions MUST run Go and UI formatting, linting, tests, exact meaningful
coverage, race tests, fuzz smoke tests, browser tests, PostgreSQL and queue
integration matrices, vulnerability and dependency scans, SBOM/provenance,
docs, API compatibility, container builds, and signed release automation.

## Execution Plan

1. Freeze the `queue` management and protocol contracts required by control.
2. Implement fleet status, desired state, commands, persistence, audit, and CLI.
3. Implement failed/dead-letter operations and rolling-version compatibility.
4. Implement authenticated/authorized API and optional web UI.
5. Add Kubernetes/HPA/KEDA visibility and optional controlled scaling.
6. Complete failure, security, load, browser, operations, and release hardening.

## Acceptance Criteria

- Workers operate without the control plane; control-plane failure cannot stop
  normal data-plane delivery unless an existing durable pause requires it.
- Every mutation is authenticated, authorized, idempotent, and audited.
- No raw backend queue semantics are duplicated outside `queue`.
- Rolling versions and disconnected/stale workers are represented safely.
- Meaningful 100% Go coverage and every GitHub Actions gate pass.
- Horizon-equivalent user scenarios are documented with intentional differences.
- `CHANGELOG.md` is complete and current.
