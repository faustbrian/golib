# Changelog

All notable changes are documented here. The format follows Keep a Changelog,
and releases use Semantic Versioning.

## [Unreleased]

### Changed

- Deep-copy nested schedule parameters when compiling a registry so caller
  mutations cannot alter compiled execution input.
- Classify task-lease heartbeat failures deterministically before canceling the
  managed execution.
- Bound the initial history-buffer allocation independently from its accepted
  maximum capacity.
- Strengthen lease-store conformance checks and exact scheduler, adapter, CLI,
  HTTP, telemetry, and lifecycle boundary coverage.
- Restore immutable main pseudo-version pins for every owned dependency so the
  current scheduler source resolves from a clean external module.
- Release managed execution capacity before completing an occurrence so
  single-worker catch-up does not reject the next cooperative execution.
- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

- Upgrade gRPC to 1.82.1 to remove the reachable `GO-2026-6061`
  vulnerabilities.
- Pin unpublished owned modules to exact resolvable `main` revisions so clean
  scheduler consumers no longer require nonexistent `v0.1.0` tags.
- Refresh owned-module checksums against the final consolidated archives.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.

### Added

- Composable application-owned pause controls with process-local state,
  per-schedule exemptions, fail-closed lookups, and typed skipped events.
- A deterministic registry overview API that combines immutable definitions
  with each enabled schedule's next run for caller-owned control surfaces.
- A distinct `After` / `EventFinished` execution boundary and background
  metadata while retaining `Completed` for every scheduling decision.
- Laravel-compatible `OnOneServer`, `WithoutOverlapping`, and
  `RunInBackground` controls with independent mutex TTLs, managed asynchronous
  lifecycle reporting, and CLI bulk overlap-lock cleanup.
- Laravel-compatible frequency helpers from seconds through yearly schedules,
  plus weekday, recurring time-window, and skip constraints that compose with
  existing environment and timezone options.
- `Runner.RunFrom` for bounded startup catch-up that transitions into the
  continuous schedule loop without losing an occurrence between both phases.
- Configurable overlap-lease heartbeat intervals with construction-time TTL
  safety validation.
- `schedulerservice` lifecycle composition that stops scheduling, drains active
  executions before owned facilities close, and applies the existing schedule
  correlation semantics to every occurrence.
- PostgreSQL lease stores and migrations can target an explicit caller-owned
  schema while preserving the public-schema default.
- code-defined versioned schedules with deterministic timezone-aware timing
- fenced memory, PostgreSQL, and Valkey 9 lease adapters
- bounded missed-run and overlap decisions
- `queue`, `idempotency`, `log`, and `telemetry` integration
- HTTP and CLI inspection and fenced recovery surfaces
- bounded history, hooks, fake clock, and lifecycle observability
- explicit definition, registry, catch-up, and occurrence-scan resource limits
- task-lease heartbeats and safe overlap-replacement capability contracts
- public cron compiler with typed expression and time-zone errors
- rollout-stable coordination identity across revision and timing changes
- bounded lease calls, callbacks, and managed non-cooperative executions
- complete Gregorian-cycle cron search for non-leap century boundaries
- multi-replica, crash-window, and live backend fault conformance suites
- threat model, rollout and crash matrices, and benchmark release baseline
- bounded runner observer registration
