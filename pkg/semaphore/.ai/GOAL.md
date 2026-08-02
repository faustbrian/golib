# Goal: Semaphore

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Build `semaphore` as a production-grade process-local counting and weighted
permit primitive. It MUST provide explicit ownership, context-aware admission,
bounded waiting, fair release, deterministic shutdown, and observability that
higher-level packages can safely compose.

This module exists only if its lifecycle and policy guarantees materially
exceed using a channel or `golang.org/x/sync/semaphore` directly. It MUST NOT be
a cosmetic wrapper.

## Authoritative References

- Go memory model, `context`, synchronization, timer, race detector, and fuzz
  documentation;
- `golang.org/x/sync/semaphore` source, behavior, and documentation;
- Shopify Semian semaphore and bulkheading behavior:
  https://github.com/Shopify/semian
- this repository's resilience architecture and engineering policy.

Record versions and behavioral differences at implementation time.

## Core Contract

Support:

- counting permits with positive integer capacity;
- weighted acquisition with positive weights no greater than capacity;
- immediate `TryAcquire` and context-aware blocking `Acquire`;
- optional bounded waiter count with typed saturation rejection;
- an owned permit object whose successful release is exactly once;
- immutable snapshots of capacity, acquired weight, available weight, queued
  waiters, admissions, rejections, cancellations, and shutdown state;
- injected observation hooks outside synchronization; and
- explicit `Close`/shutdown behavior.

Construction MUST reject zero, negative, overflowing, internally inconsistent,
or unbounded configuration before accepting work.

## Admission And Fairness

- Acquisition MUST have a documented linearization point.
- Waiting MUST be FIFO by default and starvation-free under the documented
  model.
- Weighted head-of-line blocking is an intentional consequence of strict FIFO
  and MUST be documented with alternatives, not silently bypassed.
- A canceled waiter MUST be removed promptly without consuming capacity or
  losing a wake-up.
- New callers MUST NOT bypass queued callers unless an explicit non-fair policy
  exists and is named accordingly.
- A request larger than total capacity MUST fail immediately.
- Queue bounds MUST count waiters deterministically under contention.

If priority or resize support is added later, it requires a separate goal with
precise starvation, ordering, and in-flight permit semantics.

## Permit Lifecycle

- A permit MUST expose its acquired weight and stable, non-secret identity
  metadata needed for diagnostics.
- Release MUST be safe to call concurrently but MUST return a typed duplicate
  release result rather than increasing capacity twice.
- Permit completion MUST NOT depend on a finalizer.
- Convenience execution MUST release on success, error, and panic, then
  preserve the operation result or original panic.
- A permit MUST remain releasable after the acquisition context is canceled.
- Abandoned permits are a caller bug; optional leases MUST NOT be added unless
  expiration can be distinguished safely from still-running work.

## Shutdown

Closing MUST be idempotent and define:

- whether new acquisitions fail immediately;
- how queued waiters are released with a stable shutdown error;
- whether existing permits remain valid until released;
- whether `Wait` can block until all acquired weight is returned under a
  caller deadline; and
- behavior of snapshots and duplicate closes after shutdown.

No hidden goroutine is REQUIRED for the core implementation. If a goroutine is
introduced, its owner, bound, cancellation, and leak proof MUST be explicit.

## Kubernetes Semantics

The semaphore is process-local and therefore pod-local. Capacity `N` on `R`
replicas permits up to `N * R` aggregate in-flight weight when load is evenly
distributed. The package MUST NOT claim cluster-wide exclusion.

For a global concurrency constraint, applications MUST use a distributed
system that owns leases and fencing; this package MUST NOT implement a
best-effort distributed semaphore. Docs MUST cover rolling replacement,
scale-up capacity jumps, scale-down drain, and SIGTERM admission closure.

## Boundaries

The package MUST NOT own:

- rate-over-time limiting or quotas;
- fixed resource bulkhead identity and policy;
- adaptive concurrency estimation;
- mutual-exclusion locks, distributed leases, or leader election;
- worker pools, task queues, retries, breakers, timeouts, or fallback;
- Kubernetes coordination; or
- global registries and environment-driven configuration.

`bulkhead` MAY depend on this module's narrow permit contract. Other modules
SHOULD accept consumer-owned interfaces where direct dependency is avoidable.

## Errors And Observability

Stable typed errors MUST distinguish invalid configuration, oversize weight,
queue full, context cancellation/deadline, closed semaphore, and duplicate
release. Errors MUST support `errors.Is`/`errors.As` and never embed arbitrary
keys, callbacks, or secret data.

Observers MUST receive bounded immutable events for admitted, queued, canceled,
rejected, released, and closed transitions. Slow, panicking, or reentrant
observers MUST NOT corrupt permit accounting or execute while locks are held.

## Documentation And Automation

Document quick starts, weighted examples, fairness, cancellation, shutdown,
Kubernetes capacity math, composition with bulkheads, performance tradeoffs,
migration from channels and `x/sync/semaphore`, API reference, FAQ, security,
operations, and changelog.

CI and local gates MUST include formatting, vet, Staticcheck, strict lint,
advisory NilAway, meaningful exact 100% statement coverage, exactly 100% viable mutation
kills, race, fuzz, leak, benchmark, API compatibility, vulnerability,
license, SBOM, provenance, docs, and clean-consumer checks.

## Acceptance Criteria

- Permit conservation holds for every concurrent history.
- Fair waiters cannot be bypassed or stranded.
- Cancellation and shutdown release all queued state promptly.
- No release path can exceed configured capacity.
- Kubernetes docs state local scope and aggregate scaling honestly.
- Every exported behavior is documented and proven under repository gates.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
