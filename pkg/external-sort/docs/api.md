# API and lifecycle

`NewFactory(Config)` validates the complete storage policy. `Config` declares:

- `ParentDirectory`: existing absolute non-symlink owner-only directory;
- `RecordBytes`: exact byte length of every record;
- `ChunkRecords`: maximum records held in the contiguous sort buffer; and
- `MaximumRecords`: hard limit for the complete store.

The ceiling constants bound record size, chunk count, chunk bytes, and merge
fan-in. A configuration whose ceiling would create more than 64 files is
rejected before `Open`.

`Factory.Open(ctx, key)` requires exactly 32 key bytes, consumes a random
32-byte store identity, binds the validated parent identity to a rooted
directory handle, and creates a new owner-only work directory relative to that
handle. Parent replacement or permission changes make `Open` fail closed. The
factory is safe for concurrent use. Each returned `Store` permits one active
lifecycle operation; an overlapping or callback-reentrant `Add`,
`ForEachSorted`, or `Close` returns `ErrConcurrentUse` without changing the
active operation.

`Open` normally returns either a usable Store or a nil Store with an error. If
construction creates a directory but its immediate cleanup also fails, `Open`
returns both `ErrStorage` and a non-nil cleanup-only Store. The caller must call
`Close` whenever the returned Store is non-nil, even when `err` is also non-nil.
The cleanup-only Store rejects `Add` and `ForEachSorted` with `ErrClosed` and
keeps retryable ownership of the rooted residue.

`Store.Add` copies one exact-size record. A failed call does not retain its
input. `Store.ForEachSorted` finalizes input and invokes the callback in
lexicographic byte order. Duplicate records remain distinct. Callback slices
are ephemeral and must be copied before retention.

`ForEachSorted` can be called once after pending input is successfully spilled.
It remains finalized after callback, authentication, merge-storage, or merge
cancellation failure. A pre-finalization spill or cancellation failure may be
retried. `Close` is idempotent and must be called on every path. A directory
removal failure leaves only `Close` retryable; other operations reject the
closing store. A rooted-handle close failure returns `ErrStorage` after the
directory and sensitive state have already been finalized, so later `Close`
calls are idempotent. After an overlapping call returns `ErrConcurrentUse`,
retry only after the active operation has returned.

Errors are compared with `errors.Is`. Context cancellation is returned
directly. Other public error values distinguish invalid configuration, unsafe
parents, invalid keys and records, limit exhaustion, concurrent or finalized
lifecycle misuse, entropy, storage, and authenticated corruption without
exposing sensitive values.
