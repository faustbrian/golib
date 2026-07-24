# Goal: Civil Calendar And Business Date Foundation

## Objective

Build a production-grade open-source calendar package for immutable civil dates,
year-month values, calendar arithmetic, period boundaries, weekdays, IANA time
zones, holidays, and business-calendar calculations.

The package MUST provide the useful civil-calendar part of Carbon without a
mutable fluent API, natural-language parsing, global timezone, or overlap with
clock sources and temporal interval algebra.

## Product Principles

- A civil date is not an instant and MUST not carry an accidental timezone.
- Calendar arithmetic is distinct from fixed elapsed `time.Duration` arithmetic.
- Parsing is strict, explicit, locale-independent by default, and round-trippable.
- Time zones and daylight-saving transitions are explicit at conversion points.
- Month/year arithmetic defines clamping and overflow behavior precisely.
- Business calendars are immutable, versionable, and application-supplied.
- `clock` owns current time; `temporal` owns interval and set algebra.

## Civil Date Model

- Immutable `Date` with validated proleptic Gregorian year, month, and day.
- Explicit supported year range and zero-value policy.
- Construction from components, strict ISO 8601 date text, and `time.Time` in an
  explicit location.
- Comparison, equality, day-of-week, day-of-year, ISO week/year, leap-year, and
  days-in-month operations.
- Add/subtract days, weeks, months, quarters, semesters, and years.
- Month/year arithmetic supports named policies such as clamp, reject, and
  overflow; no silent policy switching.
- Difference in calendar days and component differences with documented signs.
- Start/end helpers return civil values, not fabricated instants.

## Year, Month, Quarter, And Week Values

- Typed `Year`, `YearMonth`, `Quarter`, `Semester`, and `ISOWeek` where they
  improve invalid-state prevention.
- Navigation, comparison, containment, first/last date, and length.
- Strict parsing and canonical formatting.
- ISO week-year boundaries and week 53 validated against authoritative vectors.
- Fiscal calendars are optional explicit policies, never inferred globally.

## Local Date And Time Zone Conversion

- Compose a civil `Date` with standard-library local time-of-day values or the
  `temporal/timeofday` package through an optional adapter.
- Convert local date/time to `time.Time` only with explicit `*time.Location` and
  an ambiguity/nonexistence policy.
- Detect DST gaps and folds rather than silently selecting an instant.
- Policies include reject, earlier/later occurrence, and explicit offset match.
- Convert instants to civil values in a specified location.
- Support embedded `time/tzdata` through documentation or an optional package,
  while respecting operating-system timezone updates.
- IANA zone identity and aliases MAY integrate with `international` but the
  standard library remains authoritative for transition calculation.

## Calendar Boundaries

- Start/end dates for ISO week, month, quarter, semester, year, and ISO year.
- Configurable week start only through an explicit immutable calendar policy.
- Boundary-to-instant conversion requires timezone and DST policy.
- End-of-day MUST not be represented as an invented `23:59:59.999...` civil
  value; use next-day exclusive boundaries for instant ranges.
- Rich bounded interval relationships and set operations remain in
  `temporal`.

## Business Calendars

- Immutable calendar with weekend definition, named holidays, partial metadata,
  and revision/version identity.
- Business-day predicate, next/previous business day, add/subtract business
  days, and count business days.
- Region/provider holiday datasets are optional adapters with provenance,
  licensing, effective version, and deterministic generation.
- Observed-holiday policies are explicit and do not overwrite source dates.
- No assumption that one country has one universal business calendar.
- Cutoff times, opening hours, settlement rules, and carrier schedules remain
  domain concerns unless modeled as separate proven packages.

## Parsing And Formatting

- Strict ISO 8601 date, year-month, ordinal-date, and ISO-week formats where
  implemented.
- Explicit custom layouts MAY mirror standard-library layouts without accepting
  ambiguous natural language.
- Canonical JSON/text encoding is versioned and stable.
- Locale-aware display formatting is optional and separate from wire formats.
- Invalid UTF-8, trailing input, duplicate components, impossible dates, and
  unsupported years fail predictably.

## Persistence And Integration

- `database/sql` scanner/valuer and native `pgx` codecs for PostgreSQL `date`.
- Explicit handling of PostgreSQL infinity only if represented as a distinct
  type; never coerce it into an ordinary date.
- `wire`, `validation`, and `config` adapters.
- `clock` helper to obtain today's date in an explicit location.
- `temporal` adapters for date periods and sequences without circular core
  dependencies.
- `scheduler` MAY consume calendar policies for due-date computation while
  retaining scheduling ownership.

## Security And Resource Bounds

- Bound parse bytes, year ranges, business-day search, holiday count, generated
  data, formatting output, and timezone conversion work.
- Business-day iteration MUST have explicit search limits and no unbounded loop.
- Holiday names and metadata never become unbounded telemetry labels.
- Threat-model timezone database drift, malicious holiday data, DST ambiguity,
  date overflow, Unicode confusion, and persisted-value reinterpretation.
- Caller-owned maps/slices and calendars MUST not mutate unexpectedly.

## Non-Goals

- No current-time source, timer, ticker, scheduler, cron, interval/set algebra,
  Carbon compatibility facade, mutable date object, natural-language parser,
  `diffForHumans`, translation catalog, opening-hours engine, or global locale.
- No assumption that adding one day equals adding 24 elapsed hours.
- No bundled global holiday database without authoritative provenance and a
  demonstrated maintenance plan.
- No temporal charting.

## Package Shape

- Root: `Date`, typed calendar units, arithmetic policies, parsing, errors.
- `timezone`: DST-safe local/instant conversion policies.
- `business`: immutable business calendars and calculations.
- `postgres`: SQL/pgx codecs and explicit infinity support if implemented.
- `calendarwire`, `calendarvalidation`, and `calendarconfig`: adapters.
- `calendartest`: fixtures, clocks, timezone vectors, and assertions.
- `internal/generate`: deterministic optional dataset tooling.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Required evidence:

- exhaustive Gregorian leap, month-length, boundary, ISO-week, and arithmetic
  properties across the supported year range
- differential tests against standard-library date construction and independent
  authoritative calendars where semantics overlap
- month clamp/reject/overflow and negative arithmetic mutation tests
- timezone gap/fold, historical transition, tzdata drift, and conversion vectors
- business-day/holiday property, revision, observed-date, and search-bound tests
- malformed, Unicode, overflow, timezone, and dataset fuzzing
- race tests for immutable calendars and concurrent timezone/holiday use
- PostgreSQL integration and round-trip suites
- benchmarks for parse, arithmetic, timezone conversion, and large calendars

## Documentation Deliverables

- Five-minute civil date, calendar arithmetic, timezone, and business-day guides.
- Complete Date, typed unit, arithmetic policy, DST, business, encoding, and
  persistence API reference.
- Carbon migration guide explaining changed semantics and unsupported magic.
- Guides for exclusive day ranges, PostgreSQL, JSON, config, validation,
  holiday datasets, versioning, and operations.
- Security, performance, FAQ, troubleshooting, examples, compatibility,
  contribution guide, and maintained changelog.
- Every exported API and user-facing scenario MUST be documented.

## Automation And Release

Use the latest stable Go release as the minimum at implementation time. Pin all
tools and dependencies. GitHub Actions MUST run formatting, vet, Staticcheck,
strict golangci-lint, advisory NilAway, tests, meaningful 100% coverage, race,
fuzz smoke, mutation checks, timezone/platform matrices, PostgreSQL integration,
dataset provenance/drift, vulnerability scans, benchmarks, docs, API
compatibility, and releases. Every blocking command MUST be reproducible locally
through documented `make` targets.

## Execution Plan

1. Specify Date, typed calendar units, arithmetic policies, errors, and bounds.
2. Implement strict parsing, formatting, navigation, and ISO calendar behavior.
3. Implement DST-safe timezone conversion and PostgreSQL codecs.
4. Implement immutable business calendars and optional dataset governance.
5. Integrate clock, temporal, wire, validation, config, and scheduler boundaries.
6. Complete mutation, fuzz, timezone, persistence, and performance hardening.
7. Publish complete adoption documentation and release v1.

## Acceptance Criteria

- Civil dates cannot be confused with instants or elapsed durations.
- Calendar arithmetic and DST ambiguity are explicit and deterministic.
- Business calculations are immutable, versioned, bounded, and provenance-aware.
- Rich interval algebra remains outside core and composes through `temporal`.
- Meaningful 100% coverage and every required GitHub Actions gate pass.
