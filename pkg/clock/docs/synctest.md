# `testing/synctest` interoperability

Go 1.26's `testing/synctest` is preferred when all relevant goroutines and
standard-library timers fit inside one bubble. Fake time advances when bubble
goroutines are durably blocked, and `synctest.Wait` provides the standard
quiescence definition.

`clocktest.SystemBubble` supplies `clock.System{}` inside a bubble. Because
`System` delegates to `time`, it naturally observes bubble time. `clocktest.Wait`
checks a context and delegates quiescence to `synctest.Wait`. The package does
not install another fake clock or duplicate bubble scheduling.

Use dependency injection instead when:

- a business timestamp must be explicitly asserted;
- selected components, rather than the whole test, need controlled time;
- wall rollback, forward jumps, or frozen wall time are scenarios;
- a clock interface crosses a package contract;
- code cannot be wholly created inside a bubble.

Manual clocks may be used inside a bubble, but their time changes only through
manual advancement. Do not expect bubble fake time to advance a manual clock.

## Compatibility matrix

| Behavior | `System` in a bubble | `manual.Clock` in a bubble |
| --- | --- | --- |
| `Now` and `Since` | bubble wall/monotonic time | explicit manual wall/elapsed time |
| `Sleep`, timer, ticker | advances at durable quiescence | requires `Advance` |
| `AfterFunc` | bubble-owned callback goroutine | manual coordinator-owned callback |
| quiescence | `clocktest.Wait` / `synctest.Wait` | advancement `Waiter` |
| wall jump or freeze | not injectable | `Jump` independent of `Advance` |
| bubble isolation | timer/channel must stay in bubble | ordinary injected Go object |
| shutdown ownership | caller stops each resource | `Shutdown` releases clock-owned work |

The test suite composes `System` sleep, timers, tickers, callbacks, elapsed
measurement, and quiescence in bubbles. Manual lifecycle tests also run inside
bubbles where synctest is useful for goroutine startup, while retaining
explicit advancement as the only manual-time authority.
