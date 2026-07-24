# Security model

## Assets and trust boundaries

The package protects process availability, deterministic test behavior, clock
state integrity, and observation confidentiality. Durations, callback code,
tags, and concurrent lifecycle operations are caller-controlled inputs.

## Controls

- Active timers, tickers, callbacks, and sleepers are capped by `MaxActive`.
- Outstanding advancement waiters are independently capped by `MaxActive`.
- Reset and stop remove superseded heap entries immediately.
- One advancement is capped by `MaxWorkPerAdvance`.
- Duration arithmetic and sequence allocation reject overflow.
- Wall rollback cannot reverse the manual monotonic counter.
- Callback and observer panics do not corrupt manual clock state.
- Observations bound tag cardinality/size and omit sensitive payloads.
- Shutdown releases scheduled work and wakes owned waiters.
- Production code is scanned for `unsafe`, cgo, `go:linkname`, runtime patching,
  and global test-clock patterns.

The default limits are 65,536 active objects, 65,536 outstanding advancement
waiters, and 1,000,000 triggered events per advancement. Applications handling
untrusted schedules should configure lower budgets appropriate to their
request and memory limits.

## Caller responsibilities

Go cannot terminate arbitrary callbacks. Callback owners must return, honor
their own cancellation policy, and avoid waiting for future manual time without
a nested advancement. Tags must be bounded classifications, not sensitive or
high-cardinality values.

This package does not make distributed time trustworthy. Use fencing and
version protocols for cross-process correctness.
