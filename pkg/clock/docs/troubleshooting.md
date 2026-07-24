# Troubleshooting

## `ErrActiveLimit`

Stop or let owned one-shot resources fire, call `Shutdown`, or construct the
manual clock with an appropriate explicit limit. Repeated reset of an already
active object does not consume another active slot.

## `ErrWorkLimit`

One advance encountered more due work than permitted, often because callbacks
or very small tickers recursively register same-instant work. Inspect the
schedule, lower fan-out, or raise the budget only after bounding the workload.

## `ErrOverflow`

The elapsed target, wall offset, event deadline, registration sequence, or
`AdvanceTo` distance would leave its representable range. Split realistic
advances; huge durations are primarily boundary-test inputs.

## A waiter does not complete

Check for a callback that never returns or waits on future manual time without
a nested advance. Use a context deadline while diagnosing ownership. The clock
does not guess goroutine quiescence.

## A stopped timer still had a value

Receive values intentionally before reuse. Manual `Reset` drains its prior
buffer. System behavior follows Go 1.26 timer channel guarantees.

## `synctest` panics about bubble ownership

Create channels, timers, tickers, and goroutines inside the same bubble. Do not
operate on bubbled resources from an external goroutine.
