# Performance

## What To Measure

Benchmark protocol decode, validation, dispatch, response encode, batch
handling, and client correlation separately. Use realistic method payloads and
report Go version, hardware, request shape, batch size, allocations, and
benchmark duration.

## Required Checks

Run:

```sh
make benchmark
make test-race
```

Performance changes must preserve protocol semantics and error behavior.
Allocation reductions are not accepted when they weaken validation, duplicate
ID detection, cancellation, or response correlation.

## Capacity Planning

The package does not define server concurrency or admission control. Measure
the complete transport and handler stack under the expected method mix,
payload distribution, and deadline policy.
