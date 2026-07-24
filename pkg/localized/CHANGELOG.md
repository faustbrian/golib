# Changelog

All notable changes follow Keep a Changelog and Semantic Versioning.

## [Unreleased]

### Changed

- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Refreshed the canonical HTTP client checksum after its boundary tests
  changed, preserving isolated module verification.
- Refreshed the canonical API query checksum after its API compatibility
  tooling was standardized.

### Added

- Immutable localized text, explicit matching and fallback, deterministic
  encoding, validation, persistence, HTTP, config, wire, and test adapters.
- Bounded hostile-input, race, fuzz, mutation, PostgreSQL, benchmark, and
  compatibility gates.
- Public locale identity and registry provenance through
  `international/locale`.
- Typed `validation` findings with canonical locale paths and content-free
  diagnostic codes.
- Exact, presence-aware `api-query` values and predicates without implicit
  matching or persistence policy.
- Canonical bounded `Accept-Language` integration for immutable http-client
  request specs and standard responses.
- Strict ordered-pair construction with explicit duplicate and limit options.
- Enforced allocation ceilings for construction, lookup, matching, fallback,
  merge, and canonical JSON operations.
- Property fuzzing for canonicalization, merge identities, deterministic order,
  equality, hashes, and canonical round trips.
- Reproducible dependency pins matching the exact locally verified sibling
  revisions.

## [1.0.0] - 2026-07-16

### Added

- Initial production contract for localized domain values.

[Unreleased]: https://github.com/faustbrian/golib/pkg/localized/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/faustbrian/golib/pkg/localized/releases/tag/v1.0.0
