# Goal Harden: OpenSearch Adapter

## Mission

Prove the OpenSearch adapter against real clusters, hostile responses, overload,
partial failures, migrations, failover, version skew, and production resource
limits.

## Required Audit

1. Refresh the supported OpenSearch/client matrix, release notes, security
   advisories, API compatibility, and official operational guidance.
2. Inventory transport defaults, retries, node selection, authentication,
   serializers, query translations, bulk parsing, pagination, lifecycle APIs,
   metrics, and shutdown.
3. Differentially verify generated DSL and result translation against direct
   official-client requests for equivalent behavior.
4. Exercise every bulk item status, shard partial failure, timeout, connection
   reset, malformed body, oversized body, point-in-time expiry, version
   conflict, cluster block, and ambiguous write outcome.
5. Verify retries, rate limits, breakers, bulkheads, concurrency, and official
   client behavior cannot amplify work or exceed one shared budget.
6. Run multi-node failover, rolling upgrade, credential rotation, TLS failure,
   DNS change, cluster recovery, snapshot/restore boundary, and degraded health.
7. Prove mapping migration, reindex, alias cutover, rollback, concurrent writers,
   old/new application compatibility, reconciliation, and complete rebuild.
8. Attack tenant scope, signed cursors, raw DSL, field filtering, endpoints,
   diagnostics, traces, and credentials.
9. Run race, fuzz, leak, stress, soak, high-cardinality, and strict container
   CPU/memory/file-descriptor tests.
10. Validate dashboards, alerts, runbooks, capacity limits, and incident drills.

## Required Evidence

- real-cluster conformance across supported versions;
- exact 100% meaningful statement coverage and 100% viable mutation kills;
- outage, failover, rolling-upgrade, migration, rollback, and rebuild reports;
- race, fuzz, leak, stress, soak, and resource-bound results;
- equivalent benchmarks versus direct official-client usage;
- security, AWS deployment, operations, docs, and clean-consumer proof.
