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

For a rebuild, atomically capture a transactionally consistent source snapshot
identity and a commit-ordered durable outbox cursor in the authoritative store.
Persist both before scanning. Create a new physical index, scan the immutable
snapshot in stable key order, then replay only outbox commits after the captured
cursor. Persist scan and replay checkpoints so interruption resumes the same
snapshot instead of mixing time boundaries. Verify the complete definition,
IDs, versions, and canonical source digests; reconcile to zero drift; hold the
application write fence across a fresh verification and `CutoverAlias`; retain
the rollback generation; and resolve any ambiguous alias/checkpoint outcome
before proceeding. Delete the old index only after the retention/backup window,
all old-version readers and PITs are drained, no alias references it, and a
fresh zero-drift check passes.
