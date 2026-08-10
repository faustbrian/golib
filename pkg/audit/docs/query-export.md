# Query and export

Every query requires exact tenant, explicitly absent tenant, or all tenants.
This selection conveys no read authorization. Filters cover record ID,
inclusive recording time, actor, subject type and ID, action, correlation, and
outcome. Limits are mandatory and at most 1,000.

Ordering is always `recorded_at, record_id`; cursors are exclusive, versioned,
URL-safe encodings of that pair. Version 2 also carries an adapter-owned
acceptance watermark so records accepted after page one cannot move into the
same pagination snapshot, including later accepted backdated records. Version
1 remains parseable as a live cursor. Snapshot watermarks are bounded by the
PostgreSQL signed-bigint acceptance-order ceiling. Reuse returned cursors
without modification.
Export calls the consumer in the same stable order, stops at the query limit,
honors cancellation, and never runs the consumer under the memory adapter gate
or a PostgreSQL transaction lock. Adapter and consumer errors are sanitized;
only cancellation and deadline sentinels remain public. Verify canonical bytes,
chain links, checkpoints, counts, and final anchors independently after export.
