# Storage, Recovery, and Pruning Guide

## Responsibility boundary

The root package defines interfaces and verifies package-owned data. It does
not provide a database, filesystem, or object-storage adapter. An adapter must
not claim stronger atomicity, durability, isolation, retention, or recovery
guarantees than it has proven independently.

Use one storage namespace per profile. A maintenance adapter must return that
profile from `MaintenanceProfile`; mixing profiles in one inventory namespace
is outside the deletion safety contract.

## Publish a snapshot

`Snapshot.Commit` requires immutable-node, atomic-commit,
durable-publication, and compare-and-swap capabilities. The adapter receives
one `StoreCommit` containing:

- the exact prior-root expectation, or an explicit expectation of no root;
- the new profile-bound root and canonical root-node content address; and
- the complete canonical node image ordered by content address.

The adapter must make all nodes durable before publishing the root and must
reject a stale prior-root expectation atomically. A successful return is the
adapter's durability assertion; the package cannot prove that the transaction
or storage device honored it.

Persist the root container and root-node `NodeID` together. After restart,
strictly decode the root, use `NewStorePublication` to reconstruct the opaque
pair, and use `LoadSnapshot` to verify the pair and every reachable node. Do not
treat successful `NewStorePublication` construction as proof that the root-node
address is correct.

## Read isolation and lifecycle

`NodeReader.OpenSnapshot` must return one view fixed for its complete lifetime.
Every publication and node read receives the operation context. `ReadNode`
also receives an explicit byte bound that the adapter must enforce before I/O
or allocation. Returned node bytes transfer to the caller and must remain
stable. `Close` is called exactly once after a successful open, including after
read or verification failure.

Deletion must not invalidate a read or audit view opened earlier. An adapter
may defer physical reclamation until all older views close.

## Audit without mutation

`AuditStorage` opens one isolated `NodeAuditSnapshot` spanning the optional
current publication, all retained publications, and the complete node-ID
inventory. It fully reconstructs every publication before classifying nodes.
The resulting `StorageAudit` reports canonical unreachable identifiers but is
not deletion authority.

Use audits for monitoring and investigation. Treat any missing reachable ID,
malformed page, invalid publication, corrupt reachable node, cancellation, or
close failure as an incomplete audit. Do not delete from a cached audit result.

## Change retention and prune

`MaintainStorage` independently opens and verifies a fresh isolated audit view.
The requested retained publications are a set and must be an exact subset of
the observed retained set. The current publication is always preserved.

After verification, the package closes the view and sends one opaque
`StoreMaintenance` to `ApplyMaintenance`. The adapter must atomically:

1. compare the exact current and complete previous retained set;
2. install the desired canonical retained subset; and
3. delete exactly the supplied ascending node IDs.

A mismatch or failure must leave publications and nodes unchanged. The call is
made even for a logical no-op so the comparison remains the linearization
point.

## Recover interrupted unpublished writes

`RecoverStorage` is the bounded core recovery operation. It verifies every
current and retained publication, preserves that exact publication set, and
deletes only inventoried nodes unreachable from all of them. This safely
collects node-only debris left when a commit wrote nodes but never published
its root.

Recovery does not restore a missing or corrupt node reachable from a published
root. It fails closed on that state, incomplete inventory, stale publications,
cancellation, lifecycle failure, or adapter failure. Restoring published data
requires adapter-specific replicas, backups, or another independently verified
source.

## Crash matrix for adapters

An adapter claiming production durability should inject process termination at
least at these boundaries:

| Boundary | Required observable state after restart |
| --- | --- |
| Before any node write | Old publication and nodes unchanged |
| During node writes | Old publication readable; debris collectable by recovery |
| After nodes, before root publication | Old publication readable; new nodes unpublished |
| During root compare-and-swap | Exactly old or new complete publication, never mixed |
| After root publication | New root and every reachable node durable |
| During retention replacement | Complete old or complete new retained set |
| During deletion | Publication comparison and deletion both committed or both absent |
| During recovery | Publication set unchanged; deletion wholly committed or absent |

Also test stale compare-and-swap, repeated retry, close failure, cancellation,
concurrent readers, retained-root pruning, inventory pagination, and deferred
reclamation. Root-package tests prove the generic contract and hostile-input
planning; they do not prove a concrete adapter or storage engine.

## Retention and rebuild policy

Define retention outside the tree package. Record which roots are protected,
when a root may be released, and how readers pin a view. Run a read-only audit
before operational diagnosis, then call maintenance with the exact desired
retained subset. Use bounded inventory iteration for rebuild tooling and verify
every reconstructed publication through `LoadSnapshot`.

Never infer reachability from content hashes alone and never substitute a
Merkle hash walk for the profile's vector-committed root verification.

## Limits and observability

Size `StorageLimits`, `StorageReadLimits`, and `StorageAuditLimits` from measured
node counts and encoded sizes. Audit and maintenance limits cover publication
copies, reachable maps, inventory pages, unreachable or deleted results, node
reads, point decodes, and temporary memory. Limits are rejection boundaries,
not capacity targets.

Expose sanitized counters for operation kind, duration, node reads, inventory
pages, result counts, cancellation, resource category, stale comparison, and
adapter error class. Do not log roots together with sensitive application
metadata, complete node bytes, keys, values, proofs, or witnesses.
