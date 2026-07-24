# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and releases follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Added the `GO-SAFETY-1` ownership, concurrency, race, fuzz, resource, and
  benchmark standard with an executable `make safety` gate.
- Moved AI planning and hardening briefs into `.ai/` and clarified the
  separate purposes of ownership notices and detailed source provenance.

### Added

- A standardized OSS repository skeleton covering policy, documentation,
  legal notices, Go tooling, pinned CI, security, and release automation.
- Gated, disk-backed benchmarks for CSV and XLSX inputs of at least 50 MiB and
  100,000 rows, including a scheduled workflow with peak-memory reporting.
- Explicit chunked-streaming regressions, malformed fixtures for every major
  format, and documentation of benchmark inputs and XLSX heap amplification.
- Production readers for CSV/delimited, fixed-width, XLS, XLSX, and ZIP-backed
  ingestion with explicit limits, normalization, and structured errors.
- Realistic fixtures, hostile-input regressions, format fuzz targets,
  representative benchmarks, and 100% production-statement coverage.
- Adoption, API, architecture, format, behavior, troubleshooting, migration,
  versioning, and scenario documentation.
- Initial package goals for `tabular`, covering CSV, XLS, XLSX,
  fixed-width, and ZIP-backed ingest as the first supported scope.
- Hardening goals covering hostile-input handling, encoding discipline,
  fixture quality, performance validation, and meaningful 100% coverage.
- Package maintenance rules enforcing changelog hygiene, SemVer treatment of
  public APIs, and meaningful 100% coverage for production code.

### Fixed

- Avoid redundant row copies when CSV normalization is disabled and use a
  bounded 64 KiB source buffer to improve large-file throughput.
- Bound fuzz-smoke concurrency to avoid deadline flakes on high-core hosts.
- Avoid per-cell XLSX type lookups for ordinary values, substantially reducing
  runtime, allocations, and peak memory for large workbooks.
- Classify corrupt ZIP entry read failures through `ErrorArchive` while
  preserving the standard library's declared-size boundary.

[Unreleased]: https://github.com/faustbrian/golib/pkg/tabular/commits/main
