# Changelog

All notable changes follow Keep a Changelog. Versions follow semantic
versioning once v1 is released.

## Unreleased

### Distribution

- Include the canonical MIT licence in the independently published module.

### Changed

- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Removed package-local quality-tool dependencies now that repository tooling
  is versioned and executed exclusively by the root command surface.
- Run API compatibility with an explicitly pinned tool so isolated release
  checks do not depend on undeclared host-installed Go tools.
- Use the repository-pinned current `apidiff` revision for the canonical API
  compatibility gate.

### Added

- Immutable typed schemas, requests, plans, canonical JSON, structured errors,
  authorization hooks, mandatory constraints, and conservative query costs.
- Bounded field selection, relationship paths, typed filter expressions,
  deterministic sorts, cursor and offset page requests.
- Authenticated encrypted versioned cursors, rotation, replay hooks, nullable
  positions, and stable response page envelopes.
- Strict HTTP and JSON-RPC parsers, OpenRPC descriptors, authoritative
  `jsonapi` composition, cross-transport conformance, and `validation`
  reporting.
- Safe PostgreSQL primitives, SQLC guidance, test builders, canonical
  conformance helpers, and real PostgreSQL safety tests.
- Exact production coverage, race, fuzz, mutation, vulnerability, compatibility,
  documentation, benchmark, and release automation.

### Security

- Hostile input suites cover injection, authorization, tenant isolation,
  traversal, schema probing, cursor tampering/replay, Unicode, and resource
  exhaustion.
