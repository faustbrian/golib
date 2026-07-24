# Quickstarts

## Instant periods

Construct operational ranges with `instant.Range`; it is always closed-open.
Use `instant.New(start, end, bounds)` when endpoint membership is domain data.

```go
p, _ := instant.Range(start, end)
q, _ := instant.After(end, 15*time.Minute, temporal.ClosedOpen)
set, _ := instant.NewSet(temporal.Limits{}, p, q)

dayStart, _ := instant.Snap(
    start, instant.Day, instant.Floor, location, calendartz.Reject,
)
```

Normalization merges adjacency only when the shared endpoint is represented by
at least one input. `Period.Equal` is structural; `Period.SetEqual` compares
represented membership. Serialized `time.Time` values cannot retain monotonic
readings; constructors strip them deliberately.

## Civil-date periods

```go
week, _ := dateperiod.ISOWeek(2026, 29)
next, _ := week.MoveMonths(1, calendar.Clamp)
chunks, _ := next.SplitDays(2, temporal.Limits{Steps: 10})
instantWeek, _ := week.ToInstant(location, calendartz.Reject)
```

Months and years are calendar units, never `time.Duration`. Converting to
instants requires an explicit location and DST resolution policy.

## Local times and daily intervals

```go
open, _ := timeofday.Parse("22:00", temporal.Limits{})
close, _ := timeofday.Parse("02:00", temporal.Limits{})
shift, _ := timeofday.Between(open, close, temporal.ClosedOpen)
parts, _ := shift.Split(90*time.Minute, temporal.Limits{Steps: 4})
opensAt, _ := open.Apply(date, location, calendartz.Reject)
instantShift, _ := shift.ToInstant(date, location, calendartz.Reject)
```

Circular intervals cross midnight. Equal endpoints are rejected by `Between`;
use `Collapsed` or `FullDay`. `EndOfDay()` is the distinct `24:00` boundary.
Applying `24:00` advances to the next civil midnight rather than aliasing the
same date's `00:00`.
