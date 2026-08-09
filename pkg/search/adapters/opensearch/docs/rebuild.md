# Migration, rebuild, replay, and reconciliation

Search is derived state. The application database and durable outbox/event log
remain authoritative.

## Generation migration

1. Define canonical settings, mappings, and analyzers and calculate the shared
   `IndexDefinition` fingerprint.
2. Create a new versioned physical index.
3. Run `Reindex`; persist its bounded task cursor and poll until complete.
4. Use `VerifyIndex` to compare source/target counts and run application-level
   sampled or full source-digest reconciliation.
5. Atomically `SwapAlias` to the new generation. Keep the old generation for a
   documented rollback window.
6. Roll back by swapping the alias back. Delete the rejected/new generation
   only after writers and readers have converged.
7. Delete the old generation after the observation window and backup policy
   permit it.

## Full rebuild from source of truth

Create an empty generation, capture a source-of-truth high-water mark, bulk
project the complete source in stable key order, then replay outbox events after
that mark. External versions make replay monotonic and expose conflicts. Do not
blindly retry unknown bulk items: reconcile ID/version first. Verify counts and
content digests before alias cutover.

The core `search.ProjectionConsumer` owns event application; the application
owns durable replay checkpoints. `Client.Read` implements the reconciliation reader with a
PIT and `_id` order; it refuses partial shard results and hashes source bytes
locally. Reconciliation should record only bounded IDs, versions, and digests,
not complete source documents or tenant labels in telemetry.

Rebuild and reconciliation workers must have explicit contexts, concurrency,
bulk byte/item limits, durable checkpoints, and shutdown ownership. A failed
run leaves the current alias untouched.
