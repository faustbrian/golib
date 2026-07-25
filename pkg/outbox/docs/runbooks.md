# Recovery Runbooks

## Backlog or publisher outage

Record backlog, oldest age, readiness, retry rate, and publisher status. Leave
pending and leased rows intact; expired leases are recovery. Restore the
publisher, then increase concurrency gradually within broker and database
limits. Never replay pending or leased rows to accelerate draining.

## Dead letters and replay

Inspect ID, topic, attempts, timestamps, and the redacted `last_error` under
restricted access; never copy payload or returned publisher errors into logs
or tickets. Correlate with the publisher's restricted diagnostics, fix the
cause, authorize an explicit bounded terminal-ID list, and call `Replay` with
requester and incident reason. Missing or non-terminal IDs fail atomically.
Verify replay audit rows and consumer deduplication. For an ambiguous response,
inspect state and audit before repeating.

Configure `StoreConfig.ReplayAuthorizer` with default-deny application policy.
The hook must authenticate the requester and authorize every requested message
for the correct tenant and incident. `ErrReplayUnauthorized` is intentionally
generic; consult restricted application authorization diagnostics rather than
weakening the returned error.

Use `Store.Inspect` for bounded state/topic/time summaries. It deliberately
omits payload and metadata; direct database payload access should require a
separate, audited break-glass path.

## Retention incident

Pause maintenance. Where archival is mandatory, use only
`ArchiveAndPruneDelivered`. Hook failure preserves rows. Ambiguous commit can
archive again, so reconcile by envelope ID. `PruneDelivered` is only for
intentional permanent deletion. Treat `ErrArchiverPanic` as an archive outage;
the panic value is discarded and the selected rows remain in PostgreSQL.

Use `ArchiveAndPruneDead` before dead-letter deletion when incident evidence
must be retained. `PruneDead` is irreversible and removes replay capability.

## Disaster recovery

Restore application tables, outbox messages, and replay audit to one consistent
point. An older snapshot can republish formerly delivered records. Validate
schema, constraints, state counts, and lease timestamps, then resume one relay
and scale after normal draining. Never reconstruct records from broker state
without stable application identities and a reconciliation plan.

During failover, expect readiness and mutations against the old endpoint to
fail. Refresh the pool or service route according to infrastructure policy,
require writable-primary readiness on the replacement, and only then resume
relays. An ambiguous in-flight commit still requires durable-state inspection.

## Reproducible exercises

Run the complete crash, timeout, isolation, contention, retention, replay,
publisher, and adapter recovery suite against an ephemeral PostgreSQL major:

```sh
make recovery POSTGRES_VERSION=18
```

Repeat with `14`, `15`, `16`, and `17` before release. The command creates only
Testcontainers databases and does not connect to application or production
services. Run `make migration-integration POSTGRES_VERSION=18` separately for
the current sibling migration runtime.
