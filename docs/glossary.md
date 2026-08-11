# Glossary

**Adapter**
: A package that implements an owning contract for a transport, backend, or
provider. Dependency direction points from the adapter to the contract.

**Application use case**
: Transport-neutral orchestration of one business operation. HTTP, RPC, queue,
and CLI adapters map into it rather than sharing transport models.

**Content-bound evidence**
: Verification tied to the complete relevant input fingerprint and retained
log checksum. Git revisions are traceability metadata, not validity keys.

**Control plane**
: Operator visibility and commands for a runtime system. For queues this is
owned by `queue-control-plane`, not by worker delivery primitives.

**Delivery point**
: The exact boundary at which enqueue, publish, process, or acknowledgement is
considered durable. Benchmarks and retries must name it.

**Fencing token**
: A monotonically ordered value that lets a resource reject stale lease owners
after expiry, takeover, or network delay.

**Owned dependency**
: Another Golib module imported by a module. Owned dependencies remain explicit
and independently versioned.

**Provider adapter**
: An optional module for PostgreSQL, Valkey, Redis, a broker, cloud service, or
other external system. Core modules should remain usable without unrelated
providers.

**Releasable module**
: A public Go module with its own semantic version and prefixed tag. Internal
tools, fixtures, examples, and benchmark harnesses are cataloged but not
released independently.

**Unknown outcome**
: An operation may have completed remotely or durably even though the caller
did not receive confirmation. It must not be collapsed into an ordinary retry
without idempotency or reconciliation.

Return to the [documentation index](index.md).
