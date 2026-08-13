# semaphore

`semaphore` is a process-local, FIFO weighted counting semaphore for Go 1.26.6
and newer. It adds bounded waiting, owned exactly-once permits, deterministic
shutdown, immutable snapshots, and bounded observation events to the basic
counting-semaphore pattern.

Use it when those lifecycle and policy guarantees are required. For a simple
fixed concurrency cap, a buffered channel remains smaller. For admission
without ownership, shutdown, or queue observability, consider
`golang.org/x/sync/semaphore`.

## Quick start

```go
sem, err := semaphore.New(semaphore.Config{
    Capacity: 4,
    // Zero disables waiting; positive values bound the FIFO queue.
    MaxWaiters: 64,
})
if err != nil {
    return err
}

value, err := semaphore.Execute(ctx, sem, 2,
    func(ctx context.Context) (string, error) {
        return perform(ctx)
    },
)
```

`Execute` releases on success, error, and panic, then preserves the returned
value and error or the original panic. Manual ownership is explicit:

```go
permit, err := sem.Acquire(ctx, 2)
if err != nil {
    return err
}
defer permit.Release()
```

The permit remains releasable after `ctx` is canceled. A second or concurrent
duplicate release returns `*DuplicateReleaseError` and cannot add capacity.

## Contract

- Capacity and weights are positive `int64` values. A weight above capacity
  fails immediately.
- `Acquire` admits immediately only when capacity is available and the queue is
  empty. Otherwise it enters the bounded FIFO queue or returns `ErrQueueFull`.
- `TryAcquire` never waits and never bypasses queued callers. Capacity
  unavailability is `(nil, false, nil)`; invalid, oversized, and closed states
  return typed errors.
- Strict FIFO prevents starvation under the model that admitted permits are
  eventually released and waiting contexts remain live. A large head waiter
  intentionally blocks smaller followers. Split large requests or use separate
  semaphores only when that different policy is explicit and safe.
- Admission linearizes while the semaphore mutex is held when acquired weight,
  admission count, and permit identity are assigned. Release linearizes under
  that mutex when the exactly-once decision and capacity return occur. If
  cancellation races with a grant, the mutex winner determines the
  result; a completed grant returns an owned permit.
- `Close` is idempotent, rejects new and queued acquisition with `ErrClosed`,
  and leaves existing permits valid. `Wait(ctx)` waits only for acquired weight
  to return; call `Close` first to stop admission and reject the queue.
- No goroutine, timer, finalizer, registry, environment lookup, or distributed
  coordination is owned by the implementation.

Snapshots contain capacity, acquired and available weight, queued waiters,
admissions, rejections, cancellations, and shutdown state. Observers receive
immutable low-cardinality events outside the accounting lock. Observer panics
are recovered; slow observers delay only the caller delivering that event, and
observers must support concurrent calls. Events contain no caller keys, errors,
callbacks, context values, or arbitrary labels.

## Errors

Use `errors.Is` for categories and `errors.As` for bounded details:

- `ErrInvalidConfig` / `*ConfigError`
- `ErrInvalidWeight` or `ErrOversize` / `*WeightError`
- `ErrQueueFull` / `*QueueFullError`
- `ErrCanceled` or `ErrDeadline` / `*CanceledError`, also matching the
  corresponding `context` error
- `ErrClosed` / `*ClosedError`
- `ErrDuplicateRelease` / `*DuplicateReleaseError`

Validation precedes lifecycle checks, and a context already done when
`Acquire` begins precedes the closed-state check. These paths all return
immediately. As with standard Go context APIs, callers must not pass a nil
context.

## Scope and composition

This package owns only counting and weighted permits. It does not own rate
limits, bulkhead identity, adaptive limits, worker pools, queues, retries,
breakers, timeouts, fallbacks, locks, leases, or Kubernetes coordination.
A `bulkhead` may compose this narrow permit contract with resource identity and
fixed isolation policy; applications should otherwise depend on the smallest
consumer-owned interface they need.

The semaphore is process-local and therefore pod-local. With capacity `N` on
`R` replicas, aggregate in-flight weight can reach `N * R`. It is not global
exclusion. See [Kubernetes operations](docs/kubernetes.md) before using local
capacity in a replicated workload.

## Documentation

- [Design, fairness, cancellation, shutdown, and references](docs/design.md)
- [Kubernetes semantics](docs/kubernetes.md)
- [Operations and security](docs/operations.md)
- [Migration from channels and x/sync](docs/migration.md)
- [Performance and benchmarks](docs/performance.md)
- [API reference](docs/api.md)
- [FAQ](docs/faq.md)
- [Security policy](SECURITY.md)
- [Release notes](CHANGELOG.md)
