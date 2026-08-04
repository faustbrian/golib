// Package verkletree is the pre-v1 home of an explicitly profiled,
// storage-independent Verkle tree.
//
// The package exposes one package-owned pre-v1 profile, immutable
// snapshots and roots, canonical whole-snapshot bytes, canonical atomic
// updates, and bounded aggregate membership and non-membership proofs. Every
// expensive operation requires a
// context and explicit resource limits. Snapshots can produce canonical
// content-addressed node batches for capability-checked atomic publication and
// reconstruct snapshots from capability-checked isolated reads after verifying
// every reachable node, root, and content address. A bounded audit can compare
// the complete canonical node inventory with all verified current and retained
// roots without mutating storage. A separate capability-checked operation can
// atomically replace the retained-publication set and prune only nodes outside
// the current and desired retained roots. Bounded recovery preserves every
// verified publication while atomically pruning node-only debris left by an
// interrupted unpublished write. Canonical stateless witnesses can
// verify authenticated Set operations on present, missing, or different stem
// paths and Delete operations that are absent, leave a stem non-empty, or
// remove stems and canonically collapse authenticated unary paths, then
// independently derive and match the claimed post-state root. Restoration of
// missing or corrupt published state, concrete storage adapters, and stable-v1
// API guarantees remain unavailable.
//
// The exported API is pre-v1 and exists to evaluate a pinned commitment
// backend and complete tree semantics. Profile conformance does not imply
// stable-v1 API compatibility, production suitability, external audit, or
// Ethereum protocol compatibility.
package verkletree
