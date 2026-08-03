// Package verkletree is the pre-v1 home of an explicitly profiled,
// storage-independent Verkle tree.
//
// The package exposes one package-owned experimental profile, immutable
// snapshots and roots, canonical atomic updates, and bounded aggregate
// membership and non-membership proofs. Every expensive operation requires a
// context and explicit resource limits. Snapshots can produce canonical
// content-addressed node batches for capability-checked atomic publication and
// reconstruct snapshots from capability-checked isolated reads after verifying
// every reachable node, root, and content address. A bounded audit can compare
// the complete canonical node inventory with all verified current and retained
// roots without mutating storage. A separate capability-checked operation can
// atomically replace the retained-publication set and prune only nodes outside
// the current and desired retained roots. Canonical stateless Set witnesses can
// bind an exact update/claim key set, verify its authenticated pre-state, and
// independently derive and match the claimed post-state root for authenticated
// present, missing, or different stem paths. Stateless deletion and topology
// collapse, crash repair, concrete storage adapters, and stable-profile APIs
// remain unavailable.
//
// The exported API is experimental and exists to evaluate a pinned commitment
// backend and complete tree semantics without making stability,
// production-readiness, or Ethereum-compatibility claims.
package verkletree
