# Changelog

All notable changes to this project will be documented in this file. The
format follows Keep a Changelog, and stable releases will use Semantic
Versioning.

## Unreleased

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Made the documented idempotency example unambiguously synthetic so strict
  secret scanning does not mistake it for a credential.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Refreshed the canonical authentication checksum after its test archive
  changed, preserving isolated module verification.
- Refreshed owned authentication, authorization, and PostgreSQL checksums after
  their API compatibility baselines were normalized to module boundaries.
- Pinned the completed `queue` dead-letter v1 contracts, stable operational
  failure codes, retention negotiation, and bounded Redis/Valkey redrive
  lineage used by rolling worker fleets.
- Strengthened the deterministic local and hosted quality gate with module
  checksum verification, Staticcheck, strict golangci-lint, advisory NilAway,
  and exact per-package statement coverage.
- Added a global CLI `--output human` mode while retaining compact JSON as the
  stable default and keeping terminal control characters escaped.

### Added

- Fixed-label bounded alert telemetry export for up to 12,000 validated alerts
  without tenant, resource, or error-derived metric labels.
- Attachment disposition on privileged payload and diagnostics responses.
- Real Redis and Valkey concurrent retry/delete evidence proving exactly one
  truthful winner for a contested failed record.
- PostgreSQL child-process death evidence during authorization and live
  dispatcher execution, preserving zero or durably auditable state.
- Allocation-budgeted maximum-payload, 10,000-worker reconnect-storm, and
  backend-outage dispatch benchmarks in the local and hosted smoke gate.
- Distinct durable `unsupported`, `timed_out`, and `partial` command outcomes
  so `queue` acknowledgements cannot be flattened into a clean failure.
- Restart-safe `pending`, `dispatched`, and `acknowledged` command boundaries,
  transition timestamps, pre-dispatch cancellation, and fault-injected
  PostgreSQL recovery evidence.
- Durable bounded command deadlines, authentication methods, required
  capabilities, and worker/protocol acknowledgement snapshots.
- Durable UUID command identifiers distinct from caller-owned idempotency keys,
  including migration backfill and end-to-end result propagation.
- Separate record-list, record-inspection, payload-view, and audit-view
  permissions with exact tenant and object scope.
- Truthful CLI retention status derived from negotiated worker capabilities,
  with unknown numeric limits represented explicitly.
- Independently authorized diagnostics visibility with fail-closed masking
  across the HTTP API, typed client, and CLI.
- Fail-closed chained audit persistence before every privileged payload or
  diagnostics backend read.
- Public administrative command, result, permission, and validation contracts.
- Durable PostgreSQL command, desired-state, and tamper-evident audit storage.
- Bounded worker fleet and rolling-protocol compatibility models.
- Authenticated and authorized HTTP API with health, readiness, version,
  capability, command, audit, worker, and Kubernetes workload surfaces.
- Typed Go client and administrative CLI.
- Narrow namespace-scoped Kubernetes Deployment visibility and scaling.
- Hardened server and CLI container targets for amd64 and arm64.
- Deterministic Go quality, race, coverage, fuzz-smoke, and container CI gates.
- Real PostgreSQL 16, 17, and 18 migration, persistence, idempotency, tenant
  isolation, audit verification, and retention integration gates.
- Reproducible 10,000-worker and 100,000-audit-event load benchmarks with a
  documented development baseline and CI smoke execution.
- Maximum 1,000-worker authenticated HTTP response benchmark with every worker
  advertising the bounded 256-queue limit.
- Maximum-page queue and failure/dead-letter conversion benchmarks in the
  hosted load smoke gate.
- Pinned `govulncheck` source scanning against the canonical Go vulnerability
  database in local and hosted quality workflows.
- BuildKit SBOM and maximal provenance attestations on the multi-platform OCI
  artifact produced by CI.
- Semver-tagged GHCR publication and keyless Sigstore signing for both server
  and CLI images, with immediate workflow-identity verification.
- Targeted goroutine-leak assertions for graceful and forced HTTP server
  shutdown paths.
- Pinned mutation testing with 100% coverage and efficacy across all current
  public command, authorization, desired-state, dispatch, and orchestration
  mutants.
- Migration-only server mode for one-shot production schema Jobs that exit
  before loading serving dependencies.
- Isolated PostgreSQL 18 disaster-recovery gate using native dump and restore
  with repository-level verification of the recovered state.
- Pinned semantic API-diff gate against a reviewed full-module export baseline
  in both CI and release quality.
- Reproducible standalone archives for six operating-system and architecture
  targets with signed checksums and GitHub Release publication.
- Optional secure OTLP runtime wiring for HTTP and bounded control-command
  telemetry with explicit lifecycle ownership and trusted-context policy.
- One-shot production audit retention with strict per-tenant policies, legal
  holds, pre/post verification, bounded batches, and real PostgreSQL coverage.
- Real Chromium security coverage for exact-origin CORS, automatic preflight,
  cookie-backed CSRF, credential admission, and defensive response headers.
- Real Chromium command-envelope coverage for every public mutation and each
  action-specific selection, replay, and scale field.
- Mutation scope excludes installed browser dependencies and test-server code,
  keeping release efficacy measurements limited to owned production decisions.
- Server and CLI container builds now include the imported data-plane adapter
  and embedded UI packages in their restricted build context.
- Newest-first tenant command history with bounded opaque-cursor pagination
  across PostgreSQL, the authenticated API, typed client, and CLI.
- Safe bounded terminal-command retention after verified audit cleanup, while
  preserving active commands and current desired-state references.
- Tenant-scoped `queue` management adaptation for every control action,
  bounded bulk retry, protocol deadlines, and structured terminal outcomes.
- Authenticated failure and dead-letter list and inspect APIs, typed client,
  and CLI with bounded search/sort pagination and privileged payload viewing.
- Authenticated bounded queue-status API, typed client, CLI, and application
  capability wiring with explicit unsupported-measurement representation.
- Optional production worker and queue-status composition through a strict
  tenant-to-HTTPS management document and separate bounded per-tenant
  bearer-token files.
- Optional production tenant command dispatch through the same authenticated
  `queue` management endpoints, with protocol-v1 translation, a bounded
  acknowledgement deadline, and structured terminal outcomes.
- Optional production failure and dead-letter reads through the same tenant
  management client, with validated hidden lists and least-privilege payload
  inspection.
- Pinned native Valkey failed-attempt and dead-letter reads through the
  authenticated management transport, including hidden lists and explicitly
  authorized payload inspection.
- Pinned native Valkey retry, bounded bulk retry, delete, and record-purge
  enforcement with atomic active-failure redrive and structured ambiguity
  outcomes.
- Pinned native Valkey allowlisted replay with durable reject-or-replace
  duplicate handling, plus source and destination authorization before command
  acceptance.
- Pinned native Redis Streams failure and dead-letter reads plus retry,
  bounded bulk retry, allowlisted replay, delete, and record-purge dispatch
  through authenticated `queue` contracts without native client access.
- Authenticated `queue` HTTP integration coverage for managed pause, resume,
  status, and duplicate command enforcement, plus older, current, newer,
  partitioned, and reconnecting protocol observations.
- Pinned Redis 6.2.22 and Valkey 9.1.0 CI services proving real backend status,
  pause, and resume through `queue` and authenticated management HTTP.
- The real Valkey gate creates a failed delivery, dispatches replay, rejects a
  duplicate, preserves the source, and consumes the destination through a
  second public `queue` worker.
- Published `queue` Redis Streams and Valkey Streams status providers, with
  honest backend-version capability reporting, are pinned for the production
  worker and queue-status transport.
- Pinned `queue` retention capability negotiation, including exact-count
  reporting through Redis Streams and rolling-version HTTP integration.
- Authenticated tenant desired-state reads, a typed tenant-bound `queue`
  reader, and native queue-owned pause, resume, drain, and terminate
  convergence through the pinned data-plane lifecycle.
- Bounded alert-input evaluation for queue wait, failures, stale workers, dead
  letters, and failed or unknown control commands.
- Optional embedded same-origin operator console for current worker, queue,
  failure, dead-letter, audit, command-history, and audited mutation workflows,
  with ephemeral credentials and Chromium end-to-end coverage.
- Architecture, API, CLI, deployment, security, operations, compatibility,
  Horizon migration, troubleshooting, contribution, and release documentation.
- Protocol/failure, crash-boundary, administrative threat, scale, Horizon
  ownership, and release-gate hardening matrices with executable evidence.

### Security

- Blocked HTTP redirects in the typed administrative client so bearer tokens
  and custom API-key headers cannot be forwarded to another origin.
- Added exhaustive deployed permission scope, ambiguous request-framing,
  hostile-value XSS, and keyboard accessibility regression coverage.
- Added failure injection for duplicate sensitive mutations, control endpoint
  loss during ordinary delivery, telemetry shutdown, PostgreSQL saturation,
  and 10,000-worker stale/reconnect storms.
- Added real PostgreSQL process-death injection across command, desired-state,
  admission-audit, dispatch, acknowledgement, result, and completion-audit
  writes to prove atomic rollback and honest recovery state.
- Added exact-origin CORS, strict preflight, CSRF checks, defensive response
  headers, request bounds, process-local rate limiting, and secret-safe errors.
- Aligned the shipped CLI with the server's static API-key authentication and
  allowed those headers through approved browser preflights.
- Moved rate-limit admission behind authentication so valid administrative
  requests are isolated by stable subject instead of shared source address.
- Canonicalized persisted timestamps to UTC so idempotent responses and audit
  hashes do not depend on database session or caller timezone offsets.

### Known gaps

- Native queue purge remains unavailable because `queue` intentionally
  exposes only failure/dead-letter record purge for these stream backends.
- Production alert collection/export, desired-state cleanup, historical UI
  charts, retention reconfiguration, and deployment-specific ingress and
  PostgreSQL capacity tests remain intentionally external or incomplete.
