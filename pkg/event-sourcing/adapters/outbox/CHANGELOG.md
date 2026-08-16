# Changelog

All notable changes to this module are documented here.

## Unreleased

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

### Removed

- Remove the pre-release committed `Store`; callers now retain exclusive
  transaction, commit-ambiguity, aggregate acknowledgement, and dispatch
  ownership through `Stager`.

### Changed

- Upgrade `golang.org/x/text` to v0.41.0 so the dependency graph no longer
  contains GO-2026-5970.
- Rename the unpublished module from `adapters/gooutbox` to
  `adapters/outbox` and its Go package to `eventoutbox` before v1.
- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.
- Isolate every staging attempt in a PostgreSQL savepoint so mapping, database,
  cancellation, and savepoint failures cannot leave a one-sided batch
  committable by the caller while outer rollback remains caller-owned.
- Resolve custom topics from the new pre-persistence `TopicMessage` contract
  before acquiring stream locks. Custom resolvers must change their parameter
  from `eventsourcing.Message` to `eventoutbox.TopicMessage`; store-assigned stream
  versions and global positions are no longer available for routing.

### Added

- failure-injection, concurrency, ambiguity-recovery, process-death, relay
  duplication, hostile-codec, and realistic batch-performance evidence
- a digest-pinned PostgreSQL 18.4 fixture for same-transaction integration
  evidence
- same-transaction event and outbox staging directly from a prepared core
  aggregate save plan
- real PostgreSQL benchmark isolating same-transaction outbox append overhead
- complete committed-store-to-relay composition guidance and real PostgreSQL
  evidence for transient retry, delivery, and replay isolation
- atomic committed event-and-outbox storage through `Store`
- caller-owned same-transaction staging through `Stager`
- deterministic bounded event-message envelope mapping and validation
- explicit commit-outcome, rollback, replay, and at-least-once semantics
