# Caching and offline bundles

`ResolveCache` bounds entries, concurrent loads, positive freshness, stale
eligibility, and negative freshness. A selector has one upstream flight, but
each waiter applies its own policy:

- `FailClosed` returns upstream failure.
- `AllowStale` returns an eligible stale value only for `ErrUnavailable` and
  exposes that outage as `StaleCause`. Authoritative absence, authorization,
  cancellation, malformed responses, and identity mismatches remain failures.
- `CacheOnly` performs no provider I/O and returns `ErrOfflineMiss` without a
  usable entry.
- `ReturnUnavailable` performs no lookup and returns `ErrUnavailable`.

`Prime` is the explicit preload path. Results are verified against their lookup
before storage to prevent cache poisoning. Observers receive only state and
outcome, never schemas, subjects, IDs, payloads, or credentials.

`Bundle.MarshalBinary` creates a deterministic versioned local artifact with
provenance. `LoadBundle` applies byte and graph bounds, recompiles every schema
with local canonicalizers, verifies every claimed fingerprint, and rejects
missing dependencies. Loading and resolution never fetch remote resources.
Distribute bundles as immutable release artifacts and verify their outer digest
or signature in the deployment system.
