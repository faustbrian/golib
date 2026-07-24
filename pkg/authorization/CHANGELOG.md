# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog, and this project follows semantic
versioning for its Go API and portable policy format.

## Unreleased

### Changed

- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Use the repository-pinned current `apidiff` revision for the canonical API
  compatibility gate.

### Added

- Typed authorization requests, outcomes, reasons, explanations, and limits.
- Four explicit policy combining algorithms with exhaustive truth tables.
- Immutable revisioned snapshots and atomic optimistic engine replacement.
- Tenant-safe typed ACL evaluation, groups, batches, and resource-ID listing.
- Tenant-safe RBAC, bounded inheritance, effective permission inspection, and
  revisioned in-memory assignment administration.
- Closed typed ABAC conditions with versioned reusable conditions and bounded
  cost, depth, match, batch, and collection cardinality.
- Snapshot diff and decision dry-run support.
- Strict `authorization.policy/v1` JSON envelope and storage-neutral repository
  contract.
- Bounded manifest compiler with a copied, explicit model decoder registry.
- Strict versioned ACL, RBAC, and ABAC documents and built-in compiler
  decoders.
- PostgreSQL manifest repository with atomic optimistic updates and a reusable
  schema migration.
- Monotonic Valkey invalidation with durable revision polling and pub/sub
  wakeups.
- Repository synchronizer with direct source-of-truth polling and verified
  invalidation hints.
- Configurable fail-closed repository freshness enforcement for synchronizer
  authorization, including explicit stale-policy decisions.
- Fail-closed `net/http` integration with explicit request mapping and separate
  denial and internal-error handlers.
- Canonical `authhttp` package, dependency-neutral authenticated-principal
  mapper, and native fail-closed `jsonrpc` middleware.
- Failure-isolated decision instrumentation, bounded `log` audit events,
  `telemetry`-compatible OpenTelemetry metrics and spans, and an explicit
  advisory `cache` manifest adapter.
- Deterministic `authorizationtest` request builders, evaluators, assertions,
  canonical decision snapshots, and authorizer conformance suites.
- Independently bounded matched-policy diagnostics with explicit truncation
  propagated through engines, dry runs, snapshots, logs, and telemetry.
- Fail-closed evaluator panic containment and hostile-input fuzz coverage for
  every portable policy decoder.
- Positive snapshot revisions and pre-parse model/manifest byte limits, plus
  compiler policy-count and aggregate-document limits.
- Iterative ABAC condition preflight that enforces configured depth before
  descending into typed or portable condition trees.
- Pinned whole-module mutation testing with measured efficacy and mutant
  coverage gates.
- Shared ACL, RBAC, and ABAC model conformance, rolling-revision differential
  tests, and cold, warm, batch, inheritance, predicate, reload, compiler, and
  policy-size benchmarks.
- Complete API, model-selection, application-pattern, lifecycle, operations,
  security, troubleshooting, compatibility, and governance guides; a compiled
  multi-model example; and dedicated documentation and example automation.
- Formal decision tables, tenant-isolation evidence, a maintained hardening
  report, explicit stale-policy guidance, and versioned compatibility corpora.
- Executable consumer contracts for published `authentication`, `log`,
  and `telemetry` modules, including authentication claim collections.
- Environment-gated PostgreSQL and Valkey integration tests, manifest fuzzing,
  a decision benchmark, exact-coverage enforcement, and CI quality gates.
- API compatibility enforcement, reproducible release archives, release
  automation, security guidance, and an explicit threat model.
