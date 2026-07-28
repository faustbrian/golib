# Changelog

All notable changes to this module are documented here.

## Unreleased

### Changed

- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.
- Serialize startup, readiness, publishing, and consumer run callbacks with
  resource shutdown so concurrent stop cannot close a resource still in use.

### Added

- independently versioned service lifecycle adapter for explicit Kafka
  producers and consumers
- bounded startup, readiness, drain, retryable shutdown, task supervision,
  correlation propagation, and caller-owned trace propagation
- secret-safe panic containment for every application callback, including
  startup cleanup and retryable shutdown after panic
- pre-copy and post-propagation record validation, bounded UTF-8 lifecycle
  names, and non-aliasing consumed-header propagation
- deterministic lifecycle, concurrency, ownership, fuzz, compatibility, and
  allocation evidence
