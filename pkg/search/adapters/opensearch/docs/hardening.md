# Hardening and real-cluster exercises

The conformance harness runs the complete adapter workflow against exact
OpenSearch `2.19.3` and `3.6.0` containers. Each container is limited to one
CPU, 1 GiB of memory, 512 processes, and 1,024 file descriptors. The harness
creates no connection to a pre-existing service and removes its containers and
newly pulled images on exit.

The real-cluster contract covers direct official-client differential search,
externally versioned writes, exact bulk conflict and mapping-rejection
outcomes, PIT/search-after traversal and PIT loss, health, capacity, templates,
reindex, verification, alias cutover and rollback, reconciliation, and bounded
concurrent load. `OPENSEARCH_SOAK_DURATION` extends the load phase up to five
minutes per supported version for a release soak.

The interoperability harness creates a two-data-node cluster, proves endpoint
rotation and one-attempt failover, then replaces one node at a time from
`2.19.3` to `3.6.0`. It exercises the mixed-version cluster between
replacements and reruns conformance and multi-node checks after the upgrade.

Deterministic transport tests inject cancellation, timeouts, connection
errors, truncated and oversized bodies, malformed JSON, partial shards, every
bulk outcome class, 429/503 overload, mapping rejection, version conflicts,
cluster blocks, credential rotation, TLS policy failures, discovery changes,
circuit transitions, queue pressure, observer failures, and cleanup errors.
Race, repeated stress, fuzz, exact coverage, mutation, benchmarks, security,
API, documentation, and clean-consumer gates run independently of Docker.
Configuration validation also places absolute caps on in-flight admission,
queued callers, locale analyzers, and discovery trust rules before allocating
channels or cloning configuration collections.

Snapshot creation and restoration remain deployment-owned operations because
the adapter has no snapshot API or repository credentials. The upgrade runbook
requires an independently restored search snapshot and a tested rebuild from
the authoritative database and durable outbox before production rollout.
