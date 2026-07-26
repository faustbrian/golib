# Troubleshooting

Start from the stable error category and durability outcome, not from an error
string. Preserve `errors.Is` and `errors.As` causes internally while keeping
payloads, metadata, identifiers, credentials, panic values, and raw hostile
input out of tickets, logs, traces, and support bundles.

## Aggregate load and save

| Symptom | Meaning | Safe response |
| --- | --- | --- |
| `ErrStreamNotFound` | No readable stream exists for the canonical aggregate identity | Confirm the encoded ID and aggregate type; create only through the domain's explicit creation path |
| `ErrConcurrencyConflict` | Another writer changed the stream after the expected version | Reload, re-evaluate the command against current state, and retry only when application policy permits |
| `ErrDuplicateMessageID` | A message ID already exists in the batch or store | Reconcile the original attempt; never attach the same ID to different content |
| `CommitUnknown` | The store cannot prove whether commit succeeded | Stop automatic retry, reconcile every prepared ID and expected event, then acknowledge or repair |
| `ErrCorruptHistory` | Ordering, positions, envelope fields, or application invariants are inconsistent | Quarantine the stream, preserve evidence, restore or use the reviewed repair procedure |
| `ErrUnknownEvent` | No decoder or apply case owns the stored event identity | Restore the historical registration or deploy an explicit alias/upcaster; never skip the event |
| `ErrIncompatibleVersion` | The name is known but its schema version cannot be interpreted | Restore the decoder/upcast path and verify snapshots before resuming |

A failed or panicking apply operation poisons the current lifecycle helper.
Discard that aggregate instance. Do not save or continue behavior on partially
mutated application state.

## Iteration and replay

An iterator returning `false` means end, cancellation, closure, or failure.
Always inspect `Err` and call `Close`. Use bounded read limits and context
deadlines; increasing a limit is not a repair for an unbounded consumer.

For a stalled projection:

1. inspect its atomic status and last durable checkpoint;
2. distinguish paused state from handler, poison-policy, checkpoint, read, or
   cancellation failure;
3. determine whether the handler succeeded before checkpoint persistence;
4. make the handler idempotent and repair application state if necessary;
5. resume from the durable checkpoint or run the explicit paused rebuild;
6. keep process managers, external dispatch, queues, outboxes, and Kafka
   publication disabled during projection replay.

`ErrRebuildPartial` means application state and checkpoint state may no longer
match. Leave the projection paused until both sides are reconciled.

## PostgreSQL

Before changing data, capture the PostgreSQL version, schema migration state,
connection and transaction timeouts, pool saturation, lock waits, SQLSTATE,
stream identity, expected version, and commit outcome. Do not include payload
or metadata values.

- A lock timeout before commit is not committed; retry only under the normal
  expected-version policy.
- A transaction left open holds stream and global-position locks. Cancel and
  roll it back through its owner.
- Lost connections require a bounded reconnect path; endpoint discovery,
  fencing, and failover remain deployment responsibilities.
- Restore stream heads, the global-position allocator, and messages to one
  consistent point. Restore derived state consistently or delete and rebuild
  it.

Use the real-database conformance, contention, restart, promotion, and restore
tests to reproduce adapter behavior. They do not prove a managed provider's
network, DNS, proxy, backup, or failover configuration.

## Outbox, queue, and Kafka

Growing outbox age usually means claim, lease, publication, retry, or delivered
transition failures. Inspect stable state counts and ages without logging
envelopes. A crash after broker acknowledgement and before the delivered
transition can publish a duplicate; consumers must remain idempotent.

For queue or Kafka failures, distinguish mapping rejection, publication
acknowledgement, handler failure, settlement failure, retry exhaustion,
dead-letter failure, rebalance, cancellation, and shutdown drain. Never settle
an offset or queue record before successful handling or the explicitly durable
poison/dead-letter decision. Kafka producer idempotence does not reconcile a
PostgreSQL transaction.

## Performance

Do not diagnose latency from one benchmark sample. Capture equivalent work,
allocations, latency distributions, pool settings, stream lengths, contention,
database configuration, image digest, Go version, power mode, and concurrent
load. Separate aggregate, serialization, storage, projection, outbox, relay,
and broker layers before optimizing.

## Escalation bundle

Provide the module versions, Go version, stable error categories, durability
outcomes, bounded counts and positions, redacted configuration, exact operation
sequence, cancellation and timeout state, and the smallest reproducible test.
State which checks were local, real-database, broker-backed, CI-verified, or not
run. Never attach production event bodies, metadata values, credentials,
connection strings, or unreviewed database dumps.
