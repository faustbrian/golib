# Goal: Temporal Period And Interval Algebra

## Objective

Build a serious open-source temporal algebra package for bounded instant and
date periods, boundary semantics, interval relations, sequences, local
time-of-day values, fixed durations, circular daily intervals, and normalized
interval sets.

The package MUST provide a Go-native successor to
`github.com/faustbrian/golib/pkg/temporal` while using explicit immutable values, typed
errors, generics only where they clarify safety, and standard Go integration.
It MUST preserve the valuable mathematical behavior without transliterating a
mutable or PHP-specific API.

## Product Principles

- Interval bounds and empty/singleton semantics are explicit.
- Relations and set operations follow documented mathematical definitions.
- Date periods, instant periods, local times, and elapsed durations are distinct.
- Operations are immutable, deterministic, normalized, and non-mutating.
- Parsing is strict and round-trippable; no natural-language interpretation.
- Algorithms are bounded and resistant to combinatorial expansion.
- `clock` owns time sources and timers; `calendar` owns civil arithmetic,
  timezones, holidays, and business calendars.

## Compatibility Source

Audit the complete current public behavior of `faustbrian/temporal`, including:

- `Period\Bounds`, `DatePoint`, `Duration`, `Period`, and `Sequence`;
- ISO 8601, ISO 80000, and Bourbaki interval notation;
- period constructors for ranges, points, years, ISO years, semesters, quarters,
  months, ISO weeks, and days;
- period relations, duration comparison, iteration, splitting, difference, gap,
  subtraction, intersection, union, merge, movement, expansion, and snapping;
- `Time\Time`, `Duration`, `Interval`, `IntervalSet`, bounds, units, rounding,
  parsing, formatting, circular intervals, complements, splitting, stepping,
  relation checks, and normalized set operations.

Create a behavior-by-behavior compatibility matrix before implementation. A Go
API MAY differ where Go idioms, stronger typing, immutability, overflow safety,
or ambiguity require it, but every divergence MUST be deliberate, documented,
and covered by migration guidance.

## Bounds And Relation Model

- Four bound modes: closed, open, start-closed/end-open, and
  start-open/end-closed.
- Bound replacement and inclusion/exclusion helpers return new values.
- Empty, invalid, reversed, unbounded, and singleton intervals have explicit
  representation or rejection rules.
- Allen interval relations are named, exhaustive, mutually coherent, and backed
  by formal truth tables.
- Convenience predicates such as before, after, meets, overlaps, contains,
  starts, finishes, during, equals, abuts, and borders map to the formal model.
- Bound-sensitive equality and set equality remain distinguishable where needed.

## Instant Periods

- Immutable periods over `time.Time` with explicit location/monotonic policy.
- Canonical default is start-inclusive/end-exclusive for operational ranges.
- Constructors from endpoints, point plus duration, before/after/around,
  timestamps, and strict notation.
- Elapsed duration uses instant arithmetic and does not pretend to be calendar
  duration.
- Move, expand, change start/end, intersect, union, subtract, difference, and gap.
- Split and iterate forward/backward by positive fixed durations with strict
  count and progress bounds.
- Snapping to civil boundaries requires an explicit `calendar` adapter,
  location, and DST policy.
- Serialized instants cannot preserve process-local monotonic readings; this is
  explicit in equality and round-trip documentation.

## Date Periods

- Optional integration using `calendar.Date` for bounded civil date ranges.
- Constructors for day, ISO week, month, quarter, semester, year, and ISO year.
- Calendar movement and splitting delegate to explicit calendar arithmetic
  policies rather than converting months to fixed durations.
- Date and instant period APIs remain distinct when their semantics differ.
- End-of-day instant conversion uses next-boundary exclusive behavior.

## Sequences And Period Sets

- Immutable normalized collections of periods.
- Stable ordering and deterministic duplicate policy.
- Length/span, total covered duration, gaps, intersections, unions, subtraction,
  containment, search, transform, and reduction.
- Set operations produce normalized disjoint output with explicit bound merging.
- Algorithms MUST define complexity and enforce period/output cardinality limits.
- No mutation-oriented collection API copied from PHP; operations return new
  values or standard iterators.

## Local Time Of Day

- Immutable date-independent time-of-day with explicit precision.
- Construction from components, strict ISO text, offset since midnight, midnight,
  noon, and end boundary representation.
- Comparison, clamp, shift with wrapping policy, rounding, difference, and
  circular distance.
- Applying a time-of-day to a civil date requires explicit location and DST
  resolution through `calendar`.
- `24:00` support, if provided, is a distinct end-boundary representation and
  MUST not silently equal next-day `00:00` in all contexts.

## Fixed Duration

- Use `time.Duration` directly when its range and nanosecond precision satisfy
  the operation.
- A package duration type MAY exist for checked arithmetic, richer formatting,
  wider range, or explicit precision only if justified and interoperable.
- Fixed days/weeks are elapsed multiples; calendar days/months/years are not
  fixed durations.
- Sum, negate, absolute value, compare, clamp, multiply, divide, round, overflow,
  and division remainder semantics are explicit.
- ISO duration support MUST distinguish fixed elapsed components from calendar
  components that require a reference date.

## Daily Intervals And Interval Sets

- Intervals between time-of-day values, including ordinary, collapsed,
  full-day, and circular intervals crossing midnight.
- Explicit bound behavior at midnight and day-end.
- Includes, contains, overlaps, abuts, intersection, gap, union, difference,
  complement, shift, expand, split, and steps.
- Immutable normalized interval sets with stable order and no accidental overlap.
- Complement is defined against an explicit full-day universe.
- Circular operations MUST be validated independently around midnight.

## Parsing And Encoding

- Strict ISO 8601 interval forms applicable to supported value types.
- ISO 80000 and Bourbaki notation with exact bracket semantics.
- Stable text and JSON encodings with versioning policy.
- `database/sql` and `pgx` codecs where PostgreSQL range/multirange semantics can
  be mapped without loss; lossy cases MUST be rejected or explicit.
- `wire`, `validation`, and `config` adapters.
- Unknown formats, invalid UTF-8, trailing data, duplicate components, excessive
  precision, unsupported calendar components, and overflow fail predictably.

## Temporal Charting Compatibility Gap

The PHP package also contains Gantt/terminal charting under `Period\Chart`.
Charting is explicitly deferred from the initial Go implementation.

- Do not include `temporalchart` or chart rendering in v1 core scope.
- Record every PHP chart type, configuration option, label/color behavior,
  terminal capability, output contract, and fixture in the compatibility matrix.
- Mark charting as a known unsupported compatibility gap in README, migration,
  roadmap, and release notes.
- Core data structures MUST remain sufficient for a later optional
  `temporalchart` package without embedding rendering concerns today.
- Do not mark full PHP-package compatibility complete while charting remains
  unsupported.

## Security And Resource Bounds

- Bound parse bytes, precision, range span, split steps, iteration, sequence
  input/output, set-operation expansion, nesting, errors, and formatting output.
- Every iterator proves forward progress and rejects zero/negative steps.
- Arithmetic detects overflow before mutation or allocation.
- Threat-model parser ambiguity, interval explosion, Unicode confusion,
  PostgreSQL bound mismatch, DST misuse, and denial of service.
- Caller-owned inputs and returned values remain immutable.

## Non-Goals

- No clock/timer source, scheduler, cron, timezone database, holiday/business
  calendar, natural-language parser, `diffForHumans`, mutable fluent API, ORM,
  workflow engine, distributed clock, or chart rendering in initial scope.
- No generic ordered-value abstraction that sacrifices type safety or requires
  arbitrary runtime comparison callbacks throughout the public API.
- No silent conversion between calendar and fixed elapsed durations.
- No claim of full PHP compatibility until the deferred chart gap is resolved.

## Package Shape

- Root: bounds, relations, errors, common limits, and notation contracts.
- `instant`: `time.Time` periods, relations, operations, and sequences.
- `dateperiod`: optional `calendar.Date` periods and sequences.
- `timeofday`: local times, fixed durations, intervals, and interval sets.
- `notation`: ISO 8601, ISO 80000, and Bourbaki codecs.
- `postgres`: range/multirange SQL and pgx integration.
- `temporalwire`, `temporalvalidation`, and `temporalconfig`: adapters.
- `temporaltest`: relation tables, fixtures, generators, and assertions.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Tests MUST prove
mathematical semantics and detect realistic defects.

Required evidence includes:

- exhaustive relation truth tables across all bound combinations
- algebraic properties for intersection, union, difference, complement,
  normalization, idempotence, commutativity where applicable, and conservation
- differential compatibility fixtures generated from the PHP package
- notation round-trip and independent standard vectors
- circular midnight, full-day, collapsed, and boundary property tests
- date/instant/fixed-duration distinction and DST integration tests
- malformed notation, overflow, split, iteration, set-explosion, and precision
  fuzzing
- mutation testing of bound, relation, normalization, and arithmetic decisions
- race tests for immutable shared values, codecs, and adapters
- PostgreSQL range/multirange integration and losslessness matrix
- benchmarks for relations, normalization, large sets, splitting, parsing, and
  allocation/resource limits

## Documentation Deliverables

- Five-minute instant period, date period, and time-of-day interval quickstarts.
- Complete bounds, relations, operations, sequence, duration, notation,
  encoding, persistence, and error API reference.
- Formal relation and set-operation tables with diagrams and examples.
- Migration guide from `faustbrian/temporal` covering every supported behavior,
  deliberate API divergence, and the deferred charting gap.
- Guides for exclusive ranges, circular intervals, PostgreSQL, calendar/clock
  integration, overflow, performance, and hostile input.
- Security, FAQ, troubleshooting, compatibility, roadmap, examples,
  contribution guide, and maintained changelog.
- Every exported API and user-facing scenario MUST be documented.

## Automation And Release

Use the latest stable Go release as the minimum at implementation time. Pin all
tools and dependencies. GitHub Actions MUST run formatting, vet, Staticcheck,
strict golangci-lint, advisory NilAway, tests, meaningful 100% coverage, race,
fuzz smoke, mutation checks, PHP compatibility fixtures, PostgreSQL integration,
vulnerability scans, benchmarks, docs, API compatibility, and releases. Every
blocking command MUST be reproducible locally through documented `make` targets.

## Execution Plan

1. Capture the PHP behavior matrix and specify bounds, relations, limits, errors,
   and compatibility policy.
2. Implement instant periods, relation algebra, immutable sequences, and notation.
3. Implement time-of-day, duration, circular intervals, and normalized sets.
4. Implement optional date-period/calendar and PostgreSQL integrations.
5. Complete differential, property, mutation, fuzz, and performance hardening.
6. Document the deferred temporal charting gap and future extension seam.
7. Publish complete adoption documentation and release v1.

## Acceptance Criteria

- Bound and relation behavior is formally defined and exhaustively proven.
- Set operations are immutable, normalized, deterministic, and bounded.
- Date, instant, time-of-day, and elapsed-duration semantics cannot be confused.
- Supported PHP temporal behavior has differential compatibility evidence.
- Temporal charting is visibly documented as deferred and not silently omitted.
- Meaningful 100% coverage and every required GitHub Actions gate pass.
