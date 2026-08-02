# FAQ

## Why require an algorithm?

It makes adoption and compatibility explicit. `NewDefaultAlgorithm` is a named
choice rather than a silent constructor behavior.

## Why not learn requests per second?

Concurrency directly represents occupied work. Little's Law relates it to
throughput and latency; a fixed RPS threshold becomes stale when latency or
capacity changes.

## Why did latency increase after a workload change?

The baseline deliberately rises slowly. If the new class is legitimate, the
limit will probe downward and recover as the baseline adapts. Persistent
bimodality may require resource-aligned limiter boundaries.

## Why was a local rejection excluded?

No downstream capacity was consumed, so treating it as dependency failure
would create a false feedback loop.

## Can state be shared across pods?

No. This implementation is intentionally pod-local. A distributed controller
needs membership and failure semantics that are outside this package.

## Does priority reorder callers?

No. Metadata is bounded diagnostics only and admission is FIFO. This prevents
caller-inflated priority and starvation.
