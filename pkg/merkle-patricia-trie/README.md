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

`ProveRange` returns every raw leaf in an explicit `[start,end)` byte interval
plus a deterministic ordered witness. An empty `end` means no upper bound.
`VerifyRawRange` proves that the supplied leaf sequence is exact and
consecutive: an omitted leaf, changed value, missing node, reordered node, or
unused node is rejected. `ProveHashedRange` and
`VerifySecureHashedRange` use already transformed 32-byte Keccak paths and do
not hash their endpoints or items; this keeps raw keys and transformed secure
paths unambiguous. `RangeProofFromNodes` is the transport boundary for decoded
RLP proof-node bytes.

## State and storage tries

`StateTrie` accepts exact 20-byte addresses and `EncodedAccountValue` values.
`NewAccountValue` encodes the execution-spec account tuple `[nonce, balance,
storageRoot, codeHash]`, using a `uint64` nonce, a 32-byte unsigned balance,
and exact 32-byte commitments. It deliberately retains canonically encoded
empty accounts: fork-dependent account clearing belongs to state-transition
code outside this package.

`StorageTrie` accepts exact 32-byte slot keys and 32-byte unsigned words. It
hashes each slot exactly once, trims leading zeroes before canonical RLP
encoding, and treats an all-zero word as deletion. `GetSlot` returns
`ErrAbsentKey` for a missing slot rather than manufacturing a present zero
value. Both profiles provide immutable lookup, update, deletion, proof,
commit, rebuild, and missing-node recovery operations.

```go
storage, err := mpt.NewStorageTrie(limits)
storage, err = storage.UpdateSlot(ctx, slot, word)
storageRoot, err := storage.Root()

accountValue, err := mpt.NewAccountValue(
    nonce, balance, storageRoot, mpt.EmptyCodeHash(), limits,
)
state, err := mpt.NewStateTrie(limits)
state, err = state.UpdateAccount(ctx, address, accountValue)
stateRoot, err := state.Root()
```

## EIP-1186 boundary

`VerifyAccountProof` binds canonical account RLP to the secure path of an exact
20-byte address and returns a `uint64` nonce, 32-byte balance, storage root,
and code hash. `VerifyAccountAbsence` keeps absence distinct from malformed or
failed proofs. `VerifyStorageProof` accepts an exact 32-byte slot key and a
minimal unsigned big-endian expected value, derives the secure path, encodes
the Ethereum storage value, and binds verification to the proven account
storage root. An empty expected storage value verifies absence.

These helpers consume decoded proof-node bytes and do not depend on JSON-RPC
objects or hex/quantity conventions.

## Transaction and receipt roots

Transaction and receipt values use distinct types, so they cannot be passed to
the wrong root helper. `LegacyTransactionValue` and `LegacyReceiptValue` accept
only canonical RLP lists. `TypedTransactionValue` and `TypedReceiptValue`
require a `ForkProfile`, validate that the type is active for that fork, require
the known type-1 through type-4 payload framing to be a canonical RLP list, and
preserve the exact `type || payload` bytes.

`TransactionRoot` inserts transaction values into a raw trie under
`RLPIndexKey(0)`, `RLPIndexKey(1)`, and so on. `ReceiptRoot` additionally takes
the matching transaction sequence and rejects a receipt whose legacy/typed
kind, type, or fork profile differs from its transaction. The supported profile
matrix is Berlin type 1; London, Paris, and Shanghai types 1-2; Cancun types
1-3; and Prague and Osaka types 1-4.

```go
transaction, err := mpt.TypedTransactionValue(
    mpt.CancunProfile, 3, transactionPayloadRLP, limits,
)
receipt, err := mpt.TypedReceiptValue(
    mpt.CancunProfile, 3, receiptPayloadRLP, limits,
)
transactionRoot, err := mpt.TransactionRoot(ctx, []mpt.EncodedTransactionValue{
    transaction,
}, limits)
receiptRoot, err := mpt.ReceiptRoot(
    ctx,
    []mpt.EncodedTransactionValue{transaction},
    []mpt.EncodedReceiptValue{receipt},
    limits,
)
```

These constructors validate trie framing, canonical RLP, fork activation, and
the EIP-2718 transaction/receipt type relationship. They do not validate
transaction fields, signatures, receipt fields, or state-transition semantics;
callers remain responsible for those protocol rules.

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
