# FAQ

## Why fixed-size records?

They provide predictable allocation and encrypted record framing without a
plaintext length header. Canonical digests and identifiers commonly fit this
model.

## Are duplicates removed?

No. Every added record is yielded exactly once.

## Can callbacks retain the record slice?

No. Copy the bytes before the callback returns.

## Why only 64 chunks?

The cap makes file descriptors and merge memory explicit. The module rejects
larger fan-in instead of relying on host limits.

## Does encryption replace encrypted disks?

No. Use both. Per-record encryption limits plaintext spill exposure; encrypted
ephemeral storage and host controls protect metadata and abandoned artifacts.

## What happens after a crash?

The process cannot run `Close`, so encrypted directories may remain. Operators
must use a conservative stale-owner cleanup policy.
