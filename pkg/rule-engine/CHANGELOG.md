# Changelog

All notable changes to this project are documented here. The format follows
Keep a Changelog and semantic versioning.

## Unreleased

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

### Changed

- Delegate core and adapter mutation checks to the canonical exact-100
  repository runner instead of package-specific thresholds and exclusions.
- Keep standalone module tidiness in the release gate instead of requiring an
  unpublished canonical tag before running local competitor benchmarks.
- Verify optional domain adapters through their independently attributable
  module gates instead of duplicating them in the core integration gate.
- Let isolated compilers canonically serialize and parse definitions that use
  their registered custom operators while preserving built-in-only package
  helpers.

### Added

- Typed immutable facts, propositions, compiler, and execution plans.
- Deterministic conflict strategies and bounded forward chaining.
- Canonical JSON AST serialization and SHA-256 hashing.
- Explicit typed operators, fact resolvers, and bounded plan caching.
- Truth-table, hostile-input, race, fuzz, and benchmark suites.
