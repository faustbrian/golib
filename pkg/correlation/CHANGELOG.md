# Changelog

All notable changes follow Keep a Changelog. This project uses semantic
versioning once released.

## Unreleased

### Fixed

- Return fresh correlation and request identifiers on explicitly rejected
  malformed HTTP metadata without invoking application handlers.
- Avoid allocating header-value storage when optional inbound correlation
  metadata is absent and reuse canonical header storage when it is safe.

### Changed

- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.

### Added

- Distinct correlation, request, causation, and external identifier types.
- Secure `identifier` generation and explicit deterministic strategies.
- Context, carrier, HTTP, JSON-RPC, queue, schedule, webhook, log, telemetry,
  and request ID middleware adapters.
- Trust, privacy, multi-hop, retry, fuzz, race, mutation, coverage, allocation,
  compatibility, documentation, and CI gates.
