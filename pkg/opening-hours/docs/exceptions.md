# Dated exceptions in five minutes

Exceptions target one exact civil date in the schedule timezone. Each has an
operation, integer priority, source, revision, and optional named set.

```go
holiday, _ := openinghours.NewException(openinghours.ExceptionConfig{
    Date: openinghours.MustDate(2026, time.December, 24),
    Operation: openinghours.ExceptionClose,
    Priority: 100,
    Source: "holiday-calendar",
    Revision: "2026",
})
```

Operations apply in ascending priority. `replace` discards inherited intervals,
`add` unions, `subtract` removes, and `close` clears the date. Equal priorities
are rejected by default. `ResolveCanonical` explicitly orders equal priorities
by bounded source, revision, and operation.

Named `ExceptionSet` values preserve provenance while evaluation uses their
exact-date rules. `ExpandExceptionRange` expands inclusive multi-day input only
up to `MaximumDates`.

A date exception sees the complete date, including spillover from the previous
day. Therefore a Tuesday closure suppresses a Monday `22:00-02:00` spill.
