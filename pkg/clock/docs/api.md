# API and ownership

## Root capabilities

`Clock.Now` returns wall time. `ElapsedClock.Since` follows `time.Since` style
semantics when a timestamp has a monotonic reading. `ElapsedClock.Measure`
captures the implementation's monotonic source and returns an elapsed closure;
it is the correct choice across manual wall jumps.

`Sleeper.Sleep` returns `nil` after the duration or `ctx.Err()` after
cancellation. A non-positive duration completes immediately after checking the
context.

Factories return owned resources and an error. Callers retain ownership until
the resource fires or is stopped. A failed factory returns no resource.

## Timer

`C` exposes the event channel. `Stop` returns true only for the active-to-stopped
transition. `Reset` reports whether the timer was active before rescheduling.
Reset removes any previously buffered manual value. Zero and negative manual
timer durations become due at the current manual instant.

## Ticker

Ticker durations must be positive. `Stop` is idempotent. `Reset` schedules the
next tick from the current instant. Manual ticker channels hold one value; later
ticks are dropped until that value is received.

## Callback

`Stop` prevents a callback that has not started. `Reset` reports prior active
state. System callbacks use standard-library goroutines and panic behavior.
Manual callbacks are coordinator-owned while running; their panics are counted
without retaining values.

## Advancement and errors

`Advance` returns a `Waiter`. `Wait` synchronizes all callback work attributed
to that request and supports context cancellation. Nested requests receive
results relative to their own submission point. Outstanding requests are
bounded by `Limits.MaxActive`; excess requests return `ErrActiveLimit` without
allocating a waiter.

Inside a running callback, registering a nested advancement does not by itself
move beyond the callback's current instant. Calling `Wait` on its waiter grants
the coordinator permission to move through that request's target. A canceled
wait does not cancel the underlying advancement request.

`AdvanceTo` rejects backward targets and targets farther away than one
`time.Duration`; it never reports success at a saturated intermediate time.

Errors are stable sentinel values for `errors.Is`: root validation uses
`ErrInvalidDuration`, `ErrInvalidCallback`, and `ErrOverflow`; manual lifecycle
adds invalid-limit, active-limit, work-limit, backward-advance, closed, and
observation validation errors.
