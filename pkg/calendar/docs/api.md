# API reference

## Root package

- `Date`: immutable civil date; strict constructors, comparison, Gregorian and
  ISO queries, named add/subtract methods, component differences, and civil
  boundaries. Its zero value is invalid.
- `ArithmeticPolicy`: `Clamp`, `Reject`, and `Overflow` for month/year movement.
- `ComponentDifference`: signed years, months, and days produced by applying
  years first, months under the named policy, then exact days.
- `Year`, `YearMonth`, `Quarter`, `Semester`, `ISOWeek`: validated typed periods
  with canonical parsing, navigation, comparison, containment, boundaries, and
  lengths.
- `WeekPolicy`: immutable configurable first weekday. ISO helpers always use
  Monday independently of this policy.

## Timezone package

- `LoadLocation` bounds and validates IANA identifiers before standard-library
  loading.
- `LocalDateTime` composes `Date` and nanosecond-precision local time-of-day.
- `Resolve` detects gaps/folds and accepts only explicit resolution policy.
- `FromInstant` extracts local civil components in an explicit location.
- `DayRange` returns `[start,next-day-start)` instants.

## Business package

- `Holiday` deep-copies bounded metadata and retains observed source dates.
- `Calendar` deep-copies configuration, requires revision identity, fails
  closed when zero, and supports bounded business-day navigation/counting.
- `Observe` retains source holidays and appends separately marked observed
  entries under `NoObservance`, `NextWeekday`, or `NearestWeekday`.

## Persistence and adapters

- `postgres.Date` is finite and non-null. `postgres.InfinityDate` is the
  distinct finite/infinity sum type. Both implement SQL and pgx interfaces.
- `calendarclock.Today` composes with `clock.Clock` structurally.
- `calendartemporal` creates bounded sequences and exclusive instant endpoints.
- `calendarconfig.Date`, `calendarvalidation.Rule`, and `calendarwire` provide
  strict adapter seams.
- `calendartest` provides clocks, assertions, locations, and transition vectors.

The generated, compiler-derived public API snapshot is
[api/baseline.txt](../api/baseline.txt).
