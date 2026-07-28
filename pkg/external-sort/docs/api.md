# API and lifecycle

`NewFactory(Config)` validates the complete storage policy. `Config` declares:

- `ParentDirectory`: existing absolute non-symlink owner-only directory;
- `RecordBytes`: exact byte length of every record;
- `ChunkRecords`: maximum records held in the contiguous sort buffer; and
- `MaximumRecords`: hard limit for the complete store.

The ceiling constants bound record size, chunk count, chunk bytes, and merge
fan-in. A configuration whose ceiling would create more than 64 files is
rejected before `Open`.

`Factory.Open(ctx, key)` requires exactly 32 key bytes and creates a new
owner-only work directory. The factory is safe for concurrent use; each
returned `Store` has one owner and is not safe for concurrent use.

`Store.Add` copies one exact-size record. A failed call does not retain its
input. `Store.ForEachSorted` finalizes input and invokes the callback in
lexicographic byte order. Duplicate records remain distinct. Callback slices
are ephemeral and must be copied before retention.

`ForEachSorted` can be called once after pending input is successfully spilled.
It remains finalized after callback, authentication, merge-storage, or merge
cancellation failure. A pre-finalization spill or cancellation failure may be
retried. `Close` is idempotent and must be called on every path. A cleanup
failure leaves only `Close` retryable; other operations reject the closing
store.

Errors are compared with `errors.Is`. Context cancellation is returned
directly. Other public error values distinguish invalid configuration, unsafe
parents, invalid keys and records, limit exhaustion, lifecycle misuse, entropy,
storage, and authenticated corruption without exposing sensitive values.
