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
