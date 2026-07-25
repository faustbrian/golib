# FAQ

## Why not use `time` directly?

Do so when it is sufficient. A clock seam is useful only for explicit business
timestamps, selected deterministic control, or cross-package contracts.

## Why not one broad clock interface?

Broad interfaces force timestamp-only clients to depend on timer ownership and
make mocks brittle. Small capabilities communicate the actual dependency.

## Why are manual wall time and elapsed time separate?

Real wall clocks can roll backward. Elapsed correctness must survive that
scenario, so `Jump` never changes marks or scheduled monotonic deadlines.

## Why did my ticker produce one value after a large advance?

Its channel holds one timestamp. Further due ticks are processed and dropped
until the receiver drains the channel, matching ticker backpressure intent.

## Why does a callback waiting on a future timer block?

Manual time never advances by scheduler guess. Schedule same-instant work or
issue a nested `Advance` for future work. Use `testing/synctest` for automatic
whole-test fake-time progression.

## Are observers asynchronous?

No. They are synchronous, panic-isolated, and expected to return promptly. The
package intentionally owns no exporter or hidden telemetry goroutine.
