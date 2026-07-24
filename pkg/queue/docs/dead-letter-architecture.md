# Dead-letter data-plane architecture

This document is the reconciliation baseline for first-version dead-letter
support. It describes behavior verified in the implementation, identifies
unsupported operations explicitly, and separates the package data plane from
`queue-control-plane` authorization, audit, and presentation concerns.

## Ownership boundary

`queue` owns failure classification, delivery settlement, durable
dead-letter persistence, record reading, and redrive execution. A worker must
remain correct when no control plane is running. The control plane may
authenticate, authorize, audit, select, and request an operation through the
versioned `management` contracts; it never opens a broker client or performs a
delivery acknowledgement itself.

## Reconciled state model

| State | Meaning | Durable identity requirement |
| --- | --- | --- |
| available | Accepted by the source and eligible for delivery | Backend message identity when the backend exposes one |
| in flight | Reserved, leased, pending, or delivered to a handler | Source identity and settlement owner |
| acknowledged | Handler succeeded and source settlement succeeded | Source identity may no longer be readable |
| retryable failure | Handler failed and package policy permits another attempt | Original identity plus bounded attempt state |
| delayed retry | Retry is waiting in package backoff or broker requeue delay | Original identity; no new logical job |
| terminal pending | Classification or exhaustion requires dead-letter settlement | Source remains recoverable until destination acceptance |
| dead-lettered | Destination durably accepted the record | Stable dead-letter ID and original source ID |
| settlement failed | Append or source acknowledgement failed or is unknown | Source and destination identities retained where measurable |
| selected | A bounded management retry or replay selected the record | Stable source record ID and command idempotency key |
| redriven | Destination accepted a retry or replay | Replay generation and lineage retained |
| deleted | One selected dead-letter record was removed | Deletion result is explicit, partial, or unknown |
| purged | A confirmed bounded collection operation removed records | Backend deletion guarantee is reported |
| expired | Deliberately configured retention removed the record | Retention policy and deadline are known before expiry |

No counter or log entry alone proves a state transition. Durable states require
broker evidence, and ambiguous network outcomes remain unknown rather than
being reported as success.

## Current execution-path inventory

This table freezes behavior that existed before the dead-letter completion
work. Additive implementation must preserve these public delivery contracts
unless a documented correctness fix changes them.

| Backend | Failure and poison path | Current terminal settlement | Current management |
| --- | --- | --- | --- |
| Ring | Handler retries execute in process; process loss removes all work | No durable source or destination | Unsupported |
| Redis Pub/Sub | Handler retries execute after transient delivery; decode failure is returned | No acknowledgement, replay, or durable terminal state | Unsupported |
| Redis Streams | Success calls `XACK`; failures append records; bounded `XAUTOCLAIM` recovers idle PEL entries; permanent, malformed, or exhausted work appends before `XACK` | Package-managed append-before-ack with at-least-once duplicate risk | List, inspect, retry, bounded bulk retry, allowlisted replay, delete, and record purge |
| Valkey Streams | Failed attempts are appended to a failure stream; idle PEL work is reclaimed; exhaustion appends before `XACK` | Package-managed append-before-ack with at-least-once duplicate risk | List, inspect, retry, bounded bulk retry, allowlisted replay, delete, and record purge |
| Core NATS | Handler retries execute after a lossy Core NATS delivery; shutdown republishes best effort | No durable acknowledgement or terminal state | Unsupported |
| NSQ | Success sends `FIN`; recoverable failure sends `REQ`; exhausted, permanent, and malformed work publishes a terminal envelope before `FIN` | Package-managed publish-before-FIN with duplicate risk | Public envelope decoder; topic listing and mutations unsupported |
| RabbitMQ | Success sends `basic.ack`; retryable failure confirms a bounded-attempt republish; exhausted, permanent, and malformed work confirms a terminal publish before source ack | Package-managed confirmed publish-before-ack with duplicate risk at the confirm/ack boundary | Broker terminal queue; package record management unsupported |

The root queue currently treats `RetryCount` as the number of retries after the
initial handler invocation. The initial delivery is attempt one. Retries happen
inside one broker delivery and the backend is not acknowledged between handler
attempts.

## Current classification inventory

The `management` contract now exposes retryable, permanent, malformed,
canceled, and infrastructure classifications. Classified errors retain their
causes through `errors.Is` and `errors.As` while their string representation
contains only the stable classification and code. Joined errors resolve both
classification and code with infrastructure, canceled, malformed, permanent,
then retryable precedence. Equal-rank codes resolve lexically so join order
cannot alter persistence. Plain errors remain retryable. Context cancellation
and deadline expiration are canceled.

The root queue passes the final handler error through the additive
`core.FailureAcknowledger` extension and otherwise preserves the legacy `Nack`
contract. Root handler failures become bounded classified failures before they
reach logs, observers, or settlement. Recovered panics use permanent
`handler_panic`; acknowledgement and failure-settlement failures use
infrastructure codes. Redis Streams, Valkey Streams, NSQ, and RabbitMQ consume
the same winning classification/code resolution. Stable unsupported-version,
lease-loss, dead-letter-destination, and administrative-quarantine codes are
exported by `management`. Durable package backends produce the destination
code when terminal persistence fails, and stream settlement produces lease
loss when the delivery is no longer owned. Custom payload-version checks and
administrative quarantine integrations use the same canonical constants.

## Capability reconciliation

| Capability | Ring | Redis Pub/Sub | Redis Streams | Valkey Streams | Core NATS | NSQ | RabbitMQ |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Durable delivery | No | No | Yes | Yes | No | Yes | Yes when broker durability is configured |
| Reliable attempt count | Process only | Process only | PEL deliveries are measurable | PEL deliveries are recorded | Process only | Native delivery attempts are persisted | Package attempt header is persisted |
| Package dead-letter persistence | No | No | Implemented | Implemented | No | Implemented | Implemented when manual ack is enabled |
| Atomic source transfer | N/A | N/A | Not implemented | No; append then ack | N/A | No; publish then FIN | No; confirmed terminal publish then source ack |
| List and inspect | Unsupported | Unsupported | Implemented | Implemented | Unsupported | Unsupported | Unsupported |
| Retry and bulk retry | Unsupported | Unsupported | Implemented | Implemented | Unsupported | Unsupported | Unsupported |
| Replay | Unsupported | Unsupported | Implemented only to allowlisted streams | Implemented only to allowlisted streams | Unsupported | Unsupported | Unsupported |
| Delete and purge | Unsupported | Unsupported | Implemented for record streams | Implemented for record streams | Unsupported | Unsupported | Unsupported |
| Package retention | Unsupported | Unsupported | Disabled by default; advertises exact count only when configured; manual purge advertised | Disabled by default; advertises exact count only when configured; manual purge advertised | Unsupported | Broker policy only; no package capability | Broker policy only; no package capability |

An absent capability is an explicit unsupported result during negotiation. It
must never be replaced by process-memory emulation that claims durability.
Redis Pub/Sub and Core NATS will remain unsupported for durable dead-letter
operations.

## Management contract reconciliation

The versioned `management` and authenticated `managementhttp` packages provide
real bounded protocol, record-reader, and command transports. They do not make
a backend capable by themselves.

- Redis Streams and Valkey Streams implement `management.RecordReader` and
  record `management.Controller` operations, including replay only when an
  explicit destination allowlist is configured.
- Ring, Redis Pub/Sub, Core NATS, NSQ, and RabbitMQ advertise no failure or
  dead-letter management capability.
- `queue-control-plane` consumes these contracts without acquiring Redis or
  Valkey clients and intersects retention and mutation capabilities across
  rolling worker versions before enabling operator actions.

`management.JobRecord` now supports legacy version 0 and a bounded version-1
dead-letter envelope with source identity, classification, timing, lineage,
tenant, trace, producer/worker version, retention, diagnostics, and
hidden-by-default payload fields. The HTTP transport round-trips version 1 but
keeps legacy JSON free of v1 fields. Redis Streams, Valkey Streams, and NSQ
populate validated producer-supplied metadata while keeping broker source
identity separate. Redis Streams and Valkey Streams propagate bounded
retry/replay lineage, the last delivery observation, and a configured
management worker version. Other optional fields remain unknown when a backend
cannot measure them.

`job.Metadata` is the producer-supplied source for optional v1 fields. It is
JSON-encoded inside the existing bounded job envelope and cloned at message
construction. Every string and tag key/value is limited to 256 bytes, no more
than 32 tags are accepted, and a supplied enqueue time must be non-zero.
Original identity, schema, content type, retry policy, handler/job type, trace,
tenant, and producer version are optional: an empty value means unknown and
must remain empty when a backend cannot observe it. Payload bytes and arbitrary
failure text are deliberately not metadata fields. Backend settlement paths
must copy only this validated allowlist into operational records.

## Verified crash boundaries

Valkey terminal settlement currently has these non-atomic boundaries:

1. Before destination append, the source remains pending and recoverable.
2. After destination append but before source acknowledgement, process death
   can leave both a dead-letter record and a pending source.
3. If the acknowledgement succeeded but its response was lost, source state is
   unknown and the dead-letter record may be observed again through recovery.

Redis Streams terminal settlement has the same append-before-ack ordering.
Its retry path appends before source acknowledgement and record deletion. Its
replay path retains the source, appends before updating the durable duplicate
registry, and appends a replacement before deleting the prior replay. A crash
can therefore leave a duplicate record or destination entry, but not silently
remove the recoverable source before durable acceptance. Partial and unknown
responses require reconciliation by original stream ID and replay lineage.

RabbitMQ also confirms retry or terminal publication before acknowledging the
source. Destination failure requeues the source; process death or a failed ack
after confirm can duplicate the republished or terminal message. Its bounded
attempt and source headers are the reconciliation identity available today.

The original stream ID is the deduplication identity for stream paths. No
backend claims exactly-once transfer. NSQ likewise waits for terminal
publication before `FIN`; publication failure sends `REQ`, while process death
after publication can duplicate by source ID.

## Deliberately unsupported surfaces

Unsupported behavior is part of the stable contract, not a placeholder for an
in-memory emulation:

- Ring, Redis Pub/Sub, and Core NATS have no durable dead-letter capability.
- NSQ and RabbitMQ expose terminal destinations but no package record reader or
  mutation controller; their retention and capacity policies remain broker
  owned.
- Redis Streams and Valkey Streams support deliberate exact record-count
  retention and manual record purge. They do not advertise package time or
  byte retention, so a per-record retention deadline remains unknown.
- First-delivery time, privileged diagnostics, and a redacted prose summary
  remain unknown when the backend or producer does not supply safe evidence.
  The stable failure code is not expanded into arbitrary exception text.
- Backend-specific depth, oldest age, throughput, and rate measurements remain
  unsupported unless the status contract marks that exact measurement as
  supported. An absent measurement is never fabricated as zero.

Real-broker evidence, fault-boundary tests, race, fuzz, mutation, coverage,
benchmark, compatibility, documentation, and vulnerability gates are listed in
[integration evidence](integration-evidence.md). The authenticated control
plane consumes only negotiated `management` capabilities and never broadens
the backend guarantees in this matrix.
