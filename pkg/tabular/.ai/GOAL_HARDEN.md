# Goal Harden: `tabular`

## Purpose

This document tightens the execution bar for `tabular` beyond feature scope.
Its purpose is to prevent a superficially complete package from shipping with
weak ingestion guarantees, poor hostile-input behavior, or misleading
performance claims.

`.ai/GOAL.md` defines what the package should do.
`.ai/GOAL_HARDEN.md` defines how rigorously it must prove that it does it.

## Hardening Priorities

### 1. Real-World File Hostility

The package must assume real files are messy.

Hardening must explicitly cover:

- empty files
- missing headers
- duplicate headers
- malformed rows
- inconsistent row lengths
- trailing delimiters
- embedded delimiters
- quoted newlines
- invalid spreadsheet cells
- broken archives
- missing extracted targets
- invalid byte offsets in fixed-width layouts

### 2. Encoding And Locale Damage

The package must assume files arrive with awkward encodings and locale-specific
behavior.

Hardening must explicitly cover:

- UTF-8
- ISO-8859-1
- Windows-1252 if relevant
- Nordic character preservation
- conversion failures
- whitespace normalization
- delimiter differences tied to locale conventions

### 3. Memory And Streaming Discipline

The package must not accidentally become a memory bomb on large files.

Hardening must explicitly cover:

- row streaming behavior
- chunking behavior
- large-file benchmarks
- archive extraction overhead
- spreadsheet ingestion memory behavior
- fixed-width parsing with large input files

### 4. Deterministic Errors

Hardening must ensure errors are:

- typed where appropriate
- stable enough for callers to handle intentionally
- clear enough for debugging
- not silently swallowed or flattened into generic failures

### 5. Behavior-Proving Coverage

Meaningful `100%` coverage is not enough unless it proves the package’s real
contract.

Hardening must verify that coverage actually proves:

- parser semantics
- row-shape semantics
- normalization behavior
- encoding behavior
- archive behavior
- fixed-width extraction behavior
- error semantics

### 6. Fixture Quality

Fixtures must not be trivial toy files only.

The fixture set must include:

- realistic CSV samples
- semicolon-delimited CSV
- realistic spreadsheet samples
- fixed-width files with encoding-sensitive characters
- ZIP-backed import examples
- malformed samples for each major format

## Mandatory Verification Surfaces

Before the package is treated as release-ready, it must have:

- fixture-backed parser tests
- regression tests for every discovered ingest bug
- fuzz targets for relevant parsing surfaces
- benchmark coverage for representative file sizes
- coverage reporting that proves meaningful `100%` production-code coverage
- documentation examples validated in CI where practical

## CI Hardening Requirements

GitHub Actions must enforce:

- test execution
- coverage reporting
- formatting checks
- linting
- static analysis
- fuzz-target strategy
- benchmark strategy
- docs validation strategy
- dependency and security scanning

The package should not rely on informal local discipline for these checks.

## Release Blockers

A release must be blocked if any of these are true:

- meaningful `100%` production-code coverage is not maintained
- a supported format lacks realistic fixtures
- encoding behavior is under-specified
- parser errors are not stable or understandable
- benchmarks do not exist for the major ingestion paths
- docs do not explain practical adoption and limitations clearly

## Performance Claims

Hardening must prevent fake performance claims.

The package must not claim streaming or large-file safety without:

- representative benchmarks
- memory-aware tests where appropriate
- explicit documentation of known limits

## API Stability Discipline

Hardening must also protect users from churn.

That means:

- parser configuration changes must be treated as compatibility-sensitive
- row-shape output changes must be treated as compatibility-sensitive
- normalization changes must be treated as compatibility-sensitive
- breaking ingest semantics must always be reflected in `CHANGELOG.md`

## Final Hardening Standard

`tabular` is only ready for serious OSS adoption when it proves all of the
following:

- supported formats are clearly scoped
- ingest behavior is fixture-proven
- hostile inputs are handled deliberately
- meaningful `100%` production-code coverage is enforced

## Required Deliverables

- Findings report, hostile-file corpus, regressions, benchmark baselines, and
  updated API, adoption, security, compatibility, and `CHANGELOG.md` docs.

## Completion Criteria

- Every required format and hostile-input behavior has deterministic evidence.
- All release blockers are closed and every GitHub Actions gate passes.
- CI and release gates are in place
- docs are strong enough for direct adoption without source spelunking
