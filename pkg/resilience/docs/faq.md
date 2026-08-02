# FAQ

## Is this a resilience framework?

No. It is a small explicit composition contract used by focused packages.
There is no service container, preset stack, registry, reflection discovery,
environment configuration, or global executor.

## Why not add a timeout policy?

A goroutine raced against a timer cannot stop arbitrary synchronous work. The
caller context owns the total deadline, and adapters may derive shorter
scope-specific deadlines.

## Is the budget distributed?

No. The built-in implementation is process-local. A distributed implementation
must explicitly implement `WorkBudget` and document availability, consistency,
latency, and failure semantics.

## Does retry imply idempotency?

No. Callers and protocol adapters remain responsible for replay safety and
idempotency keys.

## Why are observers synchronous?

The core must not own an unbounded telemetry worker lifecycle. A telemetry
adapter can provide a bounded asynchronous queue with explicit shutdown.
