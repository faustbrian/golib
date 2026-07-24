# Operations, retention, and capacity

Operate the idempotency store as correctness-critical infrastructure. If the
store cannot establish ownership, the default response is `unavailable`; do
not turn a datastore incident into duplicate business execution by bypassing
it globally.

## Readiness and dependencies

For PostgreSQL, apply `postgres.GoMigration()` through the deployment
`migrations` runner before the application is ready. The neutral
`postgres.SchemaMigration()` descriptor remains available for other deployment
systems. Verify connectivity, table permissions, the intended `search_path`,
and pool capacity. Do not run schema creation in request startup.

For Valkey, construct the store with `valkey.Open`, not only `valkey.New`.
`Open` verifies Valkey 9 or newer and `maxmemory-policy noeviction`. A failed
check is a readiness failure. Re-run the check after infrastructure changes
that can alter server version or eviction policy.

The memory adapter has no production readiness guarantee: process restart
deletes every record and separate processes elect separate owners. Its
`MaxRecords` option defaults to 10,000 and cannot exceed 1,000,000. Reaching
capacity rejects acquisition of a new key; it does not evict retained records
or prevent replay and transition of an existing key.

## Signals

Instrument the application boundary rather than placing a datastore client in
the semantic core. Record at least:

- acquisition outcomes by bounded operation name and backend;
- transition latency and errors by operation and stable reason code;
- handler execution, replay, conflict, in-progress, takeover, and terminal
  failure counts;
- lease duration, heartbeat failures, and work that approaches its lease;
- stale-owner and lease-expired rejections;
- replay result sizes and rejected size-limit crossings;
- cleanup deletions, errors, duration, and oldest overdue record;
- PostgreSQL pool saturation, transaction latency, lock waits, table and index
  size, and dead tuples;
- Valkey memory use, fragmentation, rejected writes, replication health,
  persistence health, expired keys, and eviction count.

Never use raw idempotency keys, tenant identifiers, caller identifiers,
fingerprint digests, owner tokens, or fencing tokens as metric labels. Label
cardinality must remain bounded. If correlation is required in logs, compute a
keyed digest with a separately managed secret, emit only a short encoded
prefix, and rotate it according to the application's privacy policy. Do not
log replay bodies or metadata by default.

Use `idempotencylog` with the standard logger returned by `log`, and use
`idempotencytelemetry` with `telemetry.Runtime.MeterProvider()`. The metric
adapter intentionally excludes the keyed correlation digest. See the
[observability guide](observability.md) for direct wiring and fan-out.

## Lease selection and heartbeats

Choose a lease longer than normal execution plus expected scheduler, network,
and datastore jitter, but short enough to meet recovery objectives. Measure
the tail of real handler duration before setting it. A heartbeat extends the
lease from backend-authoritative time; a local timer firing does not prove the
extension succeeded.

For long-running work, heartbeat well before expiry and stop or fence business
writes if heartbeat returns an error. Do not continue because local time says
the old lease should still be valid. A heartbeat failure has an unknown result
until the record is inspected.

Graceful shutdown should stop accepting new work, stop or finish handlers
within their leases, and release only handlers that have definitely stopped
all side effects. Releasing while a goroutine continues authorizes a new
attempt concurrently.

## Retention selection

Retention must cover the longest period in which any legitimate retry,
redelivery, reconciliation job, rolling deployment, or business fence can
refer to the key. Include delayed queues, offline clients, provider retries,
backup recovery, and incident response. Use the shortest duration that covers
those obligations because records may contain sensitive data.

Deleting a record ends its fencing domain. A later record for the same logical
key starts at fence `1`. If a business row compares numeric fences beyond the
record's retention, include a generation in the business identity, never reuse
the key, or retain the record for the full comparison lifetime.

PostgreSQL cleanup is explicit. Run `Store.Cleanup` frequently in bounded
batches and continue while a full batch is returned. Multiple workers may run
because cleanup uses `FOR UPDATE SKIP LOCKED`. Alert when the oldest
`purge_at` is increasingly overdue.

Valkey cleanup is TTL-driven. Active keys use `lease + retention`; terminal,
abandoned, and expired keys use `retention`. Monitor expiry progress and memory
headroom. Never enable eviction as a cleanup mechanism.

Valkey replication is asynchronous unless the deployment adds a stronger
durability policy. The conformance failover test waits for one replica before
killing the primary and promoting that replica; this proves recovery of a
replicated ownership record, not survival of every acknowledged production
write. Select persistence, replica acknowledgements, and failover policy for
the application's tolerated loss window, then inspect after promotion before
retrying an unknown result.

## Wait, polling, and retry policy

The package does not start waiters, polling loops, automatic heartbeats, or
semantic retries. Application loops must have a context deadline, capped
attempt count, capped backoff with jitter, and an explicit error allowlist.
Inspect after unknown backend results and reconcile possible side effects
before retry. Never retry a conflict or stale-owner response as a transient
failure. See the [resource budgets](resource-budgets.md) for the complete
bounded-resource contract.

## Capacity model

Start with measured traffic, not a generic record-size constant:

```text
live records ~= peak unique keys per second * effective retention seconds
storage ~= live records * measured p95 bytes per record * safety factor
```

`effective retention` is longer than configured retention for active records
because it includes the lease, and PostgreSQL backlog can extend it further.
Measure representative records containing real key-field lengths, result
sizes, and metadata counts. Include:

- PostgreSQL heap, primary key and `purge_at` index, JSONB and TOAST overhead,
  dead tuples, WAL, replicas, backups, and cleanup lag;
- Valkey hash and key overhead, allocator fragmentation, replication,
  persistence buffers, failover headroom, and cluster imbalance;
- bursts and hot keys, not only daily averages;
- replay bandwidth and application memory used to buffer bounded results.

Keep at least enough headroom to survive the cleanup or failover recovery
window established by the service's objectives. Load-test the chosen record
shape and retention assumptions before production rollout.

## Incident response

1. Fail ownership acquisition closed and preserve the first backend error.
2. Determine whether each affected call failed before the backend command,
   after it, or with an unknown result.
3. Inspect the authoritative record after connectivity returns.
4. Reconcile business effects by stable business identity before retrying any
   call that may have crossed its side-effect boundary.
5. Do not delete records, shorten TTLs, or manufacture ownership proofs to
   clear an incident.
6. Restore normal traffic gradually while monitoring takeover, conflict,
   stale-owner, cleanup, and backend saturation signals.

See [troubleshooting](troubleshooting.md) for reason-specific diagnostics and
[crash semantics](crash-semantics.md) for recovery at each failure boundary.
The [threat model](threat-model.md) states the corresponding trust boundaries
and residual risks.
