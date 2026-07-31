# Changelog

All notable changes follow Keep a Changelog. This project uses semantic
versioning once released.

## Unreleased

### Fixed

- Trust the canonical default UUID generator while validating custom generator
  output once with a byte-oriented ASCII policy scan.
- Return fresh correlation and request identifiers on explicitly rejected
  malformed HTTP metadata without invoking application handlers.
- Avoid allocating header-value storage when optional inbound correlation
  metadata is absent and reuse canonical header storage when it is safe.

### Changed

- Pin the owned identifier module to an immutable source revision so
  correlation resolves from a clean external consumer without `go.work`.

- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.

### Added

- Distinct correlation, request, causation, and external identifier types.
- Secure `identifier` generation and explicit deterministic strategies.
- Context, carrier, HTTP, JSON-RPC, queue, schedule, webhook, log, telemetry,
  and request ID middleware adapters.
- Trust, privacy, multi-hop, retry, fuzz, race, mutation, coverage, allocation,
  compatibility, documentation, and CI gates.
