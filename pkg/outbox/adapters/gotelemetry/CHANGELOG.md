# Changelog

All notable changes to this module are documented here.

## Unreleased

### Distribution

- Include the canonical MIT licence in the independently published module.

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.

### Added

- Add an allocation-aware benchmark for low-cardinality outbox operation
  telemetry.
- Add bounded fuzz coverage for metadata copying, event metrics, backlog
  snapshots, and wrapped publication lifecycles.
