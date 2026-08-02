# Performance

Uncontended acquisition performs one mutex transition and allocates an owned
permit. Contended acquisition additionally allocates one waiter and channel.
Strict FIFO may reduce utilization when a large head request does not fit while
smaller followers would; this is the documented starvation-prevention tradeoff.
Observer cost is caller-visible but never occurs under the accounting lock.

Run reproducible local benchmarks with:

```sh
go test -run '^$' -bench . -benchmem -count=10
```

`BenchmarkUncontendedWeightedAcquire` compares the common immediate unit-weight
acquire/release operation with `golang.org/x/sync/semaphore` v0.22.0, a buffered
channel, and a minimal `sync.Cond` counter. It also isolates no-op observer
overhead. Interpret these only for that shared operation: the baselines lack
some or all of owned duplicate-safe release, positive weighted validation,
bounded FIFO waiting, shutdown, snapshots, and events. Contended, mixed-weight,
and canceled-context benchmarks cover additional owned paths. Publish Go
version, operating system, architecture, CPU, capacity, parallelism,
allocations, sample count, and a statistical comparison with any retained
result; repository source does not contain machine-specific performance claims.

Shopify Semian is not included in the Go microbenchmark because its SysV,
cross-process Ruby bulkhead semantics are not equivalent to this process-local
primitive.
