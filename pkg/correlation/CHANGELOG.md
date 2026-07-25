# Changelog

All notable changes follow Keep a Changelog. This project uses semantic
versioning once released.

## Unreleased

### Changed

- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.

### Added

- Distinct correlation, request, causation, and external identifier types.
- Secure `identifier` generation and explicit deterministic strategies.
- Context, carrier, HTTP, JSON-RPC, queue, schedule, webhook, log, telemetry,
  and request ID middleware adapters.
- Trust, privacy, multi-hop, retry, fuzz, race, mutation, coverage, allocation,
  compatibility, documentation, and CI gates.
