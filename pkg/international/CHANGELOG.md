# Changelog

All notable changes and dataset updates are recorded here.

## [Unreleased]

- Use deterministic execution counts for default fuzz smoke campaigns while
  allowing explicit duration overrides for extended fuzzing.
- Normalize standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Bound and sanitize public parse-error kinds so hostile diagnostics cannot
  panic or echo overlong caller-controlled labels.
- Isolate Make-based release gates from ignored local Go workspaces so local
  verification uses the dependency versions pinned in `go.mod`, matching CI.
- Replace the unpublished validation revision with its available successor
  and record its module checksums for reproducible clean-checkout builds.
- Require 100% mutation coverage and efficacy across all country, subdivision,
  language, locale, currency, phone, and postal acceptance and canonicalization
  implementations, in addition to selected semantic mutants.
- Keep provenance and documentation gates portable on clean CI runners without
  requiring ripgrep outside the declared Go toolchain.

## [1.0.0] - 2026-07-16

- Establish typed country, subdivision, language, locale, currency, phone, and
  postal primitives.
- Pin CLDR 48.2, IANA 2026-06-14, ISO 4217 2026-01-01, and libphonenumber
  v9.0.32-compatible metadata.
- Add strict text, JSON, SQL, pgx, config, wire, and validation
  integration.
- Add deterministic generation, provenance, exact coverage, race, fuzz,
  benchmark, and privacy hardening gates.
- Preserve authoritative mappings for reused country and currency numeric
  identifiers and reject ambiguous historical policy expansions.
- Add explicit historic text, JSON, and SQL decoding without weakening strict
  default codecs.
- Add source-labelled independent fixture vectors and differential checks for
  country, locale, currency, phone, and package-policy postal behavior.
- Add explicit Unicode and resource-budget evidence, concurrent metadata tests,
  and a clean advisory NilAway gate.
