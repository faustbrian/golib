// Package mpt implements Ethereum's execution-layer modified Merkle Patricia
// trie. The core package is storage independent and distinguishes raw,
// Keccak-secured, and Ethereum protocol key profiles.
//
// Proof verification establishes a value or absence claim under a caller
// supplied root. It does not establish that the root is canonical, finalized,
// recent, or authorized.
package mpt
