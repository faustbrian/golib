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
