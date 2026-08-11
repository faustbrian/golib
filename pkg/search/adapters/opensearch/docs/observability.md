# Capacity, dashboards, and alerts

`Health` is a readiness view. Green is healthy; yellow may be ready when
primaries are active and no shard initialization or timeout is present; red is
not ready. `Capacity` aggregates bounded cluster/node statistics without
retaining node IDs. Both reports, `Info`, `Discover`, `ResilienceSnapshot`, and
configured telemetry are operator-wide rather than tenant-scoped. They must be
collected through a separately authorized operational client or endpoint and
must never be exposed to a tenant merely because their labels omit tenant and
index names.

The versioned deployment-neutral dashboard and alert contracts are
[`operations/dashboard.json`](../operations/dashboard.json) and
[`operations/alerts.json`](../operations/alerts.json). Exporters must map the
listed signals from their declared source: the adapter's low-cardinality
telemetry, returned health/capacity reports, or the application-owned core
reconciliation report. Exporters must not add any forbidden label. The executable
validator rejects missing production signals, alert/runbook gaps, invalid
durations, unsupported telemetry signal names, and forbidden dashboard grouping.

The dashboard covers:

- request rate, latency percentiles, and HTTP/outcome category by operation;
- in-flight, queued, backpressure rejection, and circuit-open state;
- 429/503 rate, transport failures, malformed responses, partial/timed-out
  searches, shard failures, bulk unknown/rejected/conflict counts;
- active/initializing/unassigned shards, pending tasks, heap, disk available,
  store bytes, thread-pool rejection totals, and breaker trip totals;
- PIT cleanup/expiry and alias cutover signals emitted by adapter telemetry;
- reconciliation drift derived explicitly from `search.ReconciliationReport`
  by the application running the reconciler. It is not fabricated from an
  OpenSearch request-success event.

Alert on sustained overload, any red/not-ready report, low disk headroom,
initializing/unassigned primaries, increasing breaker trips, circuit-open time,
unknown write outcomes, repeated PIT cleanup failure, or non-zero rebuild drift.
Choose thresholds from load tests using the production mapping, shard topology,
document/query mix, refresh policy, and replica count. Do not add tenant, index,
query, document, node, task, or cursor labels to telemetry.

The incident response contract is
[`operations/runbook.md`](../operations/runbook.md). The deterministic drill
manifest in [`operations/incident-drills.json`](../operations/incident-drills.json)
is executed by `TestOperationalArtifactsAndIncidentDrills` for overload,
cluster loss, ambiguous writes, PIT expiry, and fenced migration rollback.
