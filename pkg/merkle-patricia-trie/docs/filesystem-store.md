# Durable filesystem store

The `filesystem` package implements `mpt.NodeStore` and `mpt.NodeIterator`
without adding a database dependency to the root package. It is intended for a
single process that owns one store directory.

## Opening and committing

```go
store, err := filesystem.Open(
    ctx,
    "/var/lib/example/state-trie",
    filesystem.DefaultLimits(),
)
if err != nil {
    return err
}
defer store.Close()

trie, err := mpt.LoadRawTrie(store.Root(), store, mpt.DefaultLimits())
if err != nil {
    return err
}
next, err := trie.Update(ctx, key, value)
if err != nil {
    return err
}
next, err = next.Commit(ctx, store)
```

`Open` creates the directory when absent, validates the checksummed root
record, and removes only adapter-named temporary files left by interrupted
atomic writes. Reads reject symlinks, non-regular files, oversized nodes, and
bytes whose legacy-Keccak hash does not equal the requested filename.

Every commit:

1. validates node-count and byte limits;
2. writes each immutable node to a same-directory temporary file;
3. syncs and renames each node to its lowercase hash filename;
4. syncs the node directory;
5. writes and syncs a checksummed root record;
6. atomically replaces `ROOT`; and
7. syncs the store directory.

A process termination before root replacement leaves the old root published.
A termination after replacement exposes the new root. In either case, every
node referenced by the observable root was synced first. Unpublished node
files are safe content-addressed orphans and may be reused by a later commit.
Incomplete temporary files are collected on the next `Open`.

If directory sync reports an error after `ROOT` was renamed, `CommitTrie`
returns `ErrStorageCommit` and `Store.Root` exposes the renamed root. The
publication outcome is therefore explicit and always points to a complete
node set, but the caller must treat the durability result as indeterminate and
reconcile the published root before retrying.

## Ownership and concurrency

One `Store` is safe for concurrent reads, audits, and one serialized writer.
Concurrent commits derived from the same previous root produce one winner and
`ErrStaleRoot` for the other writer. No store lock is held across filesystem
I/O or iterator callbacks.

The caller MUST ensure that only one open `Store` owns a directory. This
adapter does not provide cross-process locking. `Close` rejects a commit that
is currently publishing rather than waiting while filesystem I/O is active.
In-flight reads do not retain mutable adapter state and may finish.

## Limits and recovery scope

`filesystem.Limits` bounds:

- bytes in one node;
- nodes and total bytes in one commit; and
- total immutable node files, directory entries inspected by iteration, and
  interrupted-write recovery.

`MaxStoredNodes` counts every content-addressed node file, including nodes that
became unreachable from the published root or were written by a failed commit.
Because this adapter does not prune, callers must size that lifetime bound or
rotate into a separately rebuilt store before it is exhausted.

Use context deadlines for elapsed work. Nil and canceled contexts are rejected
with the root package's typed errors.

The adapter implements durable node storage and root publication only. It does
not implement `RootRetainer` or `NodePruner`; no durable lease or pruning claim
is made. Use `CollectReachableNodes` and a separately crash-tested retention
policy before deleting content-addressed nodes. The memory adapter's pruning
tests do not establish durable filesystem pruning.

## Security boundary

The directory path is caller-controlled configuration and must not be shared
with untrusted writers. The adapter validates every exact node read and
checksums the root record, but it does not authorize the root, establish chain
finality, encrypt values, hide access patterns, or protect a directory after
another process obtains write access.
