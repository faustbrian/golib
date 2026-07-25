# Goal: Distributed Application Scheduler

## Objective

Build a production-grade open-source application scheduler for Go that provides
the practical capabilities teams rely on in Laravel's scheduler while fitting
Kubernetes deployments and durable queue-based execution.

The scheduler MUST support declarative schedules, one-owner execution,
non-overlap, missed-run policy, observability, and deterministic testing. It
SHOULD dispatch durable work to `queue` rather than executing long-running
business work inside the scheduler process.

## Product Position

- Application schedules are code-defined, named, versioned, inspectable, and
  testable.
- `scheduler` coordinates schedule decisions; Kubernetes supervises scheduler
  containers and worker containers.
- Multiple scheduler replicas are supported through distributed leases.
- Kubernetes CronJobs remain appropriate for infrastructure jobs, isolated
  migrations, backups, and commands that do not benefit from application-level
  schedule registration.
- Schedule locking reduces duplicate dispatch but does not replace idempotent
  jobs.

## Schedule Definition

- Named schedules with validated cron expressions and explicit time zones.
- Convenience builders for common intervals without hiding the cron result.
- Task identity and parameter identity for repeated parameterized schedules.
- Enable/disable, environments, maintenance policy, conditions, date bounds,
  jitter, and metadata.
- Explicit missed-run policy: skip, run once, or bounded catch-up.
- Explicit overlap policy: allow, skip, or replace/cancel where safely supported.
- Hooks for before, success, failure, skipped, overlap, and completion events.
- Immutable compiled schedule registry and startup validation.

## Distributed Execution

- `OnOneServer` semantics through a distributed lease per schedule occurrence.
- `WithoutOverlapping` semantics through task leases with owner and fencing
  tokens, heartbeat, expiry, and stale-owner recovery.
- PostgreSQL lease adapter using advisory locks or owned tables according to
  required persistence semantics.
- Valkey lease adapter using native `valkey-go` and atomic bounded operations.
- In-memory adapter for deterministic single-process tests.
- Shared conformance suite and explicit backend capability matrix.
- No datastore client types in the scheduler core API.

## Dispatch And Execution

- First-class `queue` dispatcher carrying schedule ID, occurrence, attempt,
  idempotency key, and trace context.
- Optional in-process function/command runner for short operational tasks with
  bounded execution and explicit ownership.
- Optional `idempotency` integration keyed by schedule occurrence.
- Context cancellation, deadline, panic containment, and result classification.
- No shell execution by default; shell commands require an explicit isolated
  adapter and documented injection policy.

## Runtime And Operations

- Dedicated runner suitable for a small Kubernetes Deployment with multiple
  replicas.
- Deterministic tick loop with injectable clock and no one-minute polling
  requirement.
- Graceful drain that stops new dispatch and bounds in-flight schedule decisions.
- Schedule list, next-run, due-run, validate, test, and unlock/recover CLI/API.
- Structured events through `log` and metrics/traces through `telemetry`.
- Operational history contract without requiring an unbounded event store.

## Non-Goals

- No Kubernetes controller or replacement for the CronJob controller.
- No workflow/DAG engine.
- No queue, worker, or retry implementation; durable execution belongs to
  `queue`.
- No exactly-once claim.
- No unrestricted remote code execution or arbitrary shell scheduler.
- No dependency on one database or lock backend in core.

## Package Shape

- Root package: schedule definitions, registry, occurrence, runner, errors.
- `cron`: parser/compiler integration and time-zone behavior.
- `lease`: ownership, fencing, overlap, and backend contracts.
- `postgres`, `valkey`, and `memory` lease adapters.
- `queue`: `queue` dispatch integration.
- `schedulerhttp` and `schedulercli`: optional inspection/control surfaces.
- `schedulertest`: fake clock, fixtures, conformance, and assertions.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Tests MUST prove
due-time calculation, ownership, overlap, missed runs, dispatch, cancellation,
shutdown, and backend failure semantics rather than merely execute lines.

Required verification includes:

- deterministic fake-clock tests across long time ranges
- DST gap/fold, leap-year, month boundary, timezone-data, and clock-jump tests
- multiple-replica lease, failover, partition, expiry, fencing, and race tests
- PostgreSQL and Valkey 9 conformance matrices
- missed-run, bounded catch-up, overlap, shutdown, and deployment-rollout tests
- cron expression, timezone, metadata, and option fuzzing
- parser differential tests against documented cron behavior
- schedule-scale, due-scan, dispatch, and lease-contention benchmarks

## Documentation Deliverables

- Complete API reference and five-minute scheduler quickstart.
- Laravel scheduler migration guide covering named tasks, `onOneServer`,
  `withoutOverlapping`, conditions, environments, maintenance, hooks, and CLI.
- Kubernetes architecture guide explaining Scheduler Deployment versus CronJob.
- Guides for PostgreSQL/Valkey leases, fencing, missed runs, time zones, DST,
  queue dispatch, idempotency, graceful shutdown, and recovery.
- Security, operations, troubleshooting, compatibility, performance, FAQ,
  contribution, examples, and maintained `CHANGELOG.md` documentation.

## Automation And Release

GitHub Actions MUST run formatting, vetting, linting, tests, exact meaningful
coverage, race tests, fuzz smoke tests, PostgreSQL and Valkey 9 matrices,
timezone/DST suites, vulnerability scanning, benchmarks, docs, examples, API
compatibility, and release automation.

## Execution Plan

1. Specify schedules, occurrences, missed runs, overlap, leases, and limits.
2. Implement parser integration, registry, runner, fake clock, and memory lease.
3. Implement PostgreSQL and Valkey leases with fencing and failover tests.
4. Implement `queue`, idempotency, CLI, HTTP, and telemetry integrations.
5. Complete DST, race, fault, rolling-deploy, benchmark, and security hardening.
6. Publish complete Laravel migration and Kubernetes operations documentation.

## Acceptance Criteria

- Named schedules calculate occurrences deterministically across supported zones.
- Multiple replicas produce one current owner and enforce overlap policy.
- Stale owners cannot complete ownership-sensitive operations undetected.
- Queue dispatch, missed runs, shutdown, and recovery are bounded and observable.
- Meaningful 100% coverage and every GitHub Actions gate pass.
- Documentation supports Laravel migration and `CHANGELOG.md` is current.
