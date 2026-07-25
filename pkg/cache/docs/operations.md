# Operations guide

## Before deployment

Choose explicit bounds from measured value sizes and concurrency. Configure
the native Redis or Valkey client with authentication, verified TLS, dial/read/
write timeouts, retry limits, pool limits, and the intended standalone
database. The initial release does not claim cluster, Sentinel, failover, or
replica-read support.

Use a distinct namespace, cache name, key version, and codec version for each
semantic value type. During an incompatible rollout, deploy readers for the new
version before writers or bump the key-space version so mixed binaries never
interpret the same bytes differently.

## Startup and readiness

Construct dependencies in this order:

1. native client and connectivity check;
2. backend adapter with a bounded wire-record size;
3. key space and codec;
4. semantic cache with value, batch, loader, and waiter limits;
5. observers and exporter health checks.

A backend health check proves availability only. It does not prove hit, miss,
stale, invalidation, authentication, or codec correctness; those belong in
application smoke tests.

## Invalidation

Commit the source-of-truth mutation first, then delete every affected cache
key. Retry individual failures returned by `DeleteMany`. A same-instance
in-flight load cannot overwrite the successful deletion. Other processes are
not fenced; critical workflows should include a source version in the value or
key and reject obsolete writes at the source boundary.

Never use stale-while-revalidate or stale-if-error for authorization, balances,
pricing, revocation, or other data where old values can violate correctness.

## Outage and recovery

Backend errors are not misses. Decide fail-open or fail-closed in the use case.
For a reconstructible non-sensitive read, bypass explicitly and retain the
error signal. For sensitive or ordering-critical data, fail closed.

The native clients own reconnect behavior. Integration tests prove recovery at
a stable standalone endpoint after an interrupted connection. They do not prove
DNS changes, failover promotion, cluster redirects, or replica convergence.

## Capacity and expiration

Memory capacity counts key and payload bytes, not Go map/list/runtime overhead;
leave headroom. Expired memory entries are removed lazily when touched, so an
idle expired entry remains counted until access, eviction, or `Close`. There is
no janitor goroutine.

Redis and Valkey may evict records according to server policy before their hard
deadline. Treat that as a normal miss, not durability loss. Monitor server
memory, eviction policy, rejected connections, latency, and command errors in
addition to cache-level events.

## Shutdown

Stop accepting new work, then:

1. call `Cache.Close` to cancel and join loaders;
2. close the memory backend if used;
3. close the supplied Redis/Valkey client;
4. flush and close telemetry exporters.

A loader must honor its supplied context. `Close` intentionally waits rather
than leaking a loader goroutine that could later write shared state.

## Alerts and runbooks

Alert separately on backend errors, loader errors, waiter-limit rejection,
stale ratio, negative ratio, latency, evictions, and retained memory. A high hit
rate is not a freshness signal.

For unexpected misses, compare namespace/name/version, codec version, native
database, hard deadline, server eviction, and invalidation history. For schema
errors, stop readers from treating corruption as absence, identify the writer
version, and roll forward with a new key/codec version.
