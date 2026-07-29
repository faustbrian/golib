# Merkle structure selection

These structures share hashing terminology but are not interchangeable.

| Structure | Addressing and shape | Typical proof | This package |
|---|---|---|---|
| Ordered binary Merkle tree | leaves by sequence index; recursively split binary tree | inclusion and append-only consistency | canonical v1 and RFC 9162 v1 |
| Sparse Merkle tree | key bits select a path in a fixed-depth tree with defined empty nodes | membership and non-membership | not implemented |
| Merkle Patricia trie | radix paths plus Patricia compression and typed nodes | key/value trie path | not implemented |
| SSZ merkleization | type-directed 32-byte chunks, generalized indexes, zero hashes, mix-in length | type-aware branch | not implemented |
| Verkle tree | wide vector commitments with cryptographic opening proofs | key/value opening | not implemented |

Ethereum execution-layer state uses a modified Merkle Patricia trie with
nibble paths, branch/extension/leaf nodes, hex-prefix compact paths, RLP,
Keccak-256, and inline-versus-hashed child references. A binary Merkle root
with Keccak substituted for SHA-256 is not Ethereum MPT compatible.

Ethereum consensus-layer SSZ uses type-driven chunking, generalized indexes,
zero-padding rules, and mix-in length. A generic binary tree over serialized
values is not SSZ compatible.

Sparse trees require an exact key encoding, depth, empty-node derivation, and
membership/non-membership proof contract. Verkle trees require a selected
vector-commitment scheme and opening-proof transcript. Those belong in
separately specified packages or profiles with their own fixtures and
differential evidence.
