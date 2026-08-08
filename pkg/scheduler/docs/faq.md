# FAQ

**Is this a Kubernetes controller?** No. It runs as an ordinary Deployment.

**Why not use CronJobs?** Use them for infrastructure and isolated commands.
Application schedules benefit from one code-defined registry and durable queue
dispatch.

**Is execution exactly once?** No. Leases and idempotency reduce duplicates,
but crashes around external effects leave at-least-once behavior.

**Can I run functions directly?** Yes through `Executor`. `RunTimeout` bounds
the tick wait, but code that ignores cancellation stays tracked and consumes a
fixed execution slot until it returns. Prefer `queue.Dispatcher`.

**Can I schedule shell commands?** Not through the core or control surfaces.

**Which lease backend should tests use?** `memory.Store` with deterministic
instants. Production replicas should share PostgreSQL or Valkey 9.

**Why is there no schedule group API?** Shared option slices and construction
helpers keep grouping explicit, typed, and immutable without introducing a
mutable configuration DSL.

**Why are queue name and connection absent from a schedule?** The schedule owns
timing. `queue.Dispatcher` owns durable delivery, and an application adapter may
route by task identity if it needs multiple queues or backends.

**How do applications pause or interrupt scheduling?** Supply a shared
`PauseSource`, invoke its application-owned `PauseController`, and use
`EvenWhenPaused` for essential tasks. To interrupt a running scheduler, cancel
the `Run` context and call `Drain` with a deadline.
