# Adoption and migration

Choose a fixed record encoding before adoption. Canonical cryptographic digests
are a natural fit because byte ordering is deterministic and records do not
carry secrets that require parsing during merge.

1. Create a dedicated owner-only parent on encrypted ephemeral storage.
2. Derive a unique 32-byte key for the dataset and purpose.
3. Choose `ChunkRecords` from a measured memory budget.
4. Set `MaximumRecords` from an explicit source-population ceiling.
5. Ensure the resulting chunk count is at most 64.
6. Add records, consume the sorted stream, and always close the store.
7. Install signal handling and a caller-owned stale-directory janitor as
   described in [Operations and Kubernetes](operations.md).

When replacing an in-memory sort, compare output including duplicates and empty
input. When replacing plaintext spill files, add operational cleanup for stale
encrypted directories left by process crashes. Do not reuse a storage key
across unrelated datasets merely for convenience.

Applications that need more than 64 chunks should partition at a semantic
boundary or introduce a separately reviewed multi-pass design. Raising the
fan-in or silently using unbounded memory is not a compatible workaround.
