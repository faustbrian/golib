# Changelog

This project follows Semantic Versioning. Dates use ISO 8601.

## Unreleased

### Changed

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
