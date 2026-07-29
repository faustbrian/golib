// Package statemodel implements the bounded, cryptography-independent state
// transition oracle for the package-owned experimental Verkle profile.
//
// It deliberately computes no root or commitment. Future committed-tree code
// can be differentially tested against this slow immutable model without
// substituting hashes for vector commitments.
package statemodel
