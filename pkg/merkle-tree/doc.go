// Package merkletree constructs explicitly profiled cryptographic Merkle-tree
// roots, incrementally appends ordered leaves into caller-owned builders,
// creates immutable snapshots, and generates and independently verifies
// inclusion proofs.
//
// The package does not define a universal "Merkle tree" convention. Every
// root carries a profile, profile version, hash algorithm, and tree size.
// Merkle proofs establish a relationship to a trusted root; they do not
// establish that a leaf is true, fresh, authorized, or semantically valid.
package merkletree
