// Package verkletree is the pre-v1 home of an explicitly profiled,
// storage-independent Verkle tree.
//
// The package currently exposes only the immutable identity and structural
// metadata of one package-owned experimental profile. It exposes no tree,
// root, proof, witness, or persistence API. Its internal cryptographic
// encoding boundary exists to evaluate a pinned commitment backend without
// making stability, production-readiness, or Ethereum-compatibility claims.
package verkletree
