# Changelog

All notable changes to this module are documented here.

## Unreleased

### Distribution

- Include the canonical MIT licence in the independently published module.

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Refresh owned-module checksums against the final consolidated archives.
- Refresh the parent Knapsack checksum used by clean consumer builds.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.

### Added

- Add an allocation-aware benchmark for exact packaging-cost aggregation
  across a representative multi-container plan.
- Add bounded fuzz coverage for hostile container type identifiers and exact
  cost-map limit enforcement.
