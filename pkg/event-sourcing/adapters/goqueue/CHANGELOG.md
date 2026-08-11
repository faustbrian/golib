# Changelog

All notable changes to this module will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

### Added

- Add a bounded canonical JSON codec that preserves complete event delivery
  identity for compatible queue backends without importing queue behavior into
  the event-sourcing core.
- Add ordered live-only queue dispatch and task handling with separately named
  replay entry points, exact partial-progress errors, panic containment,
  immutable job options, and queue-owned settlement.
- Prove successful and failed delivery through the repository queue and
  in-memory worker while retaining backend-specific guarantee boundaries.
- Prove complete delivery retention and post-handler acknowledgement through
  digest-pinned Valkey Streams 9.1.0 after the producer worker is closed.
- Expose whether the stopping delivery was not attempted or has unknown queue
  acceptance so retry decisions retain duplicate risk.

### Fixed

- Derive stable event identity for queue failure and dead-letter records,
  preflight the complete first-party queue message bound, reject typed-nil
  queues and unencodable job policy, and copy correlation and trace-context
  metadata across ownership boundaries.
