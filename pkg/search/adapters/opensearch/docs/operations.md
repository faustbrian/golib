# Operations and deployment

## Deployment checklist

1. Supply HTTPS seed endpoints from configuration; do not derive them from a
   tenant or request. Use private/VPC endpoints where the deployment supports
   them.
2. Configure peer-verified TLS, a request timeout, maximum response bytes,
   maximum in-flight requests, and either no queue or a bounded queue wait.
3. Choose exactly one authentication mechanism: a rotation-aware basic
   credential provider or an AWS signer.
4. Keep proxying disabled unless an explicit reviewed policy is required.
   Environment proxying is opt-in, and explicit proxy URLs cannot contain
   credentials.
5. Configure a search authorizer, durable write guard, plus tenant-aware
   read/write resolver. Use a
   separate lifecycle client or credentials, lifecycle authorizer, complete
   semantic verifier, application-owned cutover guard, durable cross-instance
   lifecycle mutation coordinator, cleanup eligibility guard, and encrypted
   reindex-cursor codec for index administration.
6. Keep `Info`, `Discover`, `Health`, `Capacity`, `ResilienceSnapshot`, and
   telemetry behind an operator authorization boundary. Prefer a separate
   least-privilege operational client; these surfaces aggregate the shared
   client or cluster and are not tenant-safe APIs.
7. Call `Close`; an owned transport closes idle connections, while a borrowed
   transport remains the caller's responsibility.

The adapter rotates configured endpoints, bounds in-flight work and optional
waiting, opens a process-local circuit after a configured sequence of transport
or overload failures, and performs exactly one downstream attempt. Discovery
is explicit rather than timer-driven. It atomically replaces the pool only when
every eligible publish address passes the DNS/CIDR trust policy.

## Retry and overload policy

The official client's node retry layer is not enabled. A caller retry consumes
the shared application resilience budget and must be bounded by operation
idempotency:

- reads, health, and capacity may retry within the remaining deadline from
  their authorized operator-only surface;
- an externally versioned write may retry only after classifying whether the
  previous outcome is known; reconcile an unknown outcome first;
- bulk retries operate on individually classified items, never the original
  undifferentiated batch;
- 429 and 503 are overload signals: reduce admission, apply capped jitter, and
  honor the shared attempt/time budget;
- cancellation, mapping rejection, version conflict, lifecycle denial, and
  malformed requests are not transient retry reasons.

Queue limits provide local backpressure, not distributed admission control.
Run one controller per process and size aggregate concurrency across replicas.

Telemetry contains operation, bounded outcome category, semantic signal,
status, duration, in-flight count, queued count, and circuit state only. It deliberately excludes
tenants, indexes, node IDs, queries, sources, credentials, and cursor contents.
See [observability](observability.md) for dashboards and alerts.

For migrations, create a versioned index, run and checkpoint reindex, reconcile
acknowledged concurrent writes, then use `CutoverAlias`. The configured guard
must drain and reject or durably buffer application writes while final complete
verification and the alias mutation run. Release writes only after cutover, and
atomically update every resolver's `IndexTarget.PhysicalName` to the new backing
generation inside that same fence before writers resume. Apply the inverse
resolver update during a fenced rollback. Retain the old index for a fenced,
reconciled rollback. Treat task IDs and PIT IDs as sensitive and bounded opaque
values.

Every instance capable of `CreateIndex`, `AddAlias`, `SwapAlias`,
`CutoverAlias`, or `CleanupIndex` must use the same durable
`LifecycleMutationGuard` for overlapping tenant resources. The coordinator is
held across OpenSearch network operations and must therefore be a renewable
lease, database advisory lock, or equivalent application-owned mechanism with
bounded cancellation—not a process-local Go lock. Cleanup additionally checks
exact generation identity, every alias, retained readers/PITs, retention, and
backup eligibility inside that exclusion.

Capacity tests must use the deployment's mappings, analyzers, shard layout,
replica count, query mix, aggregation cardinality, refresh policy, document
sizes, and concurrent PIT count.

## Shutdown

Stop admission, cancel callers, wait for owned projection/rebuild workers,
close or explicitly delete application-owned PIT cursors where possible, call
`Client.Close`, then close any borrowed transport and credential refresher in
their owning component. The adapter owns no background discovery or refresh
goroutine and no retry timer.
