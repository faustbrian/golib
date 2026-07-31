// Package verkletree is the pre-v1 home of an explicitly profiled,
// storage-independent Verkle tree.
//
// The package exposes one package-owned experimental profile, immutable
// snapshots and roots, canonical atomic updates, and bounded aggregate
// membership and non-membership proofs. Every expensive operation requires a
// context and explicit resource limits. Snapshots can produce canonical
// content-addressed node batches for capability-checked atomic publication.
// Persisted reads, witnesses, recovery, pruning, and stable-profile APIs remain
// unavailable.
//
// The exported API is experimental and exists to evaluate a pinned commitment
// backend and complete tree semantics without making stability,
// production-readiness, or Ethereum-compatibility claims.
package verkletree
