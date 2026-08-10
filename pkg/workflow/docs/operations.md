# Operations

## Deployment

1. Register every definition version needed by running instances before new
   workers receive traffic.
2. Apply `postgres.SchemaMigrationsFor` in ascending version order. Keep schema
   creation and migration authorization in deployment tooling.
3. Start workers only after migrations and definition registration succeed.
4. During a rolling upgrade, keep old pinned definitions available until no
   active or recoverable instance references them.
5. Stop admission, drain workers with a bounded context, and let the process
   supervisor restart crashed processes. Worker goroutines are not supervisors.

Rollback is an operator decision. Apply migration `Down` statements only in
reverse order and only after proving no required data would be removed. Code
rollback must retain every definition behavior referenced by durable history.

## PostgreSQL schema

Migration 1 owns instances, transition idempotency records, immutable history,
and due work. Migration 2 owns audited dead-letter resolutions and the partial
dead-letter listing index. Terminal transitions set the instance archive time
in the same transaction as terminal history.

The adapter uses optimistic instance sequences and stable keyset pagination.
Claims use locked, skip-locked admission, bounded leases, monotonically
increasing fencing tokens, and tenant-fair ordering. A stale owner cannot renew,
complete, retry, or dead-letter work after losing its fence.

## Recovery and reconciliation

- **Process death before commit:** no transition is visible; the same stable
  transition identity may be retried.
- **Commit response lost:** call `ReconcileTransition`. Exact fingerprint means
  committed, missing means the same transition may be retried, and conflict
  requires investigation.
- **Death after activity attempt start:** replay exposes an in-flight attempt;
  do not call the activity again until its idempotency system reconciles it.
- **Lease expiry:** another worker may claim the work with a higher fencing
  token. The old owner must treat stale-fence errors as final ownership loss.
- **Timer worker death:** due work remains durable and becomes claimable after
  lease expiry. Timer firing must commit before work completion.
- **Inbound duplicate:** reuse the source message identity as transition ID.
  Exact replay is accepted; conflicting reuse is rejected.
- **Dead letter:** list unresolved records, authorize an actor externally, then
  submit a stable retry or discard command. Commit-unknown responses are retried
  with the exact command identity.

Restore drills must restore instance, transition, history, work, and resolution
tables from one consistent PostgreSQL recovery point. After restore, reconcile
outbox and broker projections from authoritative transition identities before
resuming acknowledgements.

## Operator commands

Callers authenticate and authorize every actor. Commands require a stable
command ID, actor, reason, decision time, and expected sequence or work fence.
Pause, resume, retry, cancel, terminate, compensate, approve, manual
compensation resolution, and dead-letter resolution are idempotent only for
exact command content. Every accepted command is audited durably.

Use `InspectInstance` for a bounded replay diagnostic and `ExportHistory` for a
bounded streaming export. Listing uses stable keyset cursors; it is not a fixed
snapshot unless the caller supplies a transaction-scoped adapter.

## Capacity

Set worker concurrency below both the database connection budget and the
downstream activity budget. Claim limits, history pages, list pages, transition
event counts, fan-out, payloads, results, and inputs are all bounded. Size the
work table for retry and lease-expiry bursts, and monitor oldest due work rather
than only queue depth.

Partitioning and retention are deployment-specific. Archive terminal instances
before deleting history, and never remove history required for replay,
reconciliation, audit, or an unresolved external outcome. Continue-as-new keeps
individual histories bounded while preserving an explicit predecessor and
successor relationship.

## Observability

Worker hooks are synchronous and must remain bounded. Export low-cardinality
event kind, work kind, disposition, and outcome metrics. Workflow, work,
transition, command, tenant, and correlation identities belong in structured
event data or traces, not metric labels.

Alert on oldest due work, repeated lease loss, unknown activity or child-start
outcomes, retry exhaustion, failed compensation, unresolved dead letters,
stuck paused or cancelling instances, and reconciliation conflicts. Hook failure
must not silently change durable semantics.

## Security

Treat definitions and activity implementations as trusted application code; the
package does not execute untrusted code. Bound and validate all external payloads
before allocation. Keep database credentials and transport diagnostics out of
history, errors, hooks, logs, and exported artifacts.

Authorize operator actions outside the package and apply tenant isolation in
the store or database policy selected by the application. The built-in schema
does not claim row-level tenant authorization. Encrypt connections and backups,
restrict migration and termination privileges, audit exports, and test restore
access separately from normal worker credentials.
