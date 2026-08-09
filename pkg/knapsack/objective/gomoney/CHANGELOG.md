# Changelog

All notable changes to this module are documented here.

## Unreleased

### Added

- Add explicit negative-cost policy, duplicate-aware entry construction, exact
  empty-plan totals, and module adoption and API documentation.
- Add allocation-aware total and comparison benchmarks plus bounded fuzz and
  property coverage for exact mappings and hostile identifiers.

### Fixed

- Reject unsupported money contexts and retain specific validation and
  arithmetic error identities through objective evaluation. Mixed-sign totals
  no longer depend on container iteration order near amount bounds.

### Distribution

- Include the canonical MIT licence in the independently published module.

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

- Pin unpublished Money and owned transitive modules to exact resolvable
  revisions so clean consumers do not depend on missing `v0.1.0` tags.
- Refresh owned-module checksums against the final consolidated archives.
- Refresh the parent Knapsack checksum used by clean consumer builds.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
