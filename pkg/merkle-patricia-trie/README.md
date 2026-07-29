# merkle-patricia-trie

`merkle-patricia-trie` is an independent Go implementation of Ethereum's
execution-layer modified Merkle Patricia trie. Its root package is `mpt`.

The module is under active pre-v1 development. Compatibility claims are made
only for surfaces backed by the pinned sources and executable evidence listed
in [source provenance](docs/source-provenance.md).

## Intended guarantees

- canonical nibble and hex-prefix compact paths;
- canonical Recursive Length Prefix encoding;
- legacy Keccak-256 node commitments;
- exact embedded-versus-hashed child references;
- immutable raw and secure trie snapshots;
- deterministic updates, deletions, roots, proofs, and iteration;
- caller-owned storage with integrity checks and atomic publication contracts;
- bounded hostile-input handling; and
- fixture and differential interoperability evidence.

The package does not implement an EVM, blockchain, network, JSON-RPC server,
binary Merkle tree, SSZ merkleization, or Verkle tree.

## Status

The compatibility foundation is being implemented in the delivery phases
described in [architecture](docs/architecture.md). No release or Ethereum
compatibility claim should be inferred from package presence alone.

Licensed under Apache-2.0.
