# Changelog

All notable changes follow Keep a Changelog conventions. Compatibility follows
semantic versioning after the first tagged release.

## Unreleased

### Changed

- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Replaced coverage-only defensive exclusions with tested internal invariant
  assertions, while retaining returned errors for every caller-reachable
  invalid input and arithmetic failure.
- Validate the complete pinned currency dataset against the supported money
  scale instead of carrying an unreachable per-call scale branch.
- Make encoding invariants and CLDR fallback parsing directly testable while
  preserving strict duplicate-key, delimiter, and formatted-output bounds.
- Removed the annotation-based defensive-statement coverage exclusion and
  require exact deduplicated coverage for every production package.

### Added

- Immutable exact `Money`, `Amount`, `RationalMoney`, `MoneyBag`, rate, ratio,
  context, allocation, tax, discount, conversion, and result values.
- ISO default, custom, cash, and safe automatic precision contexts.
- Deterministic equal and weighted allocation with signed conservation.
- Explicit tax, discount, cash-rounding, and attributed FX boundaries.
- Locale formatting, versioned JSON/text/SQL persistence, PostgreSQL numeric
  support, and reusable `moneytest` laws.
- Property, exhaustive currency, fuzz, race, mutation, coverage, benchmark, and
  compatibility gates.
