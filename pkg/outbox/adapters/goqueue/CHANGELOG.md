# Changelog

All notable changes to this module are documented here.

## Unreleased

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
