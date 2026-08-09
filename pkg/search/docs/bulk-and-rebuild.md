# Bulk ingestion and rebuilds

Bound bulk operations by item count and encoded bytes. Each index, update,
upsert, or delete uses a stable document ID and explicit external version.
Core byte admission uses remaining-budget arithmetic, so hostile configured
sizes cannot wrap an aggregate counter.
Preserve input ordering only as metadata; do not infer cross-document backend
ordering.

Inspect every item result. Retry only retryable rejected items, using the same
version, within an application retry/time budget. Split overload retries and
add jitter so partial failures do not amplify load. Unknown items require
reconciliation. Conflicts usually mean a newer projection already won.

For a rebuild, create a new physical index and replay the source of truth or
durable event history. Verify it, cut the alias atomically, retain rollback,
then delete the old index after the documented window.
