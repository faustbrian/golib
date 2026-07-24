# Changelog

All notable changes follow Keep a Changelog. The project uses Semantic
Versioning after v1.0.0.

## [Unreleased]

### Changed

- Refreshed the generated API baseline with the current Go documentation
  formatter without changing exported declarations.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.

### Added

- Immutable weekly rules, overnight ranges, full-day and closed states.
- Dated replace, add, subtract, and closure exceptions with named sets.
- DST-explicit local resolution and bounded instant/transition queries.
- Union, intersection, subtraction, and authoritative overlay algebra.
- Canonical comparison and bounded, provenance-safe human summaries.
- Strict canonical JSON, SQL/pgx JSONB persistence, adapters, and test helpers.
- `calendar` civil-date ownership, bounded zone loading, and fold resolution.
- Explicit DST policy on local queries and injected elapsed observation clocks.
- Structured Location, Track, Postal, and Spatie migration fixtures.
- Transition-waiting guidance using injected `clock` timer capabilities.
- Fuzz, race, mutation, coverage, benchmark, documentation, API, security, and
  PostgreSQL automation.
- Pairwise algebra/conservation properties, exception permutation proof, and a
  broad differential against Go timezone rules.
- A disposable mutation runner with machine-readable evidence, zero-error
  enforcement, and a blocking minimum score.

### Fixed

- Reject duplicate exception source revisions even when another priority sorts
  between them.
- Replace unreachable owned-module pseudo-versions with published revisions so
  clean checkouts can reproduce every gate without local replacements.

[Unreleased]: https://github.com/faustbrian/golib/pkg/opening-hours/commits/main
