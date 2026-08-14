# Changelog

All notable changes to this module are documented here.

## Unreleased

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

### Breaking

- Replace the ambiguous unversioned `quantity:<amount> <unit>` value with the
  canonical `quantity:v1|<amount>|<unit>` encoding. Persisted values must be
  regenerated from validated measurement quantities.

### Added

- Classify invalid and incompatible quantities with stable adapter errors while
  retaining owned measurement, math, and context causes.
- Document encoding, exact conversion, dimensions, operators, limits, API,
  examples, adoption, security, FAQ, compatibility, and migration.

### Security

- Bound tagged values, reject noncanonical and unknown identities, preserve
  exact conversion, and avoid including supplied amounts or units in adapter
  diagnostics.

### Distribution

- Include the canonical MIT licence in the independently published module.

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Rename the unpublished module from `adapters/gomeasurement` to
  `adapters/measurement` so the path identifies its target without a redundant
  language prefix.
- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

- Refresh owned-module checksums against the final consolidated archives.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Documented the exported quantity operator-name contract.

### Added

- Add an allocation-aware benchmark for exact cross-unit quantity comparison.
- Add bounded fuzz coverage that cross-checks tagged quantity acceptance
  against the canonical measurement parser.
- Add exhaustive unit and incompatible-dimension matrices, ordering properties,
  hostile pair fuzzing, concurrent metadata isolation, and canonical rule
  persistence round trips.

### Fixed

- Reject negative-zero tag spellings that do not match the canonical decimal
  representation emitted by `Quantity`.
