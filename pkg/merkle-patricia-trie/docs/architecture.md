# Architecture

## Boundary

The root `mpt` package owns canonical paths, nodes, roots, immutable snapshots,
proofs, limits, errors, and the caller-facing node-store contract. It has no
service, queue, database, telemetry, EVM, network, JSON-RPC, or complete-client
dependency. Optional persistent adapters belong in additive nested modules.

Raw, secure, state, storage, transaction, and receipt profiles use distinct
constructors or types. No boolean selects key hashing or value encoding.
Transaction and receipt envelope values are separate public types. Typed values
carry an explicit fork profile, and receipt-root construction binds each receipt
type to its corresponding transaction type.

`StateTrie` and `StorageTrie` are closed wrappers over secure snapshots rather
than aliases. State updates require `EncodedAccountValue` constructed from a
`uint64` nonce, 256-bit balance, storage root, and code hash. Storage updates
require exact 32-byte slot keys and words; zero words delete. Neither profile
exposes a pre-hashed update path, so address, slot, and generic secure-key
transformations cannot be selected interchangeably.

## Ownership

Caller input and returned bytes are copied. Immutable snapshots may be used
concurrently. A mutable writer has one caller-owned synchronization owner and
never launches hidden goroutines. Context cancellation and limits bound every
I/O or potentially expensive public operation.

`RawTrie` and `SecureTrie` values are immutable logical snapshots. Updates,
deletions, and batches return new snapshots and leave the receiver unchanged.
Iteration invokes callbacks synchronously and never retains callback-owned
data.

## Storage and publication

`NodeReader` retrieves canonical nodes by exact legacy Keccak hash.
`NodeStore` atomically writes a complete `StoreCommit` and publishes its root
only if the previous root still matches. Trie reads rehash and canonically
decode every stored node before use.

A snapshot loaded from a store must commit back to that same store. This
prevents a new store from publishing a root whose unchanged hashed descendants
remain only in the source store. Copying between stores is a rebuild operation,
not a commit. The `memory` adapter provides immutable node bytes, concurrent
reads, and copy-on-write compare-and-swap commits.

Missing-node recovery is an immutable overlay, not an unchecked store write.
`RecoverNode` verifies the supplied bytes against the reported legacy-Keccak
hash and canonical node grammar before returning a new snapshot. All traversal
surfaces consult the bounded overlay before the backing reader, so lookup,
mutation, proofs, iteration, and rebuild can resume without changing the old
snapshot. A same-root commit durably repairs recovered nodes using the store's
atomic compare-and-swap contract.

Pruning is an optional storage capability and never changes trie commitments.
`RootRetention` is an explicit historical-root lease; the published root is
always an implicit retention root. `CollectReachableNodes` performs a bounded
mark traversal, verifies every hash and canonical encoding, validates
extension transitions, and returns only independently stored hashed nodes.
The memory adapter snapshots its immutable node map and lease set, performs
marking without holding its lock, and swaps in the retained node map only when
neither publication nor retention changed. Cancellation, corruption, missing
nodes, resource exhaustion, and compare-and-swap conflicts leave the prior
node set intact. Releasing the final lease makes a historical root eligible,
not immediately deleted; deletion occurs on the next successful `Prune`.

`Rebuild` streams the source snapshot in trie order into a fresh materialized
snapshot and compares the reconstructed commitment with the source root. Only
that independent result may be committed to another store.

`SortedBuilder` is a separate mutable, root-only construction boundary. It
requires strict byte-key order and incrementally closes canonical subtries as
their prefixes leave the input frontier. Completed branch nodes are reduced to
embedded RLP or hash references, so retained state is bounded by key depth and
the open branch frontier rather than entry count. It cannot be reused after
successful finalization and does not expose or retain a mutable trie.

## Current proof contract

`Proof` carries an ordered root-to-terminal sequence of canonical RLP nodes.
`MultiProof` orders nodes by first encounter across lexicographically sorted
caller keys and carries each hashed node once. Embedded children remain inside
their parent and are not duplicated. Verification supports raw and secure
membership, non-membership, and mixed multi-key claims, rejecting wrong roots,
claims, profiles, missing nodes, duplicate or reordered nodes, hash mismatches,
and surplus material. `RangeProof` walks only subtrees intersecting an explicit
`[start,end)` interval. Every intersecting hashed node is required in
deterministic trie order; hashed subtrees wholly outside the interval remain
opaque commitments. This proves that the returned leaf sequence is complete
without loading unrelated subtrees. Secure range endpoints are explicit
already-transformed 32-byte paths. EIP-1186 helpers bind account proofs to
exact addresses and storage proofs to the storage root decoded from a proven
canonical account. Storage proof sets validate all claims and aggregate proof
limits before traversal and reject repeated or conflicting slots.

## Canonical representation

In-memory node forms are implementation details. Persistence and proofs use
canonical RLP. Child nodes whose canonical encoding is shorter than 32 bytes
are embedded; encodings of 32 bytes or more are referenced by the legacy
Keccak-256 digest of the complete encoding. Public roots are 32-byte
commitments; encoded root nodes use an explicitly different type.

## Delivery sequence

1. Source pins, decisions, threat model, and public boundaries.
2. Nibbles, compact paths, RLP, Keccak references, nodes, and empty root.
3. Immutable lookup, update, deletion, and small-state model comparison.
4. Stores, atomic commit/publication, snapshots, iteration, builders, rebuild,
   missing-node recovery, and pruning.
5. Membership, absence, multi-key, range, and EIP-1186 proofs.
6. Ethereum profile helpers, official fixtures, independent differentials,
   hardening, documentation, benchmarks, and release evidence.
