# Changelog

All notable changes to this module are documented here.

## Unreleased

### Added

- Add explicit, loss-reporting Golib conversions for event sourcing, outbox,
  queue, workflow, Kafka, correlation, tenancy, telemetry, and audit metadata.
- Add caller-invoked JSON Schema and schema-registry validation with static URI
  mappings, bounded cache resolution, explicit availability policy, and no
  event-controlled lookup target.
- Document collision, trust, ownership, loss, migration, and security policy.
- Enforce Kafka record limits before copying consumed key, value, or header
  metadata.
- Require tenant metadata during trusted tenant extraction and validate audit,
  queue, and event-sourcing tenant identifiers before emission or replay
  acceptance; tenant remains optional for audit records that do not declare it.

### Changed

- Replace direct `RegistryJSONSchemaValidator` field initialization and the
  `LookupSchema` callback with `NewRegistryJSONSchemaValidator` and
  `RegistryJSONSchemaConfig`. Callers must provide a bounded `ResolveCache`, a
  static URI-to-lookup map, an explicit availability policy, and a positive
  timeout. This is a source-breaking pre-v1 migration that prevents mutable or
  event-controlled resolver selection.
- Sort extension conversion losses by field for deterministic reports.
- Report payload-kind, content-type, and schema loss across canonical envelope
  conversions, and enumerate every unselected canonical audit field.
- Reject retained queue content-type collisions instead of silently replacing
  canonical queue metadata.
- Reject event-sourcing subjects and event-sourcing or queue extensions that
  disappear while their non-empty retained canonical values remain available.
- Preserve nil and present-empty payloads distinctly across outbox, queue, and
  workflow round trips; workflow callers must retain `DataWasNil` with the
  other `WorkflowState` fields.
- Reject non-canonical workflow event type suffixes instead of silently
  normalizing them during a round trip.
- Allow trusted audit metadata extraction to preserve an absent optional tenant.

### Distribution

- Include the canonical MIT licence for independent publication.

### Fixed

- Return the CloudEvents context-required category from both schema validators
  when either is called directly with a nil context, avoiding a registry panic.
- Preserve the tenant-specific error classification when malformed queue
  tenant metadata also violates generic queue metadata validation.
