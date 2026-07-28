# Architecture and encrypted chunk format

Records accumulate in one contiguous byte buffer bounded by `RecordBytes *
ChunkRecords`. The buffer is sorted lexicographically in place and spilled to a
new chunk. This repeats until finalization, when at most 64 sorted chunks are
opened and merged with a heap containing at most one record per chunk.

Every record is encoded independently as:

```text
random GCM nonce || AES-256-GCM ciphertext || GCM authentication tag
```

The authenticated data is:

```text
"extsort1" || uint64(chunk) || uint64(record ordinal) || uint64(record bytes)
```

All integers use big-endian encoding. The fixed encrypted record length allows
streamed reads without a plaintext header. Chunk and ordinal authentication
detect truncation, reordering, and substitution across positions. The format
is private temporary storage, not a persistence or interchange format.

The parent is validated when the factory is built and again immediately before
opening a store. Work directories and chunk files are created with
`os.MkdirTemp` and `os.CreateTemp`, then explicitly forced to modes `0700` and
`0600`. Generated paths never enter public errors.

Abrupt process termination may leave encrypted work directories. Crash cleanup
is an operator policy because the module cannot know whether another process
still owns a directory.
