# Goal: Complete Dead-Letter Control-Plane Operations

## Status And Purpose

This is a follow-up execution goal for the in-progress
`queue-control-plane` implementation. It complements `GOAL.md` and
`GOAL_HARDEN.md` without replacing their architecture, security, Kubernetes,
or Horizon-migration requirements.

The repository already contains dead-letter API, client, CLI, command,
authorization, data-plane adaptation, and PostgreSQL command-journal work. This
goal MUST reconcile that live implementation and complete the production path
without discarding valid concurrent work.

## Objective

Deliver safe, complete, deployable operational management of failures and dead
letters through the stable contracts exported by `queue`.

The control plane MUST provide truthful visibility and authenticated,
authorized, idempotent, audited workflows for listing, inspecting, retrying,
replaying, deleting, purging, retaining, and responding to dead-letter backlogs.
It MUST NOT reimplement queue settlement, issue raw broker commands, or become a
second data plane.

## Ownership Boundary

`queue` owns:

- terminal classification, retry exhaustion, and dead-letter settlement;
- backend-native storage and transfer semantics;
- record reading and mutation contracts;
- capability negotiation and backend-specific guarantees;
- worker correctness when the control plane is absent.

`queue-control-plane` owns:

- authenticated and authorized administrative API, CLI, and optional UI;
- tenant-scoped orchestration of `queue` operations;
- confirmations, idempotency, audit, command tracking, and unknown outcomes;
- bounded presentation, filtering, search, and operational history;
- alerting integration, incident workflows, and operator documentation.

The control plane MUST NOT connect directly to Redis, Valkey, NATS, NSQ, or
RabbitMQ to inspect or mutate queue records.

## Required Reconciliation

Before changing behavior:

1. Inventory the current API, typed client, CLI, data-plane resolver, command,
   PostgreSQL journal, documentation, and tests.
2. Identify which surfaces are contracts, mocks, or adapters and which are
   connected to a deployable `queue` implementation.
3. Preserve active implementation work and existing SemVer-governed behavior.
4. Reconcile every operation against `queue` capability negotiation and
   backend-specific guarantees.
5. Convert every known gap into implementation, an explicit unsupported
   capability, or a documented release blocker.

Mock-only success does not satisfy this goal. At least Valkey Streams and Redis
Streams MUST have real end-to-end control-plane evidence; every other advertised
backend operation requires equivalent evidence.

## Stable Administrative Model

Expose bounded stable representations for:

- dead-letter record and failure record;
- backend, queue, tenant, and worker identity;
- terminal classification and safe failure code;
- attempts, timestamps, age, retention deadline, and replay lineage;
- payload visibility and redaction state;
- backend capability and unavailable/unknown fields;
- operation request, command identifier, actor, reason, confirmation,
  idempotency key, status, partial result, and unknown outcome;
- per-record and bounded bulk results.

HTTP, client, CLI, and UI representations MUST remain versioned and consistent.
Unknown backend fields MUST stay unknown rather than receiving fabricated values.

## Listing And Inspection

- Provide tenant-scoped failure and dead-letter list endpoints.
- Use opaque bounded cursors supplied through `queue` contracts.
- Support only documented filters and sort fields negotiated for the backend.
- Validate page size, cursor, search, time range, queue, classification,
  attempts, age, and retention filters before data-plane dispatch.
- Stable pagination MUST not duplicate or omit records silently under concurrent
  insertion or retention; unavoidable backend behavior must be documented.
- Inspection MUST verify tenant, kind, identifier, and capability.
- Payloads and privileged diagnostics are hidden by default.
- Metadata-only access and payload access require distinct permissions.
- Raw payload viewing MUST be explicit, audited, rate-limited, and resistant to
  browser rendering, content-type confusion, and accidental logging.
- The API MUST not deserialize arbitrary job payloads to render a record.

## Retry And Bulk Retry

- Retry preserves the original logical destination and uses `queue` data-
  plane semantics.
- Bulk retry requires a bounded explicit selection, confirmation, reason, and
  idempotency key.
- The backend owns selection and cursor semantics; the control plane MUST NOT
  fetch an unbounded page and loop locally.
- Per-record results distinguish success, conflict, stale record, unsupported,
  not found, backend unavailable, partial, and unknown outcomes.
- Duplicate command submission returns the original durable outcome where the
  idempotency contract permits it.
- Concurrent retry, delete, retention, and replay of the same record MUST not
  produce false success.
- Retried jobs that fail terminally again remain linked and visible without
  unbounded lineage expansion.

## Replay

- Replay always requires an explicit destination and idempotency policy.
- Destination names MUST be validated and authorized independently from source
  record access.
- Reject-duplicate and replace-duplicate policies MUST be capability-negotiated
  and delegated to `queue`.
- Replay requires explicit confirmation, actor, reason, and audit event.
- The control plane MUST never claim exactly-once replay.
- Successful destination enqueue followed by source mutation failure MUST be
  represented as partial or unknown, never as clean failure inviting blind
  repetition.
- Replay rate and concurrency MUST be bounded to avoid recreating a poison storm.
- Cross-tenant replay is forbidden unless a future explicit privileged contract
  is separately designed and defaults to deny.

## Delete, Purge, And Retention

- Single delete requires explicit permission, reason, idempotency, and audit.
- Bulk delete and purge require bounded selection, destructive confirmation,
  capability negotiation, and per-scope authorization.
- Purge MUST identify exact tenant, queue/backend, record kind, and filter scope.
- Empty, wildcard, stale, or unbounded purge requests MUST be rejected.
- Retention policy is configured and enforced by the data plane/backend; the
  control plane presents and administers only exported capabilities.
- Retention changes, cleanup runs, automatic expiry, and manual purge are
  auditable operational events.
- Concurrent retention and operator actions MUST expose stale/conflict/unknown
  results honestly.
- UI/API/CLI MUST communicate irreversible actions and backend limitations.

## Command Lifecycle And Audit

- Every mutation has a durable command ID and idempotency key.
- Commands record actor, authentication method, tenant, target, operation,
  bounded selection, destination, policy, confirmation, reason, requested time,
  deadlines, capability snapshot, acknowledgement, result, and safe failure.
- Command states include pending, dispatched, acknowledged, partial, succeeded,
  failed, timed out, canceled where supported, unsupported, and unknown.
- Process death at validation, authorization, audit, journal, dispatch,
  acknowledgement, and result persistence boundaries MUST be fault-injected.
- Sensitive mutations MUST fail closed if required audit persistence is
  unavailable.
- Audit records are append-only compatible, tamper-evident capable, paginated,
  retained, exportable, and protected from payload leakage.
- Retrying a control command MUST not silently create a new operation when the
  original outcome is unknown.

## Authentication And Authorization

- Use `authentication` for supported administrative authentication.
- Use `authorization` with default deny.
- Define separate permissions for list metadata, inspect metadata, view payload,
  view privileged diagnostics, retry, bulk retry, replay, delete, purge,
  configure retention, and view audit.
- Enforce tenant, queue, backend, destination, and object-level scope.
- Mutation-test every permission and ownership check.
- API, CLI, and UI MUST share the same policy decisions and error semantics.
- Capability visibility MUST not leak queue or tenant existence to unauthorized
  actors.
- Emergency/break-glass access, if implemented, requires short-lived explicit
  elevation and enhanced audit; it is not a default role.

## Payload Privacy And Safe Presentation

- Payload content is hidden by default in API, CLI, UI, logs, traces, metrics,
  errors, command journals, and audit records.
- Privileged payload responses use no-store caching, safe content disposition,
  bounded bytes, neutral content type, and explicit truncation metadata.
- UI rendering MUST treat payload and metadata as untrusted text and prevent
  XSS, HTML execution, URL activation, terminal escape injection, and CSV
  formula injection.
- Download or export requires separate authorization and audit.
- Search MUST not index raw payloads by default.
- Redaction configuration MUST be testable and cannot claim removal from an
  opaque encrypted or encoded payload it did not inspect.
- Retention and privacy deletion obligations must account for dead-letter
  payloads, audit records, exports, and backups explicitly.

## Metrics, Alerts, And Incident Operations

Expose through `telemetry` where honestly measurable:

- dead-letter depth and oldest age;
- terminal rate by bounded classification;
- dead-letter settlement failure and unknown outcomes;
- retry/replay rate, success, repeated failure, conflict, and partial outcome;
- command latency, timeout, audit failure, and authorization denial;
- retention deletion and capacity pressure;
- unavailable or unsupported backend capability.

Queue names, tenants, record IDs, payloads, actor identities, error text, and
unbounded classifications MUST NOT become metric labels.

Provide configurable alerts and runbooks for:

- sudden terminal-failure spikes;
- oldest-age or depth thresholds;
- dead-letter destination outage;
- poison-message/retry storms;
- repeated redrive failure;
- retention or storage pressure;
- control-command timeout or unknown outcomes;
- unavailable record reader or backend capability drift.

Alert delivery belongs through existing telemetry/webhook integrations, not a
new unbounded notification system in this package.

## Scale And Resource Bounds

- Bound page size, search length, cursor size, filter count, time range,
  selection size, payload bytes, export size, concurrent commands, command fan-
  out, replay rate, audit history, retention history, response bytes, and UI
  rendering.
- Large backlogs MUST remain paginated and responsive without loading all
  records into memory or PostgreSQL.
- Apply rate limits separately to reads, payload views, retries, replays,
  deletes, purges, and exports.
- Cancellation and deadlines propagate through API, command journal,
  data-plane dispatch, and response handling.
- PostgreSQL and Valkey/control-state outages degrade safely and never bypass
  authorization, confirmation, idempotency, or audit requirements.

## API, Client, CLI, And UI Requirements

- Every administrative workflow is available through the versioned HTTP API.
- The typed client covers every API field, error, page, visibility mode, and
  command state without lossy conversion.
- The CLI covers listing, inspection, payload access, retry, bulk retry, replay,
  delete, purge, retention status, command status, and audit lookup.
- CLI output supports safe human-readable and stable machine-readable modes.
- Optional UI consumes only the public API and does not bypass policies.
- API schema, generated clients, CLI help, examples, and docs stay synchronized
  through automated checks.
- Unsupported backend operations are disabled or rejected with capability
  explanations, never presented as successful no-ops.

## Testing And Verification

Meaningful 100% production Go coverage remains mandatory. UI behavior requires
meaningful component and end-to-end coverage. Tests MUST prove operational and
security behavior rather than only execute routes.

Required evidence includes:

- real end-to-end Valkey Streams and Redis Streams list, inspect, retry,
  replay, delete, purge, and capability tests through `queue`;
- equivalent integration tests for every other advertised backend capability;
- rolling-version worker/control-plane negotiation tests;
- duplicate, reordered, delayed, stale, malformed, unsupported, partial, and
  unknown command-result tests;
- process-death fault injection at authorization, audit, journal, dispatch,
  acknowledgement, data-plane settlement, and result persistence boundaries;
- authorization mutation matrix for every action and tenant/object scope;
- payload disclosure, XSS, terminal escape, content-type, cache, export, CSRF,
  CORS, request-smuggling, injection, and rate-limit tests;
- concurrent retry/replay/delete/purge/retention race scenarios;
- API and cursor fuzzing with hostile record metadata and backend output;
- PostgreSQL migration, retention, backup, restore, and audit recovery tests;
- large-backlog, payload-size, command-fan-out, reconnect-storm, and backend-
  outage benchmarks with allocations and explicit budgets;
- UI/API/CLI consistency and critical-workflow end-to-end tests;
- contract tests against the versioned `queue` dead-letter surface.

All mandatory CI commands MUST be reproducible locally. Existing formatting,
vet, Staticcheck, strict golangci-lint, advisory NilAway, meaningful coverage,
race, fuzz, mutation, browser security, PostgreSQL, queue integration,
vulnerability, dependency, SBOM/provenance, benchmark, API compatibility,
container, documentation, and release gates MUST include these workflows.

## Documentation Deliverables

- Complete failure/dead-letter operator guide and five-minute deployment
  walkthrough.
- API, typed-client, CLI, UI, permission, command-state, error, pagination,
  filter, payload-visibility, replay, retention, and audit reference.
- Data-plane/control-plane ownership and capability-negotiation documentation.
- Backend operation and durability matrix sourced from `queue` evidence.
- Laravel Horizon migration guide covering failed jobs, retry, retry all,
  forget, flush, pruning, retention, metrics, notifications, and differences.
- Security guide for payload privacy, tenant isolation, break-glass access,
  destructive actions, exports, backups, and incident audit.
- Runbooks for destination outage, backlog growth, poison storms, repeated
  replay failure, command unknown outcomes, retention pressure, and recovery.
- Upgrade, rolling compatibility, backup/restore, disaster recovery,
  troubleshooting, FAQ, performance, examples, and changelog updates.

Every API, CLI, and UI workflow MUST include examples and failure guidance so an
operator can adopt it without reading implementation source.

## Execution Plan

1. Reconcile the live control-plane implementation and `queue` capability
   contracts without disturbing concurrent work.
2. Complete the concrete production record-reader and mutation transports.
3. Complete API, client, CLI, command, authorization, idempotency, audit, and
   PostgreSQL lifecycle behavior.
4. Implement retention administration, metrics, alerts, and optional UI
   workflows through public contracts.
5. Prove real backend, rolling-version, failure-boundary, security, privacy,
   concurrency, and scale behavior.
6. Complete Horizon migration, operator, incident, and recovery documentation.

## Acceptance Criteria

- Production deployments can list and inspect failures and dead letters through
  concrete tenant-scoped `queue` transports.
- Retry, bulk retry, replay, delete, purge, and retention operations are
  capability-negotiated, authorized, idempotent, audited, bounded, and truthful.
- Unknown and partial outcomes cannot be mistaken for success or clean failure.
- Payloads are private by default across every interface and persistence layer.
- No raw backend queue operation exists outside `queue`.
- Control-plane failure cannot corrupt worker delivery or dead-letter storage.
- Every advertised workflow has real backend and end-to-end evidence.
- Meaningful coverage and every required local and GitHub Actions gate pass.
- Documentation, API schema, client, CLI, UI, and changelog remain synchronized.
