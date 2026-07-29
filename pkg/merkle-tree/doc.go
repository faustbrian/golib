// Package merkletree constructs explicitly profiled cryptographic Merkle-tree
// roots, streams roots through a logarithmic-memory builder, incrementally
// appends ordered leaves into proof-retaining builders, creates immutable
// snapshots, and generates and independently verifies inclusion,
// multi-inclusion, and append-only consistency proofs. Roots and proofs have
// bounded, versioned canonical binary encodings. Immutable snapshots can be
// persisted, structurally validated, and resumed without raw leaf bytes.
//
// The package does not define a universal "Merkle tree" convention. Every
// root carries a profile, profile version, hash algorithm, and tree size.
// Merkle proofs establish a relationship to a trusted root; they do not
// establish that a leaf is true, fresh, authorized, or semantically valid.
package merkletree
