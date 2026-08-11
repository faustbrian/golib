# Consistency and source of truth

An index is rebuildable derived state. Persist application state and an outbox
event atomically, then project the event with a stable idempotency key and
monotonic document version. Acknowledgement means the backend accepted the
write; visibility depends on the selected refresh policy. Immediate refresh is
expensive and should be limited to workflows that explicitly require it.

Read-your-write is an application policy: wait for refresh, overlay current
source data, or communicate eventual visibility. Duplicate delivery is normal.
Older versions must not overwrite newer ones. An ambiguous transport outcome is
unknown, not failed; reconcile before changing its version or retry exactly the
same idempotent operation.

Periodic reconciliation compares bounded source and index pages by stable ID,
version, and fingerprint, then repairs missing and stale documents. Snapshot
absence alone never authorizes deletion of an unexpected index document: a
concurrent source write may have occurred between the source and index reads.
`NewReconciler` therefore returns `ErrReconciliationDeletionGuard`, marks the
report incomplete, and dispatches no repair batch when repair encounters an
orphan.

`DriftDivergent` means the same ID and external version have different canonical
source digests. This is evidence of poisoned, nondeterministic, or corrupted
derived state, not a newer-version decision. Repair rewrites the authoritative
source at that same version only when the adapter can deterministically replace
it; otherwise the run remains incomplete and requires a higher authoritative
version. Run reconciliation again after every repair batch and require a second
complete zero-drift pass before cutover or cleanup.

Applications that delete orphans must use `NewReconcilerWithDeletionGuard` and
implement `ReconciliationDeletionGuard` in the authoritative source store. Its
single atomic operation confirms that the source identity remains deleted,
durably reserves a tombstone version greater than the observed index version,
and guarantees that every later source write uses a greater version. The
reservation must be tenant- and index-bound, concurrency-safe, and idempotent
after an interrupted or ambiguous run. Guard errors, non-increasing versions,
and the terminal `uint64` version fail before any index repair dispatch. Raw
guard errors are not retained because source-store failures may contain
credentials; caller cancellation and deadline expiry remain available through
their standard context sentinels.
