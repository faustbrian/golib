# Adoption, comparison, and migration

## When to use this module

Use this module when an application needs an independently usable Ethereum
execution-layer modified Merkle Patricia trie with explicit key profiles,
immutable snapshots, caller-owned persistence, or offline proof verification.

Do not use it as a substitute for:

| Structure or system | Key difference |
| --- | --- |
| Binary Merkle tree | Binary position tree; no Ethereum hexary path compression or RLP nodes |
| Sparse binary Merkle tree | Fixed binary depth and default hashes; different proofs and roots |
| RFC 9162 Certificate Transparency tree | Append-only log commitment with different leaf/node hashing |
| Consensus-layer SSZ merkleization | SSZ chunking, generalized indices, and SHA-256 rather than execution MPT rules |
| Verkle tree | Vector commitments and different path/proof semantics |
| Unified Binary Trie proposals | Future/research execution-state structure, not the active hexary MPT |
| EVM or execution client | This package does not execute transactions or apply fork state transitions |
| Blockchain database | The filesystem adapter is a durable node store, not a query engine, cache, or blockchain database |

The module verifies a trie commitment. It does not determine canonical chain,
fork choice, finality, block validity, or authorization of a root.

## Adoption checklist

Before production use:

1. Identify every trie as raw, secure, state, storage, transaction, or receipt.
2. Record who validates higher-level account, transaction, and receipt
   semantics.
3. Set reviewed `Limits` and context deadlines for the real corpus and threat
   model.
4. Define which component owns root selection and publication.
5. Prove the selected `NodeStore` atomicity, durability, crash recovery, and
   stale-root behavior.
6. Define historical-root retention and pruning recovery before enabling
   deletion.
7. Keep JSON-RPC and hex/quantity conversion outside the core.
8. Run fixture and differential gates on the supported platform and toolchain.
9. Compare reconstructed roots against existing production data before cutover.
10. Retain rollback access to the old implementation and roots until the new
    reader, writer, recovery, and pruning paths have been exercised.

For a single-process durable deployment, the
[filesystem adapter](filesystem-store.md) provides atomic root publication.
It does not provide historical-root retention or pruning; choose another
adapter or add a separately proven policy when those operations are required.

## Migrating from go-ethereum trie APIs

This module intentionally does not expose Geth concrete types or mutable trie
internals. Important differences include:

- Updates return a new immutable snapshot; assign the result.
- Raw and secure profiles use different types rather than constructor flags.
- State and storage helpers require fixed-width semantic inputs.
- Empty core values delete; storage zero deletes.
- Public roots are always 32-byte commitments.
- `Commit` passes one immutable compare-and-swap request to a caller-owned
  store rather than returning a mutable node set.
- Loaded snapshots commit only to their source store; use `Rebuild` to migrate.
- Proof node order is part of the public contract, and surplus nodes are
  rejected.
- Secure iteration returns hashed paths unless the application separately
  retains preimages.
- Transaction and receipt roots accept exact pre-encoded values and do not
  import Geth transaction or receipt types.

A safe migration reads an existing trusted root, exercises lookups and proofs,
rebuilds into the new store, compares the root, and only then changes
publication ownership.

## Migrating from a generic Patricia trie

Do not reuse roots until all of these match Ethereum exactly:

- byte-to-nibble order;
- hex-prefix leaf/extension flags and even padding;
- canonical Ethereum RLP;
- legacy Keccak-256 rather than standardized SHA3-256;
- embedded child encodings below 32 bytes;
- hashed references at 32 bytes and above;
- branch value semantics and deletion compaction;
- the canonical empty root; and
- raw versus secure key transformation.

Matching lookup behavior is insufficient. Rebuild the complete key/value map
and compare roots and proof bytes against pinned fixtures and independent
clients.

## FAQ

### Is `Root` an encoded node?

No. It is always a 32-byte legacy-Keccak commitment. Encoded nodes are carried
as proof or store bytes.

### Should I hash a key before calling `SecureTrie`?

No. `SecureTrie`, `StateTrie`, and `StorageTrie` hash caller keys exactly once.
Passing a hash as the caller key commits `Keccak-256(hash)`.

### Can an empty value be stored?

No. At the core boundary an empty value deletes. Ethereum storage zero also
deletes. Absence is explicit through `ErrAbsentKey` or an absence proof.

### Does `VerifyAccountProof` trust the RPC response?

No. It binds canonical account bytes to an exact address under the supplied
state root. The caller must obtain and authorize that root independently.

### Why does receipt-root construction require transactions?

EIP-2718 requires each typed receipt to match its transaction type. Supplying
the transaction sequence prevents a valid receipt payload from being committed
under a mismatched envelope type.

### Are snapshots durable after `Update`?

No. An update creates only a logical snapshot. `Commit` makes hashed nodes
durable and publishes the root according to the store contract.

### Can I commit a loaded root into another store?

No. Unchanged descendants may exist only in the source. Use `Rebuild`, compare
the root, then commit the rebuilt snapshot to the destination.

### Does releasing a retained root delete it?

No. It only makes the root eligible. A later successful prune removes nodes
that are unreachable from every published or retained root.

### Are secure-trie preimages stored?

No. The core retains no preimage store. Secure iteration therefore exposes
transformed 32-byte paths. A preimage adapter would be a separate explicit
privacy and retention boundary.

### Is this a stable v1 release?

No. The module remains pre-v1 until every release gate and compatibility claim
in the project goal has complete evidence. Review the changelog and source
provenance before adopting a pre-v1 revision.
