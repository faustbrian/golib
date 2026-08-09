# Architecture and encrypted chunk format

Records accumulate in one contiguous byte buffer bounded by `RecordBytes *
ChunkRecords`. The buffer is sorted lexicographically in place and spilled to a
new chunk. This repeats until finalization, when at most 64 sorted chunks are
opened and merged with a heap containing at most one record per chunk.

The in-memory sort has no caller comparator or filesystem callback, so it has no
recoverable sorting-failure boundary to inject. Cancellation is checked before
sorting and again before every encrypted record write. Any failure after a
chunk file is created closes and removes that uncommitted file; cleanup failure
seals the store until `Close` succeeds.

Every record is encoded independently as:

```text
random GCM nonce || AES-256-GCM ciphertext || GCM authentication tag
```

The authenticated data is:

```text
"extsort1" || 32-byte store identity || uint64(chunk) ||
uint64(record ordinal) || uint64(record bytes)
```

All integers use big-endian encoding. The fixed encrypted record length allows
streamed reads without a plaintext header. Chunk and ordinal authentication
detect truncation, reordering, duplication, and substitution across positions.
The random store identity rejects cross-store substitution even when a caller
reuses a key. The format is private temporary storage, not a persistence or
interchange format.

Each nonce contains a 64-bit process-unique store domain followed by a monotonic
32-bit per-store record counter. One process-random 64-bit seed is combined
bijectively with a synchronized process-wide store sequence, so concurrent
stores receive distinct domains even when their injected dataset entropy
repeats. The domain is also authenticated. `MaximumRecords` is below the record
counter space; exhaustion of either sequence fails with `ErrEntropy` before
wrap. A unique key per independent dataset remains required across processes.

The parent is validated when the factory is built. Existing ancestor links are
resolved once during factory construction. `Open` then binds that resolved
parent to an `os.Root` directory handle and revalidates its file identity and
owner-only mode. Work-directory, chunk, and cleanup operations remain relative
to that handle, so renaming or replacing a pathname ancestor cannot redirect
them. Each store receives a fresh owner-only work directory and exclusive
`0600` chunk files. Directory mode is explicitly normalized and verified as
`0700`, independent of the caller's umask. Platforms where `os.Root` cannot
enforce descriptor-backed containment reject `Open`. Generated paths never
enter public errors.

Chunks are written directly to unique temporary names, synchronized, and closed
before becoming merge inputs. There is no publish rename boundary. A failed
write, sync, or close leaves the chunk uncommitted and triggers removal.

Abrupt process termination may leave encrypted work directories. Crash cleanup
is an operator policy because the module cannot know whether another process
still owns a directory. See [Operations and Kubernetes](operations.md).
