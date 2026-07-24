# Migration guide

Given a package-local interface:

```go
type Clock interface { Now() time.Time }
```

change internal fields and constructor parameters to `clock.Clock`, alias the
old interface temporarily when source compatibility requires it, and keep the
existing default behavior by injecting `clock.System{}`.

Replace global test setters with per-test instances:

```go
fake, err := manual.New(start)
service := NewService(fake)
```

Replace real sleeps with `Sleeper.Sleep` or an owned timer. Advance the fake and
wait on its returned waiter before asserting. Replace wall-based elapsed
subtraction with `Measure`; serialized timestamps are not elapsed marks.

Ticker migrations must preserve drop/backpressure assumptions. Timer migrations
must preserve stop/reset return handling. Callback migrations must assign an
owner responsible for callback termination and clock shutdown.

Do not expose `FullClock` merely because it is convenient. Public interface
widening makes later evolution harder and forces strict mocks to implement
unrelated capabilities.
