# Goal: Harden Cache Resilience Integration

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Required Proof

- Race hit, miss, stale read, expiry, invalidation, refresh, cancellation,
  loader panic, backend failure, eviction, close, and policy revision.
- Prove only eligible stale/negative results are returned and metadata always
  reveals freshness.
- Simulate many pods cold-starting, refreshing one key, rolling key revisions,
  backend failover, network partition, SIGTERM, and abrupt death.
- Test refresh through retry, breaker, bulkhead, adaptive policies, and hedge
  without multiplying loader work.
- Prove all keys, values, waiters, leases, observations, timers, and goroutines
  are bounded and cleaned up.

Meaningful exact 100% statement coverage, exactly 100% viable mutation kills,
race, fuzz, stress, leak, backend fault, benchmark, API compatibility, docs,
security, and supply-chain gates MUST pass. Final review MUST find no unsafe
stale fallback, refresh overwrite, fleet stampede claim gap, hidden retry, or
unbounded retention.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
