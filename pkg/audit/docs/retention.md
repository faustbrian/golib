# Retention, archival, erasure, and legal hold

Retention is archive-before-delete and two phase. `PlanRetention` returns
bounded candidates bound to each persisted canonical SHA-256 digest. Archive
and verify them outside database locks, then pass the unchanged plan to
`ApplyRetention`. Records changed, already removed, or placed on hold between
planning and application are not deleted.

Holds and releases are immutable append-only events. A hold is rechecked under
a per-record advisory transaction lock at prune time. Retention events remain
after record deletion so historical hold decisions are not rewritten. Retention
uses a separately privileged credential; ordinary writers have no delete right.
The record ID and canonical SHA-256 remain as an immutable identity tombstone
after content deletion so delayed retries cannot resurrect or replace the
record. Record IDs therefore must be opaque or pseudonymous: tombstones are
permanent, and a digest does not eliminate dictionary risk for predictable
content.

Callers own schedules, jurisdictional policies, data-minimization periods,
erasure exceptions, pseudonymization, archive security, hold authority, and
approval evidence. Partition detach or archival must preserve active holds and
must not bypass per-record reconciliation.
