# Performance

Peak record-buffer memory is bounded by `RecordBytes * ChunkRecords`, plus the
Go slice and merge heap overhead. The merge keeps one decrypted record and one
file descriptor per chunk, capped at 64.

Sorting costs `O(n log n)` comparisons within chunks and `O(n log k)` during
the merge, where `k <= 64`. Each record is encrypted once and decrypted once.
Smaller chunks reduce peak memory but increase file descriptors and heap work.

`BenchmarkEncryptedExternalSort` measures a complete 32-byte record workload,
including encryption, file IO, merge, cleanup, allocations, and the local
filesystem. Results are environment-specific and should be repeated on the
intended migration host with realistic chunk and population sizes.
