# Changelog

All notable changes are documented here. The format follows Keep a Changelog,
and releases use Semantic Versioning.

## [Unreleased]

### Changed

- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.

### Added

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
