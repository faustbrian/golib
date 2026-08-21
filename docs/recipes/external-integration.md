# External Dependency Integration Recipe

Vendor integrations should expose a small application-facing client while
keeping transport, authentication, pagination, retry, rate limits, circuit
state, caching, and telemetry at the infrastructure boundary.

## Policy Order

1. Derive one caller-owned total deadline.
2. Acquire local or distributed admission for each physical attempt.
3. Apply remote rate limits and explicit `Retry-After` policy.
4. Retry only replay-safe operations and bodies.
5. Hedge only idempotent calls with a shared additional-work budget.
6. Classify local rejection separately from remote failure.
7. Verify and close every response body before returning ownership.

The maintained fixture combines bounded HTTP calls, focused resilience
controls, signed webhook delivery, filesystem storage, and authenticated
secret envelopes through injected dependencies.

```sh
./scripts/run-modules.sh check --jobs 1 --modules pkg/service/integration/reference-external
```

Its loopback and in-memory adapters prove composition and lifecycle, not live
provider credentials, quotas, failover, or production networking. Add
provider-specific interoperability without weakening the shared client
contract.

Related guidance: [resilience](../resilience.md),
[integration ownership](../integration-map.md), and
[limitations](../limitations.md).

Return to the [recipe index](index.md).
