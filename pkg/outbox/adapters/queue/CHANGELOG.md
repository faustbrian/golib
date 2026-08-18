# Changelog

All notable changes to this module are documented here.

## Unreleased

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

### Added

- Add a bounded, adapter-owned task payload with stable task, idempotency,
  ordering, content, event, schema, and metadata fields.
- Expose acceptance and retry disposition without collapsing unknown backend
  acceptance into a known rejection.
- Verify task round trips through durable Redis Streams and Valkey Streams
  producer paths.
- Prove unacknowledged redelivery, competing PostgreSQL relays, and actual
  subprocess-death windows before and after enqueue and delivery marking for
  both Redis Streams and Valkey Streams.

### Changed

- Replace obsolete owned-module pseudo-version pins with the monorepo's local
  `v0.0.0` source-proxy coordinates; release tooling continues to emit exact
  `v1.0.0` dependency versions.
- Upgrade `golang.org/x/text` to v0.41.0 so the dependency graph no longer
  contains GO-2026-5970.
- Rename the unpublished module from `adapters/goqueue` to `adapters/queue`
  and its Go package to `outboxqueue` before v1.
- Publish the owned task payload instead of `Envelope.CanonicalJSON`, exclude
  relay attempt state from task identity, and attach only operational queue
  metadata without worker retry or scheduling policy.

### Migration

- Consumers must decode `Task` fields instead of the former canonical envelope
  and durably deduplicate `idempotency_key` or `task_id` before side effects.

### Fixed

- Replace unresolved owned-module `v0.0.0` requirements with immutable main
  pseudo-versions so workspace-disabled consumers resolve the adapter.

### Distribution

- Include the canonical MIT licence in the independently published module.

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

- Refresh owned-module checksums against the final consolidated archives.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
