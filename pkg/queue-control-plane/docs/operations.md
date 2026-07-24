# Operations, retention, backup, and incidents

## Routine checks

Monitor liveness and readiness separately. Liveness proves the HTTP process can
respond; readiness proves its configured dependency check succeeds. Query
`/version` and `/v1/capabilities` after every rollout so automation records the
actual revision and enabled sources.

When telemetry is enabled, the production process exports HTTP traces and
metrics plus `queue.control.command.count` and
`queue.control.command.duration`. Command instruments use bounded `action` and
`outcome` attributes; alert on `dispatch_failed`, `journal_error`, and
`outcome_unknown` without adding tenant, actor, target, or idempotency labels.

The management tenant document enables bounded worker and queue snapshots.
The `alerts` package derives deterministic queue-wait, failure, stale-worker,
dead-letter, and command-failure threshold breaches from bounded validated
snapshots. Unsupported measurements never trigger an alert.

Managed workers read durable desired state through the authenticated API and
apply it in an application-owned reconciliation loop. Monitor read failures,
revision conflicts, and timed-out drains. Control-plane unavailability must not
stop ordinary delivery or imply active state; the worker retains the last
applied authored revision.

A lost management endpoint or Valkey connection makes an in-flight
administrative acknowledgement unknown; it does not stop ordinary queue
delivery and must not trigger a direct-backend fallback. PostgreSQL connection
saturation blocks command admission before transaction work and dispatch.
Metric export failure is isolated from command execution. The hardening suite
exercises these cases, kills a separate process at every PostgreSQL
command/audit transaction boundary, and runs 10,000-worker stale and reconnect
storms.

The evaluator does not send notifications or store time series.
`alerts.NewTelemetryExporter` accepts at most 12,000 validated alerts and emits
only the fixed `kind` metric label through a meter owned by `telemetry`; it
never labels tenant, queue, record, actor, or error text. Production worker and
queue snapshots are available when the management tenant document is
configured. Scheduled evaluation is a deployment blocker: run the bounded
evaluator and exporter from an application-owned scheduler before relying on
these alerts.

## Audit and command retention

The PostgreSQL audit store verifies complete tenant chains in bounded pages and
deletes bounded prefixes while advancing each anchor transactionally. The
production binary exposes this only as the one-shot retention mode documented
in the deployment guide; it is not an HTTP mutation and never runs inside a
serving replica.

Before scheduling a retention Job:

1. Set a documented tenant retention period and legal hold policy.
2. Verify the chain and export the verification head to protected telemetry.
3. Configure batches no larger than 1,000 entries and at most 100 per run.
4. Rerun an incomplete result until no eligible prefix remains.
5. Verify the retained chain from the updated anchor.

Never delete audit rows directly. Direct deletion does not advance the trusted
anchor and makes the retained chain unverifiable.

The strict policy file records legal holds explicitly. A legal-hold tenant is
skipped before verification or mutation, so removing a hold requires a reviewed
policy-file change. After verified audit cleanup, the same cutoff removes only
terminal commands with no retained audit or current desired-state reference.
Active commands and the commands backing pause, drain, or termination state
remain durable. Desired-state cleanup is not yet implemented; do not add ad hoc
deletes.

Command deletion ends the stored idempotency window for that key. Retention
must exceed every automated retry and manual incident-reconciliation window.
After a key expires, inspect the target system rather than assuming the old
command never ran.

## Backup and restore

Use PostgreSQL-consistent backups that include all `queue_control_*` tables and
the migration state. Encrypt backups, restrict restore permission, and retain
them separately from the live database.

A restore procedure must:

1. Stop command traffic or restore into an isolated database.
2. Restore commands, desired states, audit events, and audit anchors from the
   same snapshot.
3. Apply any required embedded migrations exactly once.
4. Verify every tenant audit chain from its restored anchor.
5. Compare `/version`, capabilities, command heads, and desired state with the
   recovery record before reopening traffic.

`make disaster-recovery-postgres` seeds a real PostgreSQL 18 source through the
production repositories, creates a native custom-format backup, restores it
into an isolated database, reapplies migrations idempotently, and verifies the
command, desired-state, readiness, and retained audit-chain head. CI and release
quality run this drill without reusing a database from another job.

## Incident response

### Suspected key compromise

1. Remove or deny the subject in a new access document and restart all
   replicas.
2. Preserve PostgreSQL and ingress logs; do not mutate audit rows.
3. Verify the affected tenant chains and enumerate commands by actor and time.
4. Inspect command outcomes and the external target state before remediation.
5. Issue a new subject or key only after scope is understood.

### `outcome_unknown`

1. Stop automatic retries.
2. Read the command by its original idempotency key.
3. Inspect the Kubernetes or future `queue` acknowledgement state.
4. Restore outcome persistence or reconcile manually under a new documented
   incident action only when the original effect is known.

### Audit verification failure

1. Make the database read-only to control-plane writers if operationally safe.
2. Preserve the database, anchors, backups, process revision, and logs.
3. Compare the last independently exported verification head.
4. Treat the history after the last trusted head as untrusted.
5. Restore only after forensic review; never regenerate hashes to hide damage.

## Upgrade and rollback

Roll out database-compatible server revisions gradually. Run migrations under
a single owner, verify readiness, version, and capabilities, then add replicas.
The current schema has eight embedded forward migrations; downgrade automation
is not provided.

Workers continue normal queue processing without the control plane. During a
control-plane rollback, preserve PostgreSQL and do not roll back schema or
delete newer command records without an explicit compatibility assessment.
Kubernetes scaling remains independent from worker protocol negotiation.

The complete fault and protocol response table is in the
[hardening evidence matrix](hardening.md).
