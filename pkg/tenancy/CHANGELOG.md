# Changelog

All notable changes follow Keep a Changelog. This module uses semantic
versioning once released.

## Unreleased

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
