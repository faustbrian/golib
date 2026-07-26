# Five-minute guide

## Civil dates

Construct from validated components or strict ASCII `YYYY-MM-DD` text:

```go
date, err := calendar.NewDate(2024, time.February, 29)
same, err := calendar.ParseDate("2024-02-29")
```

The supported range is `0001-01-01` through `9999-12-31`. `Date{}` is invalid;
check `IsValid` when a zero value can enter your API. Dates compare and expose
weekday, ordinal day, ISO week-year, leap-year, and boundary queries.

## Calendar arithmetic

Days and weeks are calendar units. Months, quarters, semesters, and years also
require `Clamp`, `Reject`, or `Overflow`. See [arithmetic](arithmetic.md).

## Timezones

Build a `timezone.LocalDateTime`, load a bounded IANA name, then call `Resolve`
with `Reject`, `Earlier`, `Later`, or `MatchOffset`. Gaps never produce an
instant. Folds never select an occurrence silently.

## Business days

Construct a `business.Calendar` with an application revision, weekend set, and
holidays. Every iterative method takes a positive search limit. Counts use a
half-open `[start,end)` date range and return a negative count when reversed.
