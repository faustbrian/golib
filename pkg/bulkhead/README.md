# bulkhead

`bulkhead` is a process-local, fixed-capacity isolation policy for finite
resources. It combines caller-owned resource identity, weighted concurrency,
immediate rejection or strict FIFO bounded waiting, bounded explicit
partitions, lifecycle observations, and graceful drain.

It is not a worker pool, queue, rate limiter, circuit breaker, retry engine,
timeout, adaptive limiter, distributed lock, or cluster-wide concurrency
control.

## Quick start

```go
database, err := bulkhead.New(bulkhead.Config{
    Resource:       "inventory-db",
    PolicyRevision: "2026-08-02",
    Capacity:       16,
    Admission: bulkhead.Wait{
        MaxQueued: 32,
        MaxWait:   25 * time.Millisecond,
    },
})
if err != nil {
    return err
}

row, timing, err := bulkhead.Execute(ctx, database, 1,
    func(ctx context.Context) (*sql.Row, error) {
        return db.QueryRowContext(ctx, query), nil
    },
)
```

`timing.WaitDuration` and `timing.ExecutionDuration` are separate. An
operation that ignores `ctx` continues to hold capacity until it actually
returns.

## Standalone permits

```go
permit, err := database.Acquire(ctx, 2)
if err != nil {
    return err
}
defer permit.Release()
```

Release is concurrency-safe and exactly once. A second release returns
`ErrPermitReleased`; a successful permit remains releasable after its
acquisition context is canceled.

## Error contract

Admission outcomes remain distinguishable with `errors.Is`:

- `ErrRejected`: immediate capacity rejection;
- `ErrQueueFull`: bounded queue saturation;
- `ErrCallerCanceled` joined with `context.Canceled` or
  `context.DeadlineExceeded`;
- `ErrWaitTimeout`: the configured maximum queue wait elapsed;
- `ErrClosed`: drain has stopped admission;
- `ErrInvalidWeight`: non-positive or over-capacity weight;
- `ErrReentrant`: the same bulkhead was recursively acquired through an
  `Execute` context.

Protected-operation errors pass through unchanged and are not reclassified as
admission errors.

## Explicit partitions

Use one shared `Bulkhead` for every path consuming the same constrained
resource and different instances for independent failure domains. A `Registry`
provides bounded, application-owned partition identity:

```go
registry, _ := bulkhead.NewRegistry(
    bulkhead.FixedPartitions{Maximum: 8},
)
inventory, _ := registry.Create(inventoryConfig)
payments, _ := registry.Create(paymentsConfig)
```

Lookup never creates a partition and no package-global registry exists.
Partitions are never evicted automatically. Configuration replacement is
explicit: `Close`, `Drain`, `Remove`, then `Create`. `Remove` rejects a
partition unless it is closed and fully drained, so an active permit is never
discarded and a retained old pointer cannot admit work under a second policy.

## Waiting and fairness

`RejectImmediately` is the low-latency default. `Wait` requires positive
`MaxQueued` and `MaxWait` bounds. Waiters are admitted in strict FIFO order.
New calls do not bypass a queued call, including when a lighter request would
fit. This makes starvation behavior predictable but intentionally permits
weighted head-of-line blocking. Use independent partitions when workloads need
independent fairness or latency objectives.

The caller context is the total deadline. `MaxWait` can shorten that deadline
but never extends it. Synchronous observer latency is opt-in through a non-nil
observer and consumes request and queue-wait time.

## Drain

Application shutdown order is:

1. stop readiness or otherwise stop new external traffic;
2. call `Close` to reject new work and wake queued callers with `ErrClosed`;
3. call `Drain` with the pod termination deadline;
4. let admitted operations return and release their permits;
5. treat `ErrDrainIncomplete` joined with the context error as ambiguous:
   capacity was not falsely reclaimed and the operation might still run.

`Close` and `Drain` are idempotent with respect to admission closure. Abrupt
process termination cannot provide completion evidence.

## Kubernetes sizing

Capacity is per process and therefore per pod. For detailed equations using
downstream capacity, fan-out, connection pools, HPA maximum replicas, and
rolling-update surge, see [Kubernetes and sizing](docs/kubernetes.md).
Dependency saturation must not fail liveness. Rejections can lower CPU and make
CPU-only autoscaling scale the wrong way; queue depth, rejections, and wait
latency need workload-specific alerting or carefully reviewed custom metrics.

## Documentation

- [API and ownership](docs/api.md)
- [Architecture and invariants](docs/architecture.md)
- [Composition](docs/composition.md)
- [Kubernetes and sizing](docs/kubernetes.md)
- [Operations](docs/operations.md)
- [Migration](docs/migration.md)
- [Security](docs/security.md)
- [Performance](docs/performance.md)
- [Verification and hardening](docs/hardening-audit.md)
- [FAQ](docs/faq.md)

## License

MIT. See [LICENSE](LICENSE).
