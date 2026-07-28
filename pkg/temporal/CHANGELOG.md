# Changelog

This project follows Semantic Versioning. Dates use ISO 8601.

## Unreleased

### Fixed

- Resolve API compatibility checks against the active monorepo workspace
  instead of downloading unpublished internal module versions.
- Parse duration component counts directly at the platform integer width before
  multiplication, eliminating narrowing conversions from hostile input.
- Run API compatibility through the isolated versioned-tool path so clean
  verification snapshots do not leak module flags into `apidiff`.

### Changed

- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

- Validate unsigned rounding modes with their meaningful upper bound only.
- Refresh owned-module checksums against the final consolidated archives.
- Use deterministic execution counts for default fuzz smoke campaigns while
  allowing explicit duration overrides for extended fuzzing.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.

### Added

- Explicit four-mode bounds and exhaustive Allen relations.
- Immutable instant and civil-date periods and normalized sets.
- Fixed durations, local times, circular daily intervals, and complements.
- Strict ISO 8601, ISO 80000, Bourbaki, JSON, SQL, and pgx adapters.
- `calendar`, `config`, `validation`, and format-neutral wire seams.
- Explicit civil snapping, local-time/daily application, and versioned set
  documents.
- Differential PHP fixtures, property/fuzz/race/mutation/benchmark gates.
- Exhaustive convenience-predicate tables, associative algebra properties, and
  a reproducible hardening evidence report.
- A generated, pinned inventory and behavior classification for every
  non-chart public PHP symbol.

### Compatibility

- PHP terminal and Gantt chart rendering is deferred. Full PHP-package
  compatibility is not claimed until an optional future renderer closes it.
