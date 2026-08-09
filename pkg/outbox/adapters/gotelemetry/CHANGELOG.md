# Changelog

All notable changes to this module are documented here.

## Unreleased

### Security

- Exclude envelope identities, destinations, contents, arbitrary metadata,
  errors, and panic values from telemetry; restrict propagation to W3C trace
  context keys and bound every metric dimension.

### Changed

- Version the instrumentation scope and replace raw attempt counts with fixed
  retry-state buckets.
- Preserve caller cancellation, exact publication results, downstream panics,
  and optional publisher health checks while isolating telemetry provider
  failures and lifecycle state.
- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.
- Normalize standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.

### Documentation

- Document instruments, cardinality, privacy, semantics, failure isolation,
  lifecycle ownership, API usage, adoption, compatibility, migration, security,
  and frequently asked questions.

### Fixed

- Replace the unresolved Outbox `v0.0.0` requirement with an immutable main
  pseudo-version so workspace-disabled consumers resolve the adapter.

### Distribution

- Include the canonical MIT licence in the independently published module.

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Added

- Add an allocation-aware benchmark for low-cardinality outbox operation
  telemetry.
- Add bounded fuzz coverage for metadata copying, event metrics, backlog
  snapshots, and wrapped publication lifecycles.
