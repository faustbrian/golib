# Troubleshooting

Start with the returned outcome or `*idempotency.Error`. Preserve
`Error.Reason`, `Error.Field`, and the wrapped cause internally, but redact
keys, results, metadata, and credentials from support output.

## Outcomes

| Outcome | Meaning | Operator action |
| --- | --- | --- |
| `acquired` | A new or deliberately released attempt owns the record. | Execute with the returned ownership proof. |
| `stale_owner_takeover` | An active lease elapsed and a higher fence now owns the record. | Confirm the handler protects effects with the new fence; investigate slow or dead prior owners. |
| `replayed` | A completed result matched the fingerprint. | Return the bounded stored result without executing. |
| `terminal_failure` | A recorded terminal failure matched the fingerprint. | Replay the stored failure according to the integration contract. |
| `in_progress` | Another unexpired owner is current. | Retry with backoff no earlier than the integration policy allows; do not execute concurrently. |
| `conflict` | The same key was reused with a different fingerprint. | Fix client key reuse or fingerprint-version rollout; never overwrite the record. |
| `unavailable` | Durable ownership could not be established. | Fail closed, inspect the backend cause, and reconcile unknown results after recovery. |

## Stable reason codes

| Reason | Common cause | Check |
| --- | --- | --- |
| `invalid_key` | Empty key component. | Validate namespace, tenant, operation, caller, and value before acquisition. |
| `invalid_fingerprint` | Empty policy version or malformed persisted digest. | Keep a nonempty policy version and investigate corrupt or incompatible records. |
| `limit_exceeded` | Key, lease, result, or metadata crossed a semantic bound. | Compare byte lengths with the limits in the state-machine guide. |
| `stale_owner` | Owner token or fence belongs to an older attempt. | Stop the old handler; never retry completion with its proof. |
| `lease_expired` | The backend clock reached the current owner's lease boundary. | Stop or fence writes, inspect the record, and reacquire normally if safe. |
| `not_found` | Inspect or mutation targeted a missing record. | Check retention, cleanup, prefix/schema selection, and key construction. |
| `invalid_transition` | The state does not allow the requested mutation. | Inspect the record and follow the transition matrix. |
| `unavailable` | Transport, authentication, pool, server, or unknown backend failure. | Use the wrapped cause and backend health; do not infer whether a timed-out mutation committed. |
| `invalid_configuration` | Required store or integration option is missing or invalid. | Validate dependencies, lease, retention, limits, callbacks, and replay headers at startup. |
| `invalid_lease` | Lease is zero or negative. | Supply a positive lease no greater than 24 hours. |
| `invalid_payload` | A persisted record or replay envelope is malformed or unsupported. | Quarantine the key, preserve evidence, and verify writer/reader compatibility. |
| `unsafe_backend` | Valkey version or eviction policy violates the adapter contract. | Upgrade to Valkey 9+ and configure `noeviction` before readiness succeeds. |

## A request remains in progress

Inspect the record and compare its lease timestamp only for diagnosis; the
backend remains authoritative. Check handler latency, heartbeat success,
process liveness, queue visibility timeout, and datastore latency. Do not
expire or delete a live record manually. If the owner died, normal acquisition
will return `stale_owner_takeover` after the backend observes lease expiry.

If this repeats under load, the lease may be below tail latency, heartbeats may
be starved, or a hot key may represent multiple business operations that need
distinct identities.

## Conflicts after a deployment

Fingerprint policy versions are semantic identifiers, not software release
numbers. A request created under one policy conflicts if a deployment computes
a different version or digest for the same key. During a rolling change,
either keep producing the old policy until its retry window ends or introduce
a new key generation. Never silently reinterpret an existing version.

Exclude transport-only data such as trace headers, connection metadata, JSON
whitespace, map iteration order, and retry counters. Include every business
field whose change must make key reuse unsafe.

## PostgreSQL diagnostics

- Verify the application role's `search_path` selects the schema containing
  `idempotency_records`.
- Verify the migration version and index exist before application readiness.
- Inspect pool exhaustion, transaction duration, advisory-lock waits, row-lock
  waits, dead tuples, and cleanup lag.
- Treat connection loss or commit errors as unknown results; reconnect and
  inspect the record.
- Do not edit the JSONB record manually. The decoder rejects inconsistent or
  malformed state.

## Valkey diagnostics

- Run the same checks as `valkey.Open`: Valkey 9+ and
  `maxmemory-policy noeviction`.
- Verify the ACL allows scripts, `TIME`, hash commands, expiry commands,
  `INFO server`, and `CONFIG GET maxmemory-policy` used by startup.
- Check cluster slot health, failovers, memory headroom, persistence, replica
  lag, client timeouts, and server latency.
- `NOSCRIPT` is recovered by the client through script evaluation; repeated
  failures indicate ACL, connectivity, or server problems.
- A lost reply after script execution is an unknown result. Reconnect and
  inspect; do not execute work solely because the client returned an error.

## Corrupt or unsupported records

Fail closed and preserve the raw datastore record in restricted incident
evidence. Identify the writer version and whether a rolling deployment allowed
an older reader to encounter a newer schema. Restore with a tested migration
or application reconciliation; do not weaken decoding or replace the record
with an assumed state. Report a reproducible decoder bypass through the
[security policy](../SECURITY.md).
