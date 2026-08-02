# Replay and idempotency safety

Concurrent replay is safe only when duplicate execution has an explicitly
reviewed outcome. Reads can still be unsafe when they consume streams, mutate
caches, acquire leases, or trigger billing. Writes, payments, acknowledgements,
transactions, and callbacks are unsafe by default.

An idempotency key is useful only if every proxy, queue, service, datastore,
and side-effecting integration preserves the key and suppresses duplicates for
the entire retry and hedge horizon. Authentication and tenant context must be
identical across attempts. `ReplaySafe` records the caller's decision; the
package neither verifies nor broadens it.

Each factory invocation must allocate independent mutable headers, byte slices,
bodies, readers, destinations, and callbacks, or use explicit synchronization.
Go's `http.Request.Clone` shallow-copies `Body`; a fresh body must be created
through `GetBody` or an application-owned replay mechanism.
