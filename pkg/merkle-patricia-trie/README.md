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

## Raw trie quick start

```go
limits := mpt.DefaultLimits()
trie, err := mpt.NewRawTrie(limits)
if err != nil {
    return err
}
trie, err = trie.Update(ctx, []byte("dog"), []byte("puppy"))
if err != nil {
    return err
}
value, err := trie.Get(ctx, []byte("dog"))
root, err := trie.Root()
```

Updates return new immutable snapshots; the receiver remains unchanged. Empty
values have deletion semantics. `Root` is always the 32-byte legacy
Keccak-256 commitment, including for embedded root encodings.

Use `NewSecureTrie` when caller keys must be legacy-Keccak transformed exactly
once. Secure iteration exposes transformed keys because the core does not
retain preimages. Use `RLPIndexKey` with a raw trie for transaction and receipt
indexes.

## Storage and proofs

`Commit` sends immutable hashed nodes and a compare-and-swap root publication
to a caller-owned `NodeStore`. `LoadRawTrie` and `LoadSecureTrie` resolve nodes
lazily and verify the hash and canonical encoding of every read. Loaded
snapshots commit only to their source store; use `Rebuild` before migrating a
root to another store. The `memory` package provides a concurrent atomic
adapter.

Stores may implement `RootRetainer` and `NodePruner`. The memory adapter keeps
the published root implicitly retained. Call `RetainRoot` before publishing a
new root when an older snapshot must remain loadable, keep the returned
`RootRetention`, and call `Release` only when every user of that root is done.
`Prune` validates the complete canonical graph for the published and retained
roots, then atomically removes all other stored nodes:

```go
lease, err := store.RetainRoot(ctx, historicalRoot, reachabilityLimits)
if err != nil {
    return err
}

// After every historical-root user is finished:
if err := lease.Release(releaseCtx); err != nil {
    return err
}
result, err := store.Prune(ctx, reachabilityLimits)
```

Retention is explicit and process-local for the memory adapter. A lost lease
is not crash-recovery evidence. Persistent adapters must durably define lease,
publication, pruning, and recovery semantics before presenting the same
guarantee. `CollectReachableNodes` is the bounded integrity-checked mark
primitive available to adapter authors.

Missing reads return `MissingNodeError` with only the exact unavailable hash.
After retrieving that encoded node from a peer or archive, call
`RecoverNode` to produce a new immutable overlay snapshot. The old snapshot
continues to report the missing node. Recovered bytes are hash-checked,
canonically decoded, copied, and bounded by `MaxRecoveryNodes` and
`MaxRecoveryBytes`; a commit atomically repairs the source store without
changing the recovered root. Retry the original operation after each recovery
because it may identify the next missing descendant.

`Prove` creates membership or non-membership paths. `ProveMany` creates one
deterministically ordered proof for a set of keys and includes each shared
hashed node once. `MembershipClaim` and `AbsenceClaim` keep claim intent
explicit; `VerifyRawMultiProof` and `VerifySecureMultiProof` reject duplicated,
reordered, unused, or missing proof nodes. The profile-specific verification
functions bind the supplied root, key transformation, exact value or absence,
canonical nodes, path transitions, and resource limits. A valid proof says
nothing about whether the supplied root is canonical-chain, finalized, recent,
or authorized.

## EIP-1186 boundary

`VerifyAccountProof` binds canonical account RLP to the secure path of an exact
20-byte address and returns owned nonce, balance, storage-root, and code-hash
fields. `VerifyAccountAbsence` keeps absence distinct from malformed or failed
proofs. `VerifyStorageProof` accepts an exact 32-byte slot key and a minimal
unsigned big-endian value, derives the secure path, encodes the Ethereum
storage value, and binds verification to the proven account storage root. An
empty expected storage value verifies absence.

These helpers consume decoded proof-node bytes and do not depend on JSON-RPC
objects or hex/quantity conventions.

## Transaction and receipt roots

`LegacyTrieValue` accepts only a canonical RLP list. `TypedTrieValue` preserves
an explicit EIP-2718 type byte and opaque non-empty payload. `TransactionRoot`
and `ReceiptRoot` insert those values into a raw trie under `RLPIndexKey(0)`,
`RLPIndexKey(1)`, and so on.

These constructors validate trie framing and canonical legacy RLP only. They do
not validate transaction or receipt fields, signatures, type activation, or
fork rules; callers must supply values already validated under their explicit
protocol profile.

For already sorted raw key/value streams, `SortedBuilder` calculates the same
root without retaining the completed trie. Keys must be strictly increasing,
values must be non-empty, and finalization succeeds exactly once. The builder
retains only the open nibble frontier, copies caller bytes, has no goroutines,
and is owned by one caller:

```go
builder, err := mpt.NewSortedBuilder(limits)
if err != nil {
    return err
}
for _, entry := range sortedEntries {
    if err := builder.Add(ctx, entry.Key, entry.Value); err != nil {
        return err
    }
}
root, err := builder.Finalize(ctx)
```

This root-only builder is intended for transaction/receipt-style workloads. It
does not produce a mutable snapshot or persist nodes.

## Status

The compatibility foundation is being implemented in the delivery phases
described in [architecture](docs/architecture.md). No release or Ethereum
compatibility claim should be inferred from package presence alone.

Licensed under Apache-2.0.
