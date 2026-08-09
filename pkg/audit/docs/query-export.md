# Query and export

Every query requires exact tenant, explicitly absent tenant, or all tenants.
This selection conveys no read authorization. Filters cover inclusive recording
time, actor, subject type and ID, action, correlation, and outcome. Limits are
mandatory and at most 1,000.

Ordering is always `recorded_at, record_id`; cursors are exclusive, versioned,
URL-safe encodings of that pair. Reuse the returned cursor without modification.
Export calls the consumer in the same stable order, stops at the query limit,
honors cancellation, and never runs the consumer under the memory adapter lock
or a PostgreSQL transaction lock. Verify canonical bytes, chain links,
checkpoints, counts, and final anchors independently after export.
