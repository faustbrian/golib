# Goal: OpenSearch Adapter

## Objective

Implement the production OpenSearch adapter for `search` using the maintained
official Go client where it meets requirements. Pin the supported OpenSearch
and client versions at implementation time and inventory every used API and
compatibility boundary.

Authoritative starting points:

- https://docs.opensearch.org/latest/clients/go/
- https://github.com/opensearch-project/opensearch-go
- https://docs.opensearch.org/latest/api-reference/

## Required Capabilities

- endpoint discovery/configuration, TLS, basic authentication, AWS signing,
  credential rotation, proxy policy, and safe transport ownership;
- single and bulk index/update/upsert/delete with external versioning and
  exact per-item outcome classification;
- translation of every shared query, filter, sort, source, highlight,
  aggregation, suggestion, and geo capability claimed by `search`;
- point-in-time plus search-after pagination with bounded signed cursors;
- mappings, settings, analyzers, aliases, templates where needed, index
  creation, reindexing, verification, cutover, rollback, and cleanup;
- health, readiness, capacity, throttling, circuit breaking, backpressure,
  telemetry, and actionable diagnostics;
- outbox/event projection, replay, reconciliation, and full rebuild workflows.

Raw OpenSearch DSL MAY be available only through an explicit escape hatch with
clear injection, portability, validation, and stability caveats.

## Failure And Resilience Contract

Classify transport errors, cluster blocks, shard failures, partial results,
bulk item failures, 429/503 overload, version conflicts, mapping rejection,
point-in-time expiry, malformed responses, cancellation, and unknown outcomes.
Retries MUST be bounded by operation idempotency and the shared resilience
budget. Node retries in the official client MUST not multiply package retries.

Response bodies, bulk buffers, point-in-time handles, idle connections, timers,
and credential refresh MUST have explicit ownership and shutdown.

## Security And Tenancy

Support least-privilege credentials and AWS-managed OpenSearch deployment
patterns without hardwiring deployment topology. Prevent credential forwarding,
unsafe endpoints, query leakage, field disclosure, raw tenant labels, and
cross-tenant alias/index/cursor confusion.

## Verification

Run the shared adapter conformance suite against every supported OpenSearch
version. Test multi-node behavior, partial shard/bulk failures, failover,
rolling upgrades, throttling, point-in-time loss, malformed responses, TLS and
credential rotation, index migration, rebuild, and reconciliation. Exact 100%
statement coverage and 100% viable mutation kills are REQUIRED.

## Documentation And Delivery

Document deployment, AWS authentication, index design, mappings, aliases,
pagination, retries, capacity, dashboards, upgrades, backup/snapshot boundaries,
rebuild, troubleshooting, and complete examples. Add module manifests, CI,
benchmarks, changelog, and clean-consumer proof.
