# Hardening Goal: Localized Value Foundation

## Objective

Prove that `localized` is standards-aligned, immutable, deterministic,
bounded, privacy-safe, interoperable, and panic-free across locale
canonicalization, exact lookup, language matching, fallback, merging, hostile
Unicode, encoding, persistence, and concurrent use.

## Required Audits

### Locale And Canonicalization Audit

- Exhaust language, script, region, variant, extension, private-use,
  grandfathered, deprecated, `und`, `mul`, malformed, and unknown-tag cases.
- Verify case-insensitive identity and canonical output against the supported
  `international` registry revision.
- Prove duplicate detection happens after canonicalization.
- Record standards versions, registry provenance, update policy, and drift.

### Lookup, Matching, And Fallback Audit

- Exhaust exact, parent, script-sensitive, region-sensitive, weighted,
  wildcard, default, missing, and present-empty cases.
- Prove exact lookup never falls back and matching never mutates stored values.
- Test tie-breaking and preference order independently of map iteration.
- Attack cyclic, duplicate, invalid, and oversized fallback plans and prove
  bounded termination.

### Merge, Ownership, And Determinism Audit

- Exhaust left-wins, right-wins, reject, resolver, absent, and empty conflicts.
- Property-test canonicalization, merge stability, idempotence, deterministic
  iteration, equality, and round trips.
- Prove constructor inputs, outputs, iterators, generic values, and codecs cannot
  alias mutable caller state contrary to contract.
- Mutation-test every presence, conflict, fallback, and tie-breaking branch.

### Encoding And Persistence Audit

- Fuzz JSON, text, HTTP preference input, SQL, pgx, and wire adapters with
  invalid UTF-8, duplicate tags, huge strings, deep input, trailing data,
  invalid weights, and unsupported representations.
- Prove accepted values round-trip without locale, empty/missing, ordering, or
  text drift.
- Validate Track, Postal, Location, and Spatie Translatable compatibility
  fixtures and classify every intentional divergence.
- Verify errors and diagnostics do not disclose localized content by default.

### Concurrency, Security, And Resource Audit

- Race/stress concurrent lookup, iteration, matching, merging, encoding, and
  observation hooks.
- Enforce locale, tag, text, fallback, candidate, merge, parser, diagnostic, and
  allocation budgets under adversarial input.
- Threat-test Unicode confusables, private-use tags, locale enumeration,
  fallback amplification, and telemetry cardinality.
- Prove no hidden goroutine, mutable global locale, unbounded cache, unsafe,
  cgo, `go:linkname`, callback deadlock, or content leak remains.

## Required Deliverables

- Locale canonicalization and lookup/matching/fallback behavior matrices.
- Merge, missing/empty, encoding, and legacy compatibility matrices.
- Fuzz, mutation, race, aliasing, PostgreSQL, privacy, and benchmark evidence.
- Enforced resource budgets and dependency/registry provenance report.
- Updated API, adoption, migration, security, performance, FAQ, and
  troubleshooting documentation.

## Release Blockers

- Any incorrect locale identity, unstable resolution, implicit fallback,
  confused empty/missing state, lost value, mutable alias, nondeterministic
  encoding, content disclosure, race, panic, or unbounded work.
- Any translation-framework concern leaking into the focused value package.
- Any unclassified legacy or standards divergence where compatibility is
  claimed.
- Missing meaningful 100% coverage, mutation evidence, or green blocking CI.

## Completion Criteria

- Locale, lookup, matching, fallback, merge, encoding, persistence, and legacy
  suites pass.
- All operations survive enforced limits and adversarial Unicode/input stress.
- Race, fuzz, mutation, vulnerability, compatibility, and performance gates
  pass.
- NilAway runs visibly as advisory without blocking findings.
- No release blocker remains and the changelog is current.
