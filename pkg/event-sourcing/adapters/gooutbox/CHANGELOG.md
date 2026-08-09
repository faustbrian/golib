# Changelog

All notable changes to this module are documented here.

## Unreleased

### Removed

- Remove the pre-release committed `Store`; callers now retain exclusive
  transaction, commit-ambiguity, aggregate acknowledgement, and dispatch
  ownership through `Stager`.

### Changed

- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

### Added

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
