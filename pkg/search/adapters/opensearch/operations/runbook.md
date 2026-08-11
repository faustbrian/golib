# OpenSearch adapter incident runbook

## overload

Confirm whether rejections are local admission, OpenSearch 429/503 responses,
or an open process-local circuit. Stop caller retries that exceed the shared
attempt/deadline budget, reduce concurrency, and preserve unknown write items
for reconciliation. Scale only after checking heap, disk, shard, thread-pool,
and breaker capacity.

## cluster-loss

Mark the service unready on red health or zero data nodes. Keep reads and
writes bounded by their existing contexts; do not add an unbounded retry loop.
Confirm seed reachability, TLS identity, DNS answers, and shard recovery. Resume
traffic only after primaries are active and initialization has completed.

## unknown-write-outcome

Do not replay the original write or bulk batch. Read the authoritative ID and
external version, compare it with the source-of-truth event, and retry only a
specific item whose prior outcome has been reconciled. Preserve the original
operation category without logging source, tenant, index, credential, or task
values.

## pit-expiry

Discard the expired cursor and restart the bounded traversal from a new PIT.
Investigate traversal duration, page/result limits, cleanup failures, and
cluster pressure. Never delete all cluster PITs; delete only the PIT decoded
from the application-owned signed search cursor.

## migration-rollback

Stop new application writes with the configured durable ingress fence and wait
for in-flight writes to drain. Reconcile all writes acknowledged on the current
generation into the rollback generation. Use `CutoverAlias` for a fresh full
verification and reverse alias mutation, then release queued writes. Keep both
generations until reconciliation reports zero drift.

## migration-unknown-outcome

Keep the durable migration-ID coordinator held while gathering authoritative
evidence. For `MigrationCreating`, inspect the exact physical generation and
its live definition before repairing state to created or abandoning it. For
`MigrationDispatching`, locate the exact backend task from deployment audit
records; never submit a replacement task without proving the first was absent.
For an alias/checkpoint ambiguity, read the complete alias membership, retain
the write fence, reconcile acknowledged writes in the observed direction, and
repair state only to the matching verified transition. For `MigrationCleaning`,
confirm whether the exact generation still exists and that its name was not
reused. Every repair records operator, timestamp, source evidence, old/new
phase, and plan fingerprint. If evidence is missing or contradictory, retain
both generations and leave `ErrMigrationRecovery` unresolved.

## generation-cleanup

Require a fresh destructive authorization, exact physical name/fingerprint,
no read or write alias reference, drained old-version readers and PITs, elapsed
rollback and backup retention, and a repeated forward zero-drift proof. Execute
the final checks and deletion inside the durable migration-ID coordinator.
Never delete by prefix, age alone, or a caller-stored fingerprint.

## drift-repair

Run bounded reconciliation from one persisted source snapshot, classify every
missing, stale, divergent, and orphan record, and dispatch only attributed
external-version repairs. Orphans require an atomic durable tombstone
reservation. Reconcile unknown or partial outcomes item by item, resume from the
persisted checkpoint, and require a second complete zero-drift pass before
cutover or cleanup.

## full-rebuild

Persist one transactionally consistent source snapshot identity and its
commit-ordered outbox cursor. Resume the stable source scan and later outbox
replay from separate durable checkpoints; never mix snapshot boundaries.
Verify the live definition and complete ID/version/canonical-digest set, then
run two zero-drift reconciliation passes before fenced cutover. Keep the old
generation until rollback, backup, reader, and PIT retention conditions pass.

## resource-exhaustion

The single-node release harness constrains each OpenSearch container to one
CPU, 1 GiB of memory, 512 processes, and 1,024 file descriptors. Multi-node
rolling-upgrade containers use the OpenSearch bootstrap minimum of 65,536 file
descriptors while retaining the CPU, memory, and process limits. Alert before
85% heap, below 15% disk headroom, any OOM kill, sustained CPU saturation,
descriptor exhaustion, increasing thread-pool rejections, or breaker trips.
Reproduce with the production mapping, aggregation cardinality, PIT
concurrency, refresh policy, and bulk sizes before changing limits.
