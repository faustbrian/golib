# Changelog

All notable changes follow Keep a Changelog. This module uses semantic
versioning once released.

## Unreleased

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

### Changed

- Emit provider-safe `tn2_` lowercase hexadecimal namespaces. Existing `tn1_`
  keys require the documented bounded dual-read and backfill migration.
- Reject system and unscoped values at first-party integration namespace
  boundaries.
- Reject cyclic administrative pagination and bound the total pages inspected
  by one iteration.
- Propagate tenant scope to PostgreSQL connection acquisition and verify it
  again after callbacks so scope left changed rolls back before commit.
- Reject conflicting background-task scopes synchronously instead of silently
  dropping work when no asynchronous error handler is configured.
- Preserve submission values, deadlines, and cancellation in background tasks
  while retaining group-owned shutdown cancellation.
- Generate paired permissive-grant and restrictive PostgreSQL RLS policies so
  the plan grants scoped rows while another permissive policy cannot bypass
  tenant isolation. Existing consumers must migrate from standalone `Create`
  and `Drop` execution to the documented paired statement order.
- Graceful background group closure releases its owned derived context after
  all submitted work has drained.
- Pin both supported OpenSearch integration images by digest so isolation
  evidence cannot drift behind a mutable version tag.

### Added

- Validated, opaque, case-sensitive tenant identifiers with bounded wire
  serialization and redacted diagnostic formatting.
- Immutable tenant, system, and intentionally unscoped operation scopes with
  explicit administrative reasons and system capabilities.
- Conflict-safe context propagation and fail-closed tenant and scope assertions.
- Keyed, collision-resistant namespaces for cache, idempotency, rate-limit,
  search, queue, scheduler, event, workflow, and telemetry boundaries.
- Trust-gated HTTP, JSON-RPC, queue, outbox, Kafka, CloudEvents, audit,
  correlation, idempotency, workflow, event-sourcing, and telemetry propagation
  contracts with deterministic missing, duplicate, conflict, spoofing,
  malformed, overwrite, and size failures.
- Bounded tenant-scoped background groups with explicit graceful close,
  cancellation, shutdown, and task-error ownership.
- Audited, bounded, cancellable, and resumable cross-tenant iteration that
  derives every tenant operation independently from an unscoped base context.
- Explicit PostgreSQL predicates, transaction-local tenant settings with
  readback and pool-reset safety, and migration-owned fail-closed RLS plans.
- Reusable `tenancytest` scope helpers plus property, concurrency, fuzz, and
  allocation benchmark coverage for tenant isolation boundaries.
- Hostile wire fuzzing for HTTP headers and raw JSON-RPC metadata.
- Stateful replay, retry, confused-deputy, namespace, concurrent stress, and
  configurable soak fixtures across every owned integration boundary.
- Redacted default and Go-syntax diagnostics for identities, metadata,
  administrative reasons, capabilities, and complete scopes.
- Redact identities, metadata, administrative reasons, capabilities, and
  complete scopes when passed directly to `log/slog`.
- Compose opaque tenant telemetry namespaces through real OpenTelemetry spans
  and structured logs without raw tenant attributes or values.
- Complete trust, integration, PostgreSQL, administration, migration, security,
  analyzer-boundary, hardening, FAQ, and clean-consumer guidance and tooling.
- Add an executable blocking analyzer policy for direct first-party provider
  construction, replacement contexts, and tenant metric labels, with a narrow
  reviewed-adapter exception fixture.
- Add external-consumer compositions for cache, search, queue/event replay,
  workflow retry, audit attribution, telemetry cardinality, and durable
  administrative fan-out with partial failure and idempotent resume.
- Add a live Redis Streams composition proving tenant identity survives reclaim,
  retry, and dead-letter persistence without cross-tenant queue visibility.
- Add a live PostgreSQL composition proving tenant-bound workflow retry and
  resume plus durable support audit attribution through first-party stores.
- Add a streamed PostgreSQL primary/replica promotion fixture proving an open
  tenant transaction fails during proxy cutover and the same connection pool
  reconnects without retaining scope or exposing another tenant.
- Add an exhaustive owned-surface inventory covering identity, context,
  propagation, namespace, persistence, provider, analyzer, error, diagnostic,
  and administrative escape-hatch boundaries.
- MIT licensing and a private vulnerability-reporting and support boundary.
