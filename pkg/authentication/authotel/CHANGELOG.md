# Changelog

All notable changes to this module are documented here.

## Unreleased

### Distribution

- Include the canonical MIT licence in the independently published module.

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Normalize credential, outcome, and failure dimensions to the documented
  closed value sets; clamp negative durations; complete each attempt exactly
  once under duplicate or concurrent callbacks without making duplicates wait
  for provider work; and isolate provider and observer panics without
  disclosing panic values.
- Define adapter telemetry convention version `1.0.0` without mislabeling it as
  an OpenTelemetry instrumentation-module version or schema URL, and document
  signal stability, bounded-provider prerequisites, provider ownership,
  privacy, cancellation, concurrency, lifecycle, compatibility, and migration
  policy.
- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

- Refresh owned-module checksums against the final consolidated archives.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Refreshed the canonical authentication checksum after its test archive
  changed, preserving isolated module verification.
- Refreshed the canonical authentication checksum after its API compatibility
  baseline was normalized to the module boundary.

### Added

- Add an allocation-aware benchmark for the complete authentication
  instrumentation start-and-finish path.
- Add bounded fuzz coverage for arbitrary credential, outcome, failure, and
  duration values across the complete instrumentation lifecycle.
