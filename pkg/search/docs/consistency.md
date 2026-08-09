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
version, and fingerprint, then repairs missing, stale, or unexpected documents.
