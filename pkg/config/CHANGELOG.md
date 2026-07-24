# Changelog

All notable changes are documented here. The format follows Keep a Changelog
and releases use Semantic Versioning.

## [Unreleased]

### Changed

- Replaced obsolete package-local workflow references with the authoritative
  root CI matrix and its canonical per-module quality contract.

### Added

- Typed immutable configuration plans and snapshots with safe provenance.
- Strict JSON, YAML, TOML, dotenv, environment, filesystem, defaults, and
  programmatic sources.
- Explicit bounded discovery, merge semantics, validation, optional values,
  redacted secrets, byte sizes, test helpers, fuzzing, race tests, and
  benchmarks.
- Exact coverage, compatibility, security, documentation, and release gates.
- Canonical source-tree enforcement, private mutable-state rejection, and
  secret-safe cause wrappers across every public parser and conversion error.
- Runnable source-composition examples plus filesystem and discovery fuzz
  targets for paths, policies, symlinks, permissions, and malformed data.
- Redacted custom-source and oversized numeric failures, plus deterministic
  cycle, depth, key-count, array-element, and cancellation guards for source
  trees and typed schemas.
- Complete precedence/merge permutation evidence and a structured-format
  conformance matrix with executable intentional-difference cases.
- `ErrSourceChanged`, context-aware filesystem/decode extension contracts, and
  generation checks for fail-closed reads under concurrent mutation.
- Diagnostic JSON/log canary coverage that seals arbitrary causes, plus
  Windows-compatible filtering of unrelated process-environment names.
- Exact case-sensitive discovery containment with filesystem-identity boundary
  checks, plus default rejection of Windows junction and mount-point reparse
  traversal.
- Required hardening traceability from every audit contract to executable
  evidence, including all 13,699 non-empty precedence subset permutations.
- A `configtest.DiffSecrets` assertion helper for value-aware diffs that never
  expose either secret operand.
- Explicit conformance assertions that decoding, defaults, environment
  loading, metadata, and snapshot cloning do not mutate private struct state.

[Unreleased]: https://github.com/faustbrian/golib/pkg/config/commits/main
