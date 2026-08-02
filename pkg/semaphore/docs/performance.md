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

The benchmark matrix covers immediate and parallel acquire/release, mixed
weights, already-canceled contexts, strict-FIFO head-of-line handoff, queue
depths 1/32/256, allocations, and no-op observer overhead. It compares the
owned implementation with `golang.org/x/sync/semaphore` v0.22.0, a buffered
channel, a minimal `sync.Cond` counter, and
`github.com/v8fg/kit4go/semaphore` v0.9.0. The latter was released on
2026-07-28 and was selected as an actively released third-party semaphore.

Interpret only like-for-like operations. The semantic differences are part of
the result:

| Baseline | Weight | Waiting policy | Queue bound | Release ownership | Shutdown/observation |
| --- | --- | --- | --- | --- | --- |
| this package | positive weighted | strict FIFO with head-of-line blocking | explicit | permit, exactly once | close, drain, snapshot, observer |
| x/sync v0.22.0 | non-negative weighted | strict FIFO with head-of-line blocking | none | caller supplies weight | none |
| buffered channel | unit only | scheduler/channel order, not this FIFO contract | token capacity only | caller receives token | channel semantics |
| minimal `sync.Cond` | unit only | unfair signal selection | none | caller increments counter | none |
| kit4go v0.9.0 | weighted | unit requests may bypass weighted requests | none | caller supplies weight | close; no owned drain or observer |

The channel, `sync.Cond`, and kit4go contention numbers therefore measure
weaker fairness or lifecycle contracts and are not evidence that those designs
are substitutes. The strict-FIFO benchmark compares only this package and
x/sync, and names the bounded-queue and owned-release difference in the
sub-benchmark labels.

The retained local sample records Go version, operating system, architecture,
CPU, capacity, parallelism, allocations, sample count, command, dependency
versions, a statistical summary, and the raw-output digest in
[`benchmarks/2026-08-02-darwin-arm64.md`](benchmarks/2026-08-02-darwin-arm64.md).
It is reproducible local evidence, not a cross-machine performance guarantee.

Shopify Semian is not included in the Go microbenchmark because its SysV,
cross-process Ruby bulkhead semantics are not equivalent to this process-local
primitive.
