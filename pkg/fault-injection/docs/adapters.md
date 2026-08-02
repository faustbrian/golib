# Adapter contracts

## Phase model

`PhaseBefore` runs before delegation. `PhaseDuring` represents the owned
operation window: latency is applied at entry, and cancellation/deadline faults
pass an already-ended child context to context-aware delegates before the
injected context error is returned. `PhaseAfter` runs after delegation and
closes newly acquired resources when it replaces success with failure.

This is deterministic in-process behavior. It does not claim scheduler,
packet, kernel, proxy, database, broker, or cluster fidelity.

## Generic functions

`Run` skips the operation for before-phase error/cancel/deadline faults. A
during-phase context fault invokes the operation with an ended child context so
cleanup is testable. Any injected error returns zero `T`. A selected panic is
accounted before the bounded safe string is panicked.

## Reader

- short read and truncate cap one delegated read;
- corrupt XORs the first returned byte with a nonzero mask;
- reorder reverses a bounded returned prefix;
- drop consumes delegated bytes and returns `ErrDropped`;
- duplicate returns the original bytes and retains one bounded replay buffer;
- interruption returns a bounded partial result with `ErrStreamInterrupted`;
- before/during errors return no bytes; after errors preserve `n`.

No lock is held across delegated IO. A duplicate buffer is bounded by the
fault limit and remains attributable to the decision that created it, including
if reset occurs before it is read.

## Writer

- short write and truncate delegate a bounded prefix and return
  `io.ErrShortWrite` when the caller supplied more data;
- corruption and reordering copy and transform only a bounded prefix while
  preserving the remaining caller bytes;
- drop reports the caller buffer accepted without delegating it;
- duplicate delegates the bounded input twice;
- interruption preserves the delegated partial count and returns
  `ErrStreamInterrupted`;
- after errors preserve the delegated `n`.

Organic partial results and errors are returned unchanged unless an explicitly
selected after fault replaces the error.

## HTTP

The adapter preserves the `http.RoundTripper` request immutability and
concurrent-use contract. An injected error before transport closes the request
body. A fault after response acquisition closes the response body. On success,
the body remains caller-owned and is wrapped at `BoundaryHTTPBody`; `Close`
always delegates to the original body.

## Network

`WrapConn` preserves addresses, deadlines, and close ownership. Reset closes
the connection. Half-close uses `CloseRead` or `CloseWrite` where available and
falls back to full close. `NetworkError` distinguishes temporary from permanent
injected failures. A dial or accept result rejected after establishment is
closed before the injected error is returned.

## Filesystem

Open and read use distinct boundaries. An after-open fault closes the acquired
file. Successful files remain caller-owned; `Stat` and `Close` delegate to the
original `fs.File`. This adapter does not invent write, rename, permission, or
transaction semantics.

## Time

`WrapSleeper` preserves caller cancellation. `WrapTimerFactory` stops a timer
rejected after construction. Timer channel, stop, reset, and ownership remain
defined by the supplied factory.
