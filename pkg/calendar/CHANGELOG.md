# Changelog

All notable changes follow Keep a Changelog. The project uses semantic
versioning after the first stable release.

## Unreleased

### Changed

- Replaced host Ruby documentation validation with a self-contained Go link
  checker that uses the module's declared toolchain.

### Added

- Immutable bounded `Date` and typed calendar periods.
- Explicit clamp, reject, and overflow arithmetic policies.
- DST gap/fold detection and bounded IANA loading.
- Immutable revisioned business calendars and observance policies.
- SQL and native pgx PostgreSQL date codecs with distinct infinity support.
- Clock, temporal, config, validation, wire, and test adapters.
- Exhaustive Gregorian, 19-case mutation, fuzz, race, integration, and
  benchmark gates.
- Blocking allocation budgets for core, business, timezone, wire, and pgx hot
  paths.
- Historical second-offset and date-line timezone vectors plus broad standard
  library differential coverage.
- Shared codec and generated-corpus concurrency proofs, plus an explicit
  business compatibility report.
- Hostile-input fuzzing for every typed calendar parser.
- All-year quarter, semester, and policy-permitted arithmetic inverse proofs.
- Refreshed PostgreSQL 14-18 image pins and actionable integration startup
  diagnostics.
- Portable fuzz and provenance gates without undeclared runner tools.
