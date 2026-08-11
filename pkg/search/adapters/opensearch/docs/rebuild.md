# Migration, rebuild, replay, and reconciliation

Search is derived state. The application database and durable outbox/event log
remain authoritative.

## Generation migration

1. Define canonical settings, mappings, and analyzers and calculate the shared
   `IndexDefinition` fingerprint.
2. Create a new versioned physical index.
   Configure one durable `LifecycleMutationGuard` shared by every application
   instance that can create generations, change aliases, cut over, or clean up.
   The adapter rejects those mutations when the coordinator is absent.
3. Run `Reindex`; persist its AES-256-GCM-encrypted, expiring,
   tenant/source/target-bound cursor and atomically replace it after every
   incomplete poll, because polling renews the bounded continuation lease. Keep
   writers active and record every acknowledged write in the durable replay
   stream.
4. Use `VerifyIndex` with distinct physical source and target generations and
   the expected target definition fingerprint. The
   configured semantic verifier must compare every source/target ID, external
   version, and canonical source digest from a stable point-in-time view within
   a hard traversal bound and must attest current live mappings/settings.
   Sampling and count equality cannot authorize cutover.
5. Reconcile every write acknowledged during reindex. Then call
   `CutoverAlias`, whose application-owned `LifecycleCutoverGuard` must stop
   new writes and drain in-flight writes while the adapter repeats complete
   semantic verification and atomically changes the alias. A Go mutex is not a
   valid fence because verification performs network I/O; use an ingress gate,
   durable buffer, or equivalent application quiescence mechanism.
6. Release queued writes only after `CutoverAlias` returns; they must resolve
   through the new alias. Keep the old generation for a documented rollback
   window.
   If alias mutation succeeds but the application migration checkpoint fails,
   do not infer completion from the moved alias. Resolve the mutation outcome,
   reconcile durable writes, and repair the application-owned migration state
   before resuming.
7. Before rollback, fence writers and reconcile writes acknowledged on the new
   generation back to the retained generation, then use `CutoverAlias` in the
   reverse direction. Delete the rejected generation only after writers and
   readers have converged. `SwapAlias` is a low-level atomic mutation for
   bootstrap or an already externally fenced recovery; it does not verify data
   and must not be used as the migration cutover protocol.
8. Delete the old generation after the observation window and backup policy
   permit it. Bind `CleanupIndex` to a logical alias distinct from both the
   active and inactive physical generations. It holds the same lifecycle mutation coordinator
   across the application-owned final `LifecycleCleanupGuard` checks and the
   backend deletion, so a competing alias or name-reuse mutation cannot enter
   between eligibility verification and deletion.

## Full rebuild from source of truth

In one authoritative-store transaction, capture a repeatable snapshot identity
and the commit-ordered durable outbox cursor covered by that snapshot. Persist
both before work begins. Create an empty generation, scan only that immutable
snapshot in stable key order with durable page checkpoints, then replay outbox
commits strictly after the captured cursor with a separate durable checkpoint.
An interrupted run must resume the same snapshot/cursor boundary or restart a
new generation; it must not combine a new source view with an old cursor.
External versions make replay monotonic and expose conflicts. Do not blindly
retry unknown bulk items: reconcile ID/version first. Verify the live definition,
complete IDs, versions, and canonical content digests and require a second
zero-drift reconciliation pass before alias cutover.

The core `search.ProjectionConsumer` owns event application; the application
owns durable replay checkpoints. `Client.Read` implements the reconciliation reader with a
PIT and `_id` order; it refuses partial shard results and hashes source bytes
locally. Reconciliation should record only bounded IDs, versions, and digests,
not complete source documents or tenant labels in telemetry.

Rebuild and reconciliation workers must have explicit contexts, concurrency,
bulk byte/item limits, durable checkpoints, and shutdown ownership. A failed
run leaves the current alias untouched.

Every rebuild or replay write must also pass the adapter `WriteGuard` against
the application-owned current document or tombstone. External versioning alone
is insufficient: OpenSearch removes delete-version tombstones after
`index.gc_deletes`, after which an older replay can recreate the backend
document. The durable source guard must continue rejecting that stale replay
independently of backend tombstone retention.

The OpenSearch reconciliation reader can report an index-only document, but
its absence from a separately read source snapshot is not deletion authority.
`search.NewReconciler` refuses orphan repair before sending a bulk request.
Production orphan cleanup must use `search.NewReconcilerWithDeletionGuard` with
an application-owned source transaction that confirms deletion and durably
reserves a monotonic tombstone version. The guard is not an OpenSearch adapter
responsibility and must fence concurrent source recreation for the same
tenant, logical index, and document ID.

Before deleting any generation, obtain a fresh cleanup authorization and verify
the exact physical name and fingerprint, enumerate every read/write alias and
reject any remaining reference, drain old application versions and owned PITs,
confirm the rollback/backup retention window has elapsed, repeat forward
verification and zero-drift reconciliation, then delete inside the migration
coordinator. Name reuse is forbidden until the cleaning checkpoint is complete.
