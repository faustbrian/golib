# Changelog

All notable changes to this project are documented here. The format follows
Keep a Changelog and semantic versioning.

## Unreleased

### Changed

- Keep standalone module tidiness in the release gate instead of requiring an
  unpublished canonical tag before running local competitor benchmarks.
- Verify optional domain adapters through their independently attributable
  module gates instead of duplicating them in the core integration gate.

### Added

- Typed immutable facts, propositions, compiler, and execution plans.
- Deterministic conflict strategies and bounded forward chaining.
- Canonical JSON AST serialization and SHA-256 hashing.
- Explicit typed operators, fact resolvers, and bounded plan caching.
- Truth-table, hostile-input, race, fuzz, and benchmark suites.
