# Changelog

All notable changes to this module are documented here.

## Unreleased

### Added

- Add explicit, loss-reporting Golib conversions for event sourcing, outbox,
  queue, workflow, Kafka, correlation, tenancy, telemetry, and audit metadata.
- Add caller-invoked JSON Schema and schema-registry validation without hidden
  lookup or network access.
- Document collision, trust, ownership, loss, migration, and security policy.
- Enforce Kafka record limits before copying consumed key, value, or header
  metadata.
- Require tenant metadata during trusted extraction and validate audit, queue,
  and event-sourcing tenant identifiers before emission or replay acceptance.

### Distribution

- Include the canonical MIT licence for independent publication.

### Fixed

- Preserve the tenant-specific error classification when malformed queue
  tenant metadata also violates generic queue metadata validation.
