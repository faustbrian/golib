# State machines and ordering

## Manual clock

| State | Operation | Result | Next state |
| --- | --- | --- | --- |
| constructed | successful `New` | explicit wall origin and limits | open |
| open | `Now`, `Mark`, `SinceMark`, `Snapshot` | observation only | open |
| open | valid `Jump` | wall offset changes; elapsed unchanged | open |
| open | accepted `Advance` or `AdvanceTo` | owned waiter | advancing |
| advancing | nested/concurrent `Advance` | ordered bounded waiter | advancing |
| advancing | all target work quiesces | exact results delivered | open |
| advancing | work budget exhausted | all waiters fail predictably | open |
| open or advancing | `Shutdown` | scheduled work and waiters released | closed |
| closed | `Shutdown` | `nil` | closed |
| closed | mutating operation | `ErrClosed` | closed |

## Manual timer

| State | Operation | Result | Next state |
| --- | --- | --- | --- |
| created | successful `NewTimer` | owned channel timer | active |
| active | due advancement | one value sent | fired |
| active | `Stop` | `true` | stopped |
| fired | receive from `C` | timestamp | drained |
| fired, drained, or stopped | `Stop` | `false` | unchanged |
| active | `Reset` | `true, nil` | active at new deadline |
| fired, drained, or stopped | `Reset` | `false, nil`; old value drained | active at new deadline |
| any | rejected `Reset` | error | prior state preserved |
| active | `Shutdown` | no channel value | released |
| released | `Reset` | `false, ErrClosed` | released |

## Manual ticker

| State | Operation | Result | Next state |
| --- | --- | --- | --- |
| created | successful `NewTicker` | owned channel ticker | active |
| active | due advancement | send or drop one tick | active at next deadline |
| active | `Stop` | no value | stopped |
| stopped | `Stop` | no value | stopped |
| active or stopped | valid `Reset` | `nil` | active at new period |
| any | invalid/rejected `Reset` | error | prior state preserved |
| active | next-deadline overflow | current tick sent | released |
| active | `Shutdown` | no further values | released |
| released | `Reset` | `ErrClosed` | released |

## Manual callback

| State | Operation | Result | Next state |
| --- | --- | --- | --- |
| created | successful `AfterFunc` | owned callback | active |
| active | due advancement | function starts outside the lock | running |
| active | `Stop` | `true` | stopped |
| running, completed, panicked, or stopped | `Stop` | `false` | unchanged |
| active | `Reset` | `true, nil` | active at new deadline |
| running, completed, panicked, or stopped | `Reset` | `false, nil` | active at new deadline |
| running | callback returns | callback count recorded | completed |
| running | callback panics | panic count recorded; payload discarded | panicked |
| active | `Shutdown` | callback does not start | released |

Resetting a running callback schedules a new invocation and does not wait for
the running invocation, matching the standard callback-timer contract.

## Manual sleep and advancement

| Object/state | Operation | Result | Next state |
| --- | --- | --- | --- |
| sleeper/created | positive `Sleep` | one owned schedule | active |
| sleeper/active | due advancement | `nil` | completed |
| sleeper/active | context done | `ctx.Err()` | canceled and released |
| sleeper/active | `Shutdown` | `ErrClosed` | released |
| advance/created | accepted `Advance` | owned waiter | active |
| advance/active | target work quiesces | exact `Result` | completed |
| advance/active | work limit | partial `Result`, `ErrWorkLimit` | failed and released |
| advance/active | `Shutdown` | partial `Result`, `ErrClosed` | failed and released |
| waiter/active | wait context done | `ctx.Err()` for that wait call | request remains active |

## System differential contract

`System` delegates timer, ticker, and callback state to `time`. The maintained
differential test compares timer and callback `Stop`/`Reset` returns directly
with `time.Timer`. Invalid ticker periods and nil callbacks are translated from
standard-library panics into package sentinel errors. Since Go 1.23, system
timer channels are synchronous and do not require a drain before `Stop` or
`Reset`; manual channels retain their documented one-value deterministic
buffer.

| System API | Delegated lifecycle | Intentional package difference |
| --- | --- | --- |
| `NewTimer`, `Timer.Stop`, `Timer.Reset` | exact `time.Timer` returns and channel rules | factory/reset add interface errors |
| `NewTicker`, `Ticker.Stop`, `Ticker.Reset` | exact valid-period ticker scheduling | invalid periods return `ErrInvalidDuration` |
| `AfterFunc`, callback `Stop`/`Reset` | exact function-timer start and overlap rules | nil function returns `ErrInvalidCallback` |
| `Sleep` | one `time.Timer` until fire or cancellation | context cancellation stops ownership early |

## Callback and sleep notes

Callbacks transition from active to started exactly once. Stop cannot prevent a
started callback. A reset before start removes the prior heap entry before
registering its replacement. Manual callback completion or panic is
synchronized by advancement. Sleepers transition to completed, canceled, or
closed exactly once.

## Ordering

The heap key is `(monotonic deadline, registration sequence)`. A reset receives
a new sequence and removes its superseded entry immediately. Work registered
by a callback at the same instant follows already-registered same-instant work.
Nested advances are linearized at registration and have independent, bounded
waiters.

A running callback freezes implicit future progress. Same-instant work may run
to unblock it, while future progress requires `Wait` on a pending advancement.
This makes multiple stop/reset/create operations in one callback relative to a
stable instant rather than goroutine scheduling.

Ticker backpressure changes delivery, not deadline progression. An advance from
zero through three one-second periods processes deadlines 1s, 2s, and 3s, but a
receiver that never drains observes only the first buffered timestamp.
