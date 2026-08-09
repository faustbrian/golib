# Operations and capacity

Record request duration, backend status class, retries, rejected and unknown
bulk items, bytes, hit count, partial shards, PIT creation/cleanup, migration
state, reconciliation drift, and alias changes. Correlate without logging query
secrets, raw sources, credentials, PIT IDs, or signed cursors.

Set context deadlines below upstream request deadlines. Bound response bytes,
decode depth, page size, bulk items and bytes, concurrent requests, retry count,
total retry time, migration polling, and reconciliation pages. Capacity plans
must measure corpus size, shard count, replicas, ingestion rate, refresh rate,
query mix, analyzer cost, aggregation cardinality, PIT concurrency, latency,
allocations, and recovery headroom.

Runbooks must cover overload, rejected writes, unknown outcomes, partial shard
results, expired PITs, index blocks, migration pause/resume, alias rollback,
drift repair, credential rotation, and full rebuild.

Reconciliation rejects pages larger than requested and caps the complete scan
at the configured page-count and page-item product. The in-memory contract fake
uses the same product as an absolute document-capacity bound.
