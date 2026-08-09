# Capacity, dashboards, and alerts

`Health` is a readiness view. Green is healthy; yellow may be ready when
primaries are active and no shard initialization or timeout is present; red is
not ready. `Capacity` aggregates bounded cluster/node statistics without
retaining node IDs.

Build dashboards from the adapter's low-cardinality telemetry and the returned
health/capacity reports:

- request rate, latency percentiles, and HTTP/outcome category by operation;
- in-flight, queued, backpressure rejection, and circuit-open state;
- 429/503 rate, transport failures, malformed responses, partial/timed-out
  searches, shard failures, bulk unknown/rejected/conflict counts;
- active/initializing/unassigned shards, pending tasks, heap, disk available,
  store bytes, thread-pool rejection totals, and breaker trip totals;
- PIT cleanup/expiry, discovery rejection, reindex duration/failure, alias
  cutover, and reconciliation drift.

Alert on sustained overload, any red/not-ready report, low disk headroom,
initializing/unassigned primaries, increasing breaker trips, circuit-open time,
unknown write outcomes, repeated PIT cleanup failure, or non-zero rebuild drift.
Choose thresholds from load tests using the production mapping, shard topology,
document/query mix, refresh policy, and replica count. Do not add tenant, index,
query, document, node, task, or cursor labels to telemetry.
