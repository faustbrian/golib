# Operations, storage, and recovery

## Immutable snapshots and updates

`RawTrie`, `SecureTrie`, `StateTrie`, and `StorageTrie` are immutable logical
snapshots. Update and delete methods return a new value and leave the receiver
unchanged. A failed operation does not expose partial state.

`RawTrie.ApplyBatch` and `SecureTrie.ApplyBatch` validate the complete mutation
set before returning a new snapshot. Construct mutations with `Put` and
`Remove`. A batch rejects zero-value mutations, empty put values, repeated
keys, invalid keys or values, and work beyond `MaxBatchOperations`.

Snapshots are safe for concurrent reads. Multiple goroutines may derive new
snapshots from the same old snapshot. The application owns synchronization
when deciding which derived root becomes authoritative. `SortedBuilder` is a
separate mutable object owned by one caller and cannot be used after successful
finalization.

## Node-store contract

The root package depends only on caller-owned interfaces:

```go
type NodeReader interface {
    GetNode(context.Context, mpt.Root) ([]byte, error)
}

type NodeStore interface {
    NodeReader
    CommitTrie(context.Context, mpt.StoreCommit) error
}
```

`GetNode` is addressed by the exact legacy-Keccak hash. Returned bytes are
untrusted: the trie copies, rehashes, decodes, and validates them. An unavailable
hash must be reported as `ErrMissingNode`; the package converts it to a
`MissingNodeError` that identifies only the missing hash.

`CommitTrie` is one atomic compare-and-swap transaction:

1. verify that `StoreCommit.PreviousRoot()` is still published;
2. make every `StoreCommit.Nodes()` value durable under its exact hash;
3. publish `StoreCommit.Root()` only after all node writes are durable; and
4. return `ErrStaleRoot` on a publication conflict.

A failure before root replacement must leave the old complete root observable.
An adapter that detects a durability failure after atomic replacement may
return an error with either the old or new complete root observable, but it
must expose that outcome and document reconciliation. A backend without atomic
publication or explicit durability semantics must not claim the strong commit
contract. Stored and returned byte slices must be immutable from the caller's
perspective.

A snapshot loaded from a store can commit only to the same store. Use
`Rebuild` to migrate a root to a different backend so unchanged descendants
are copied rather than silently referenced from the source.

## Publication and snapshot retention

In-memory mutation, logical snapshot creation, node durability, root
publication, retention, and pruning are separate events. Holding a trie value
does not automatically retain its durable root in an external store.

Stores that support historical roots implement `RootRetainer`. Retain a root
before publishing a replacement and keep the returned `RootRetention` until
all readers have released the snapshot. `Release` makes the root eligible for
future pruning; it does not delete nodes immediately.

Persistent retention operations may cross their durable publication point
before reporting a storage-sync error. After such an error, reopen and
inventory durable retentions before retrying or pruning. A returned non-nil
retention remains caller-owned even when accompanied by an error.

Persistent adapters must define:

- whether node writes and root publication share one durable transaction;
- crash behavior before, during, and after publication;
- compare-and-swap conflict handling;
- durable representation and recovery of retained-root leases;
- pruning synchronization with publication and retention changes; and
- how callers audit or rebuild stored nodes.

The `memory` adapter is concurrent and atomic but process-local. The
`filesystem` adapter syncs immutable node files before atomic root replacement
and has process-termination recovery tests at both sides of root publication.
It also persists bounded historical-root retentions and recovers interrupted
retention and pruning operations. It requires exclusive directory ownership.

## Iteration and streaming construction

`RawTrie.Iterate` yields reconstructed raw keys in lexicographic byte order.
`SecureTrie.IterateHashed` yields transformed 32-byte paths because the core
does not retain preimages. `IterationOptions` combines:

- `Prefix`, matched conjunctively;
- inclusive `Start`;
- exclusive `End`; and
- `Limit`, where zero means the configured hard maximum.

Callbacks run synchronously without internal locks. The trie does not retain
callback data, and `Entry.Key` and `Entry.Value` return owned copies. Callback
errors and context cancellation stop traversal.

`SortedBuilder` accepts strictly increasing raw keys and non-empty values. A
duplicate or late key fails without changing the accepted prefix. The builder
retains the open trie frontier rather than the complete result and finalizes
exactly once. It calculates a root only; use ordinary insertion plus `Commit`
when materialized nodes are required.

## Rebuild and audit

`Rebuild` iterates a snapshot, reconstructs it through canonical updates, and
requires the rebuilt root to match. It is the migration boundary between
stores and a corruption-detection tool, not an EVM state transition.

`NodeIterator` is an optional store audit capability. It must yield immutable
nodes in ascending hash order and respect the caller's maximum. The package's
`CollectReachableNodes` validates the complete hashed graph reachable from an
explicit root set while enforcing `ReachabilityLimits`.

## Missing-node recovery

Handle missing data as a resumable sequence:

```go
value, err := trie.Get(ctx, key)
var missing *mpt.MissingNodeError
if errors.As(err, &missing) {
    encoded := fetchByExactHash(missing.Hash)
    trie, err = trie.RecoverNode(ctx, missing.Hash, encoded)
    // Retry Get. A deeper missing node may be reported next.
}
```

`RecoverNode` verifies the exact hash and canonical node grammar, copies the
bytes, and returns a new snapshot with a bounded overlay. The old snapshot is
unchanged. Committing the recovered snapshot atomically repairs the source
store without changing the root. Wrong hashes are `CorruptNodeError`; malformed
canonical structure remains distinct from unavailable data.

## Pruning

`NodePruner.Prune` may remove only nodes unreachable from the currently
published root and every retained historical root. The mark phase must verify
hashes and canonical transitions. Publication or retention changes during a
prune must cause a conflict or a retry, never deletion based on stale reachability.

`PruneResult` reports stored nodes before and after and the removed byte count.
Cancellation, corruption, missing data, a resource limit, or a stale root must
leave the previous node set intact.

A storage failure is different: a non-zero result accompanied by an error
means the adapter crossed its documented prune commit point. The result
describes the committed logical removal, and adapter recovery must finish it.
A zero result accompanied by an error means no removal committed and recovery
must restore any staged nodes.

## Errors and retry decisions

Use `errors.Is` for stable categories and `errors.As` for bounded diagnostics:

| Outcome | Typical error | Caller action |
| --- | --- | --- |
| Key is not present | `ErrAbsentKey` | Treat as a normal lookup result where allowed |
| Store lacks a node | `MissingNodeError`, `ErrMissingNode` | Fetch exact hash and resume with `RecoverNode` |
| Stored bytes fail integrity | `CorruptNodeError`, `ErrCorruptNode` | Quarantine backend data; do not retry blindly |
| Proof lacks required data | `ErrIncompleteProof` | Request the missing proof path |
| Proof is malformed or surplus | `ErrMalformedProof` | Reject the producer |
| Well-formed proof does not prove claim | `ErrFailedProof` | Reject the claim |
| Proof starts at another root | `ErrWrongRoot` | Reconcile trusted root and proof source |
| Publication raced | `ErrStaleRoot` | Reload the published root and reapply policy |
| Work exceeded a bound | `ErrResourceLimit` | Reject or deliberately raise a reviewed limit |
| Context ended | `ErrCanceled` plus context cause | Apply normal cancellation or deadline policy |

Errors do not include complete keys, values, encoded nodes, proofs, preimages,
or storage credentials.

## Resource limits

Start from `DefaultLimits`, then lower bounds for the application's expected
corpus. Review every increase as a denial-of-service budget. `Limits` covers
key and value bytes, traversal and encoding nodes, hash operations, store
reads, iterator results, rebuild work, batch operations, proof keys/nodes/bytes,
and recovery nodes/bytes. `ReachabilityLimits` separately bounds roots,
retentions, nodes, bytes, depth, reads, and hashes during audit and pruning.

All I/O and potentially expensive public operations take `context.Context`.
Use caller deadlines in addition to structural limits. A nil context is
rejected; there are no hidden goroutines or background retries.
