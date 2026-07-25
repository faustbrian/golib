# Stampede control and concurrency model

`GetOrLoad` coalesces work by the fully encoded backend key. Coalescing is
process-local; it is not a distributed lock and does not promise exactly-once
loading across processes.

The first miss creates a flight. Before entering the loader, the flight reads
the cache again so a concurrent writer can satisfy it. Independent flights
share a global semaphore capped by `MaxConcurrent`. Callers joining one flight
are capped by `MaxWaitersPerKey`.

Each caller waits with its own context. Cancellation detaches only that waiter.
The loader receives a cache-owned context so one impatient caller cannot cancel
work needed by others. `Close` cancels that shared context, prevents new
flights, waits for active goroutines, and is idempotent.

Loader panics are recovered, classified as `ErrLoaderPanic`, and cannot poison
the flight map. Source errors match `ErrLoader`; they are never converted into
negative entries. A `Found:false` result may create a bounded negative record.

A successful `Set`, `Add`, `Replace`, or `Delete` on the same cache instance
wins over a concurrent foreground load or background refresh. Failed writes
and rejected conditions do not suppress a valid loader result. This ordering
does not fence mutations made by another process or another `Cache` instance.

Stale-while-revalidate uses the same flight map and global bound. Repeated stale
reads therefore start one background refresh per key. Stale-if-error waits on
the same foreground flight and returns the stale value plus the refresh error.

Applications must make loaders context-aware and must bound their own network
clients. A loader that ignores cancellation can delay `Close` indefinitely.

The global loader semaphore does not promise FIFO fairness. Independent keys
make progress subject to Go scheduler and channel scheduling. A loader must not
recursively call the same cache with its supplied context; such calls return
`ErrRecursiveLoad`. If it discards that context, recursion cannot be detected
and may deadlock, which is another reason context propagation is mandatory.
