# Changelog

All notable changes follow Keep a Changelog and Semantic Versioning.

## [Unreleased]

### Added

- Established the Ethereum MPT module boundary, authoritative-source pins,
  compatibility decisions, and hostile-input threat model.
- Added canonical hex-prefix paths, bounded canonical RLP, legacy Keccak root
  commitments, canonical node encoding and validation, and exact embedded
  versus hashed child references.
- Added immutable raw and secure trie snapshots with bounded lookup, update,
  replacement, deletion, canonical compaction, and history-independent roots.
- Added atomic batch mutation, ordered raw and hashed-key iteration, lazy
  hash-addressed loading, integrity-checked atomic commits, and a concurrent
  in-memory store with stale-root protection.
- Added optional deterministic bounded node-store iteration for audits and
  rebuild tooling.
- Added root-verified raw and secure rebuilds that fully materialize snapshots
  for safe migration between stores.
- Added bounded immutable missing-node recovery overlays that validate fetched
  nodes, resume every traversal surface, and atomically repair the source store.
- Added bounded Ethereum-style membership and non-membership proof generation
  and verification with strict root, key, value, profile, ordering, and surplus
  node binding.
- Added deterministic raw and secure multi-key proofs with shared-node
  deduplication and mixed membership/absence verification.
- Imported the pinned legacy Ethereum raw and secure trie fixture corpus
  byte-for-byte with checksum, license, update, applicability, and local
  coverage records.
- Added canonical RLP integer key derivation for raw transaction and receipt
  trie indexes.
- Added bounded fuzz harnesses for compact paths, canonical RLP, node decoding,
  proof verification, mutation sequences, and ordered iteration.
- Added transport-independent EIP-1186 account membership, account absence,
  canonical account decoding, and storage-slot proof verification helpers.
- Added explicit validated legacy-RLP and EIP-2718 typed trie values plus
  canonical transaction and receipt root construction from RLP indexes.
- Added deterministic mutation-trace differential tests against pinned Geth
  and EthereumJS implementations for raw and secure trie profiles.
- Added a bounded sorted-input raw-trie root builder with strict ordering,
  transactional rejection, single finalization, and ordinary-insertion parity.
