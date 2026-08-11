# Hardening and real-cluster exercises

The conformance harness runs the complete adapter workflow against exact
OpenSearch `2.19.6` and `3.8.0` containers. Each container is limited to one
CPU, 1 GiB of memory, 512 processes, and 1,024 file descriptors. The harness
creates no connection to a pre-existing service and removes its containers and
newly pulled images on exit.

The real-cluster contract covers direct official-client differential search,
externally versioned writes, exact bulk conflict and mapping-rejection
outcomes, PIT/search-after traversal and PIT loss, health, capacity, templates,
reindex, verification, alias cutover and rollback, reconciliation, and bounded
concurrent load. `OPENSEARCH_SOAK_DURATION` extends the load phase up to five
minutes per supported version for a release soak.
The load fixture also fails above a 10-second search-plus-write cycle, four
client CPU cores plus a two-second allowance, 64 MiB of peak Go-heap growth,
1 GiB process peak RSS, or 32 MiB plus 512 KiB per completed cycle of aggregate
HTTP request/response bodies. Network accounting is application payload bytes;
TCP, TLS, and link framing remain deployment-level measurements. The fixture
publishes completed cycles and wall duration so these are strict bounds rather
than health-only observations.

The interoperability harness creates a two-data-node cluster, proves endpoint
rotation and one-attempt failover, then replaces one node at a time from
`2.19.6` to `3.8.0`. It exercises the mixed-version cluster between
replacements and reruns conformance and multi-node checks after the upgrade.
Each multi-node container retains the same CPU, memory, and process limits but
uses OpenSearch's required 65,536-file-descriptor minimum; the harness asserts
that configured ceiling and live descriptor use independently.

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

`make benchmark-integration` records ten independent samples by default for
the fake, adapter, and direct official client over the same 128-document corpus,
external-version writes, exact ordered query results, and pagination sequence.
Release evidence retains the raw `-benchmem` output, Go/OpenSearch versions,
host CPU and memory, mapping/shard/refresh configuration, and a pinned
`benchstat` comparison under the ignored module-scoped `.artifacts` directory;
`OPENSEARCH_BENCHMARK_EVIDENCE_DIR` can select another owned destination. A
single sample or comparison with different result semantics is not publishable
benchmark evidence.

The version matrix also builds the frozen `testdata/mixedappv1` wire-protocol
peer and overlaps it with the current adapter against one real alias. Both
processes issue attributed external-version writes and ordered reads; this
proves the declared application wire handoff without claiming that an
unreleased historical adapter binary was exercised.

The TLS security matrix is a real backend authorization test, not only a
credential-rotation exercise. On both supported versions it permits tenant-A
adapter writes and searches, denies tenant-B reads and writes, denies cluster
and Security administration to runtime credentials, and uses a separate
cluster-monitoring operator for health, capacity, DNS change, and recovery.

Snapshot creation and restoration remain deployment-owned operations because
the adapter has no snapshot API or repository credentials. The upgrade runbook
requires an independently restored search snapshot and a tested rebuild from
the authoritative database and durable outbox before production rollout.
