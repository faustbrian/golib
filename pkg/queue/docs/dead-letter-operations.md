# Dead-letter operations and recovery

This runbook applies to package-managed durable dead letters. It does not turn
Ring, Redis Pub/Sub, or Core NATS into durable transports. Operators must first
confirm the backend and advertised capabilities rather than inferring support
from an HTTP route or control-plane screen.

## Triage sequence

1. Stop or rate-limit redrive when failure volume is rising. A redrive into the
   same broken handler can amplify the incident.
2. Record worker protocol versions, negotiated capabilities, source pending
   depth, dead-letter depth, oldest age, settlement failures, and broker health.
3. Separate handler failures from `dead_letter_destination_unavailable`,
   `lease_lost`, acknowledgement failures, and unknown command outcomes.
4. Keep payload visibility hidden during routine triage. Reveal one record only
   through an authorized path when metadata and safe codes are insufficient.
5. Repair destination capacity, permissions, type/topology, or connectivity
   before resuming terminal settlement or redrive.

## Destination outage or capacity pressure

Redis Streams and Valkey Streams leave the source pending when terminal append
fails. NSQ sends `REQ` when terminal publication fails. RabbitMQ negatively
acknowledges and requeues after a failed terminal publish or confirmation.
Do not delete or acknowledge the source to reduce the visible backlog. Restore
the destination, verify one canary terminal transfer, then increase worker or
redrive concurrency gradually.

Count retention is disabled unless `WithRecordRetention` is configured on the
stream backends. Time and byte retention are unsupported package capabilities;
configure and audit broker policy separately. NSQ and RabbitMQ retention is
entirely broker-owned. A full broker must backpressure or fail settlement; it
must never be treated as successful dead-lettering.

## Ambiguous append and settlement outcomes

For Redis Streams and Valkey Streams, append succeeds before source `XACK`.
For NSQ, terminal publish succeeds before `FIN`. For RabbitMQ, a publisher
confirmation arrives before source `basic.ack`. Process death or response loss
between those steps can produce a dead letter plus a recoverable source.

Reconcile duplicates by the source record ID and bounded redrive lineage. Do
not compare arbitrary payloads or claim exactly-once processing. A lease-loss
code means the current worker cannot prove source ownership; inspect broker
state before retrying an administrative mutation.

## Poison storms

Pause automated redrive, preserve representative hidden records, and group by
stable classification and failure code. Malformed and unsupported-version
records are not corrected by retry. Permanent handler failures require a code
or data change. Retryable exhaustion may be redriven only after its dependency
recovers. Administrative quarantine uses the canonical
`administrative_quarantine` code and remains an authorized control-plane
decision, not an unbounded worker-side list.

## Retry, replay, delete, and purge

- Retry returns work to its logical queue. Replay requires an allowlisted
  destination and an explicit reject-or-replace duplicate policy.
- Keep batch selection at or below the contract limit and bound caller
  concurrency. Treat partial and unknown outcomes as reconciliation work.
- A successful enqueue precedes source-record deletion. If deletion is unknown,
  expect a duplicate instead of reporting false success.
- Delete affects one record. Record purge is destructive and confirmed. Queue
  purge is unsupported by the stream controllers.
- Redis and Valkey redrives carry original/prior dead-letter IDs and a bounded
  generation so repeated failure does not recursively grow metadata.

## Backup, restore, and rollback

Backup and restore are broker responsibilities. Preserve the source stream or
queue, consumer-group state where the broker supports it, record streams or
terminal queues/topics, and replay duplicate registries as one operational
unit. Restoring only payload records can invalidate pending ownership and
idempotency evidence.

During rollback, keep newer record streams intact. Older workers may not
understand new management fields but must not purge them. Negotiate the
intersection of worker capabilities, drain incompatible workers, and retain
the rollback configuration until the broker retention window has passed.

## Incident closure

Confirm source pending depth is stable or falling, destination appends succeed,
oldest age recovers, redrive is bounded, and no partial or unknown commands
remain unreconciled. Record any deliberately revealed payload access in the
control-plane audit system; `queue` supplies bounded context but does not own
administrative authorization or auditing.
