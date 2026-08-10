# Upgrades and backup boundaries

The release matrix proves exact OpenSearch versions `2.19.6` and `3.8.0` with
official Go client `v4.7.3`. Treat any other patch, plugin, analyzer, or managed
service profile as unverified until conformance is rerun.

Before an OpenSearch upgrade:

1. read the server breaking-change and rolling-upgrade guidance for every hop;
2. snapshot according to the deployment provider's supported repository and
   restore procedure, and test restoration independently;
3. verify plugin/analyzer availability and mapping compatibility;
4. run conformance, workload benchmarks, failover, partial-shard/bulk, PIT-loss,
   TLS/auth rotation, migration, rebuild, and reconciliation tests in a
   disposable environment;
5. drain or bound long-lived PIT cursors and pause alias cutovers;
6. roll data nodes and cluster-manager nodes in the provider-required order,
   verifying health and adapter operations after each replacement;
7. resume projections, reconcile from the durable checkpoint, and monitor the
   dashboards before completing the rollout.

An OpenSearch snapshot is not the source-of-truth backup and this adapter does
not create, retain, encrypt, or restore snapshots. The application database and
durable outbox need their own backup/restore contract. A search snapshot can
reduce recovery time, but a full rebuild from authoritative data must remain
tested. Alias rollback handles a bad index generation; it does not downgrade a
server binary or replace a tested restore plan.

The compatibility review was refreshed on 2026-08-10 against the official
OpenSearch 2.19.6 and 3.8.0 release notes, the `opensearch-go` v4.7.3 release,
and the current rolling-upgrade guide. OpenSearch 2.19.6 includes bulk-hang and
circuit-breaker fixes. OpenSearch 3.8.0 includes timeout status, malformed
mapping, deserialization-depth, path-boundary, and dependency security fixes.
The adapter does not adopt new 3.8-only APIs, so its HTTP surface remains in the
2.19/3.8 common subset.

Primary references:

- <https://github.com/opensearch-project/OpenSearch/releases/tag/2.19.6>
- <https://github.com/opensearch-project/OpenSearch/releases/tag/3.8.0>
- <https://github.com/opensearch-project/opensearch-go/releases/tag/v4.7.3>
- <https://docs.opensearch.org/latest/migrate-or-upgrade/rolling-upgrade/>
