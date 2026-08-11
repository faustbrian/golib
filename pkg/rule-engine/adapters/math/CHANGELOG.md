# Changelog

All notable changes to this module are documented here.

## Unreleased

### Breaking

- Replace the unversioned `decimal:` value tag and `decimal_*` operator names
  with the collision-resistant v1 persistence contracts documented in the
  module migration guide.

### Added

- Add caller-selected decimal limits with stable error identities for invalid
  tags, noncanonical payloads, decimal syntax, resource limits, and
  cancellation.
- Document encoding, exactness, operators, composition, adoption, security,
  compatibility, migration, and common integration questions.

### Distribution

- Include the canonical MIT licence in the independently published module.

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Rename the unpublished module from `adapters/gomath` to `adapters/math` so
  the path identifies its target without a redundant language prefix.
- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

- Refresh owned-module checksums against the final consolidated archives.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Documented the exported decimal operator-name contract.

### Added

- Add an allocation-aware benchmark for exact decimal operator evaluation.
- Add bounded fuzz coverage that cross-checks tagged decimal acceptance
  against the canonical decimal parser.
- Add canonical RuleSet round-trip compatibility, cross-engine race isolation,
  hostile-value redaction, unequal-operand fuzzing, and exact parser benchmarks.

### Fixed

- Canonicalize positive-exponent zero as `0` so every value emitted by
  `Decimal` is accepted by the v1 operators.
- Reject digit input when its configured parser budget is exhausted, before an
  attacker-sized coefficient string can be allocated.
