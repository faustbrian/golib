# Goal: Opening Hours And Availability Foundation

## Objective

Build a production-grade open-source Go package for representing, normalizing,
querying, combining, and persisting recurring opening-hours schedules and dated
exceptions.

The package MUST provide precise generic availability semantics for stores,
service points, offices, pickup locations, support desks, and similar resources.
It MUST NOT absorb provider-specific scraping, free-form carrier parsing,
appointment booking, workforce planning, or application business policy.

## Product Principles

- Schedules are immutable values with deterministic canonical forms.
- Civil dates, local times, instants, and elapsed durations remain distinct.
- Time zones and daylight-saving transitions are always explicit.
- Weekly recurrence and dated exceptions are modeled separately.
- Multiple ranges per day, overnight ranges, full-day opening, and closure are
  first-class states rather than string conventions.
- Query results explain which schedule rule produced the answer.
- Caller input is copied or otherwise protected from aliasing and mutation.
- Every parser, expansion, merge, and search operation has explicit bounds.
- `calendar` owns civil dates and zones; `temporal` owns time-of-day
  intervals; `clock` owns current time.

## Schedule Model

- Immutable schedule with an explicit IANA timezone identity.
- Seven-day weekly template with zero or more local-time ranges per day.
- Explicit day states: inherited, open for ranges, open all day, and closed.
- Dated exceptions that can replace, add to, subtract from, or close a date.
- Multi-day and named exception sets with deterministic precedence.
- Optional metadata such as label, source, revision, and effective range with
  bounded values and no effect on interval semantics.
- Explicit effective start/end dates and behavior outside the effective range.
- Stable equality, comparison, canonicalization, hashing, and revision support.
- Zero-value behavior MUST be documented and MUST NOT imply always open.

## Daily And Overnight Ranges

- Ordinary ranges, multiple disjoint ranges, and ranges crossing midnight.
- Explicit start-inclusive/end-exclusive default with documented alternatives
  only where they can be represented without ambiguity.
- Full-day opening is distinct from an empty set and from `00:00-00:00`.
- Overnight ranges define which civil day owns the rule and how spillover is
  considered when a following date has an exception.
- Overlapping and adjacent ranges can be rejected or normalized through named
  policies; behavior MUST never change silently.
- Duplicate ranges, inverted ranges, precision loss, and day-boundary overflow
  fail predictably.
- Precision is explicit and compatible with `temporal/timeofday`.

## Exceptions And Precedence

- Exact-date exceptions support closure, replacement ranges, additions, and
  removals through explicit operation types.
- Date ranges and recurring holiday inputs MAY be supported through adapters,
  but MUST resolve to deterministic dated rules before evaluation.
- Conflicting exceptions have a documented stable precedence model.
- Duplicate source revisions and ambiguous equal-priority rules are rejected or
  resolved only through an explicit caller policy.
- Exceptions can represent public holidays, exceptional pickup windows,
  maintenance closures, and temporary extended hours without changing the
  weekly template.
- Exception provenance is preserved for diagnostics and query explanations.

## Normalization And Composition

- Canonical ordering of weekdays, exceptions, and ranges.
- Merge, union, intersection, subtraction, and overlay operations where their
  schedule semantics can be defined without guessing business intent.
- Composition distinguishes adding availability from overriding a schedule.
- Normalization is idempotent and produces stable serialized output.
- Operations return new values and never mutate inputs.
- Output cardinality, interval fragmentation, exception expansion, and metadata
  growth are bounded.
- Semantic equality is separate from source/provenance equality where needed.

## Availability Queries

- `IsOpen` at an instant with explicit schedule timezone evaluation.
- Availability for a local date/time only with explicit DST resolution policy.
- Effective ranges for a civil date and for a bounded instant interval.
- Next opening, next closing, next transition, previous transition, and open
  duration within a bounded search horizon.
- Explanations identify weekly rule, exception, timezone, and transition that
  produced the result without exposing sensitive metadata.
- Search exhaustion returns a typed result or error rather than looping forever.
- Queries around DST gaps, folds, timezone changes, midnight, and overnight
  spillover have fully specified behavior.
- Helpers using current time require an injected `clock` capability; core
  schedule queries never read the process clock implicitly.

## Parsing And Formatting

- Stable canonical JSON and text encodings for package-owned values.
- Human-readable formatting is separate from canonical wire encoding.
- Structured import builders for common weekday/range/exception representations.
- Optional adapters MAY support schema.org `OpeningHoursSpecification` and
  other documented interchange formats when lossless mappings are possible.
- Lossy input mappings require explicit policy and diagnostics.
- Provider-specific prose such as carrier-specific opening-hours strings remains
  in application adapters unless a separately specified parser is justified.
- Invalid UTF-8, duplicate keys, unknown fields under strict mode, trailing
  input, oversized documents, excessive precision, and impossible values fail
  deterministically.

## Persistence And Integration

- `database/sql` scanner/valuer and native `pgx` support for stable JSONB or a
  documented normalized representation.
- `wire`, `validation`, and `config` adapters without dependency cycles.
- `calendar` integration for civil dates, weekdays, business calendars, and
  explicit timezone conversion.
- `temporal/timeofday` integration for daily and circular interval algebra.
- `clock` integration for `Now`, transition waiting examples, and tests.
- `localized` MAY provide localized labels while remaining optional.
- Application adapters own carrier/provider payload interpretation and database
  migration from legacy Laravel/Spatie representations.

## Concurrency And Observability

- Immutable values are safe for concurrent reads without hidden locks.
- Optional compiled indexes or caches have explicit ownership and bounded size.
- No package-global registry, hidden goroutine, background refresh, or exporter.
- Optional observation hooks report bounded operation, outcome, range counts,
  search steps, and duration without schedule labels or customer data.
- Observation callbacks MUST NOT execute under internal locks or change results.

## Security And Resource Bounds

- Bound serialized bytes, ranges per day, exceptions, metadata, nesting,
  normalization fragments, search horizon, transitions, and output cardinality.
- Detect arithmetic overflow, impossible dates, invalid zones, and non-progress.
- Fuzz hostile parser input, schedule construction, normalization, composition,
  timezone transitions, and search operations.
- Avoid unsafe, cgo, `go:linkname`, reflection-heavy mutation, and global state.
- Errors are typed, bounded, deterministic, and safe to expose without dumping
  complete schedules or untrusted payloads.

## Non-Goals

- No provider-specific scraper, natural-language parser, appointment system,
  workforce rota, payroll calendar, queue scheduler, or cron engine.
- No ownership of civil-date arithmetic, holiday datasets, interval algebra,
  clock sources, timezone database, translation catalogs, or HTTP transport.
- No assumption that opening hours imply order eligibility, SLA availability,
  inventory availability, or authorization.
- No mutable fluent Carbon-style API and no process-global locale or timezone.

## Package Shape

- Root: schedules, weekly rules, exceptions, policies, errors, and queries.
- `compile`: optional immutable indexed representation for repeated queries.
- `encoding`: canonical JSON/text and structured import/export contracts.
- `postgres`: SQL/pgx codecs and persistence compatibility fixtures.
- `openinghourswire`, `openinghoursvalidation`, and `openinghoursconfig`: adapters.
- `openinghourstest`: builders, fixtures, timezone vectors, and assertions.
- `internal/generate`: deterministic fixtures or schemas only when required.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Executing lines
without proving schedule behavior does not satisfy this requirement.

Required evidence includes:

- exhaustive weekday, range, boundary, empty, full-day, and overnight matrices
- exception precedence, replacement, addition, subtraction, and closure tables
- algebraic properties for normalization, union, intersection, subtraction,
  idempotence, stable ordering, and immutability
- timezone transition tests across DST gaps, folds, offsets, midnight, and
  historical rule changes
- next/previous transition and bounded-search properties proving termination
- compatibility fixtures from Track, Postal, Location, and Spatie opening-hours
  representations where migration semantics are claimed
- parser, constructor, arithmetic, cardinality, and hostile-input fuzzing
- race and aliasing tests for shared immutable values and optional compiled data
- mutation tests for boundary, precedence, overlap, and transition decisions
- PostgreSQL integration and lossless round-trip tests
- benchmarks with allocations for construction, normalization, daily lookup,
  transition search, large exception sets, composition, and encoding

## Documentation Deliverables

- Five-minute weekly schedule, exception, overnight, timezone, and query guides.
- Complete API reference for every exported type, policy, error, and operation.
- Formal precedence, normalization, range-boundary, and DST behavior tables.
- Adoption guides for service points, storefronts, support hours, and legacy
  Laravel/Spatie migration.
- Cookbook examples for holidays, temporary closures, merged schedules,
  persistence, validation, JSON, PostgreSQL, testing, and observability.
- Security model, performance guide, FAQ, troubleshooting, compatibility,
  architecture, roadmap, contribution guide, and maintained changelog.
- Every user-facing scenario and public API MUST be documented sufficiently for
  adoption without reading implementation source.

## Automation And Release

Use the latest stable Go release as the minimum at implementation time. Pin all
tools and dependencies. GitHub Actions MUST run formatting, vet, Staticcheck,
strict golangci-lint, advisory NilAway, tests, meaningful 100% coverage, race,
fuzz smoke, mutation checks, timezone and PostgreSQL integration, vulnerability
scans, benchmarks, docs, API compatibility, and releases. Every blocking command
MUST be reproducible locally through documented `make` targets.

Repository setup MUST include README badges for every blocking workflow/job,
Dependabot, security policy, contribution guide, code of conduct, license,
notice and third-party attribution handling, release automation, changelog,
repository topics, and complete adoption documentation.

## Execution Plan

1. Specify schedule states, range ownership, exception precedence, limits, and
   canonical encodings.
2. Implement immutable schedules, normalization, exceptions, and daily queries.
3. Implement timezone-safe instant queries and bounded transition search.
4. Add persistence, wire, validation, calendar, temporal, and clock adapters.
5. Prove legacy compatibility, fuzz, race, mutation, and performance behavior.
6. Complete adoption documentation and release v1.

## Acceptance Criteria

- Weekly, overnight, full-day, closed, and exception semantics are unambiguous.
- DST and timezone behavior is explicit, tested, and never guessed silently.
- Every search and composition operation is deterministic and bounded.
- Values are immutable, concurrency-safe, and losslessly serializable where
  compatibility is claimed.
- Track, Postal, and Location representations have documented migration paths.
- Meaningful 100% coverage and every required GitHub Actions gate pass.
