# Policy decision guide

## Plain cache-aside

Start with `GetOrLoad` and no optional stale or negative policy. This is the
easiest behavior to reason about: fresh cache hits return immediately, misses
coalesce into a source lookup, and failures reach the caller.

Use it when the source can absorb a miss after expiration and callers require
fresh values.

## Negative caching

Set `NegativeTTL` only when `Found:false` means authoritative absence. Keep it
shorter than the positive TTL because creation of a formerly absent resource
otherwise remains invisible until the negative entry expires.

Do not negative-cache permission failures, timeouts, decode failures, or
partial upstream responses. Return an error from the loader instead.

## Stale-while-revalidate

Enable `StaleWhileRevalidate` when tail latency matters more than surfacing a
refresh failure. A stale read starts at most one bounded background refresh and
returns the stale value immediately. Observe refresh errors because callers do
not receive them.

Good fits include public catalog data and other bounded-staleness reads.

## Stale-if-error

Enable `StaleIfError` when a caller may use old data during source failure but
must know that refresh failed. The call waits for the coalesced refresh and
returns the stale result together with the loader error.

Good fits include vendor lookups where a caller can explicitly degrade its
response.

Do not use either stale policy for authorization, revocation, balances,
pricing, or other decisions where an old value can violate correctness.

## Sliding TTL

Sliding TTL extends a fresh record on each successful hit using atomic
`IfPresent`. Use it for activity-based data where frequent reads should keep a
record warm. Avoid it for data that must be periodically revalidated regardless
of traffic. Sliding TTL does not extend stale records.

## Jitter

Set `RefreshJitter` below the positive TTL to spread loaded-value expirations.
Jitter subtracts from the fresh TTL; it never lengthens freshness. Inject a
deterministic `JitterSource` in tests.

## Cross-process invalidation

Load coalescing and mutation precedence are process-local. If another process
can invalidate while this process loads, use source versioning, versioned keys,
or another application-level fence. The cache does not claim a distributed
lock or globally ordered invalidation.
