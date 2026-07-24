# Hardening Goal: Civil Calendar And Business Dates

## Objective

Prove that `calendar` remains correct, deterministic, bounded, immutable, and
compatible across leap years, ISO week boundaries, month arithmetic, DST gaps
and folds, timezone database changes, hostile holiday data, persistence, and
concurrent use.

## Required Audits

### Gregorian And Arithmetic Audit

- Exhaust supported years for leap rules, month lengths, day/year numbers,
  weekdays, ISO week/year, quarter, semester, and boundary behavior.
- Property-test add/subtract inverse behavior where the selected month policy
  mathematically permits it.
- Mutation-test clamp, reject, overflow, negative movement, and range checks.
- Verify zero, minimum, maximum, and overflow states fail predictably.

### Timezone And DST Audit

- Test gaps, folds, unusual offsets, date-line changes, historical transitions,
  aliases, missing zones, tzdata updates, and explicit ambiguity policies.
- Prove end-of-day range examples use next-day exclusive instants.
- Differential-test conversions against the supported standard library.
- Document persisted local values and behavior under timezone database change.

### Business Calendar Audit

- Exercise arbitrary weekends, overlapping holidays, observed policies,
  revisions, empty/full calendars, long closures, and search limits.
- Prove input holiday collections and returned calendars are immutable.
- Verify dataset provenance, checksums, deterministic generation, and classified
  compatibility diffs where datasets ship.
- Mutation-test business-day admission and counting branches.

### Parsing, Persistence, And Resource Audit

- Fuzz ISO/custom input, invalid UTF-8, impossible values, trailing data, huge
  years, timezone identifiers, holiday metadata, and encoded forms.
- Test JSON/text/SQL/pgx/config/wire/validation round trips and PostgreSQL
  infinity policy.
- Enforce parser, year, holiday, search, output, and allocation budgets.
- Race-test shared calendars, locations, codecs, and generated metadata.

## Required Deliverables

- Gregorian/ISO truth tables and arithmetic policy matrix.
- Timezone transition, ambiguity, and tzdata compatibility corpus.
- Business-calendar provenance, revision, and resource-budget report.
- Mutation, fuzz, race, PostgreSQL, and benchmark evidence.
- Updated API, Carbon migration, timezone, business, security, and FAQ docs.

## Release Blockers

- Any invalid date acceptance, wrong ISO week, silent arithmetic policy change,
  DST misresolution, infinite business-day loop, data mutation, persistence
  corruption, race, panic, or unbounded behavior.
- Any holiday dataset lacking authoritative provenance or deterministic updates.
- Any use of fabricated end-of-day instants as a correctness boundary.
- Missing meaningful 100% coverage, mutation evidence, or green blocking CI.

## Completion Criteria

- Gregorian, arithmetic, timezone, business, encoding, and persistence suites pass.
- Calendar and dataset compatibility behavior is explicit and versioned.
- Race, fuzz, mutation, vulnerability, and performance gates pass.
- NilAway runs visibly as advisory without blocking findings.
- No release blocker remains and the changelog is current.
