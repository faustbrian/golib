# Changelog

All notable changes to this module are documented here.

## Unreleased

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
