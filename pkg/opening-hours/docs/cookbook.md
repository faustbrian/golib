# Cookbook

## Holiday closure

Create `ExceptionClose` with the holiday date, source, revision, and priority.
Group a year in `NewExceptionSet` or use bounded `ExpandExceptionRange`.

## Temporary extended hours

Use `ExceptionAdd` for an extra interval. Use `ExceptionReplace` when the whole
date is authoritative. These operations are intentionally different.

## Maintenance window

Use `ExceptionSubtract` with one or more ranges. Subtraction may split a weekly
range into multiple normalized fragments.

## Composition

Use union for “either resource,” intersection for “both constraints,” subtract
for blackouts, and overlay for authoritative right-hand configuration.

## JSON and PostgreSQL

Call `CanonicalJSON`, store the bytes in JSONB, and scan directly into
`Schedule`. Use `postgres.JSONB` only when SQL `NULL` is a domain state.

## Testing

Use `openinghourstest.Time`, `Range`, and `Weekly` for static fixtures. Prefer
table tests at start/end boundaries and include the resource timezone's next
gap and fold.

## Wait for the next transition

Keep waiting in application code and inject both wall-time and timer
capabilities from `clock`. The package itself never starts a goroutine or
reads the process clock:

```go
type transitionClock interface {
    clock.Clock
    clock.TimerFactory
}

func waitForTransition(
    ctx context.Context,
    source transitionClock,
    schedule openinghours.Schedule,
) (openinghours.Transition, error) {
    now := source.Now()
    transition, err := schedule.NextTransition(now, 30*24*time.Hour)
    if err != nil {
        return openinghours.Transition{}, err
    }
    timer, err := source.NewTimer(transition.Instant.Sub(now))
    if err != nil {
        return openinghours.Transition{}, err
    }
    defer timer.Stop()

    select {
    case <-ctx.Done():
        return openinghours.Transition{}, ctx.Err()
    case <-timer.C():
        return transition, nil
    }
}
```

Callers choose the horizon and decide whether `CodeSearchExhausted` means no
wait, a later bounded retry, or an application event. Recompute after wake-up
before acting because configuration and the installed timezone database may
have changed.

## Observability

Translate `Observation` into bounded metrics. Do not attach labels, revisions,
dates, timezones, or customer identifiers.
