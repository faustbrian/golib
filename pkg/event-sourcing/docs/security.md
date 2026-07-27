# Security, privacy, and compliance

Event history is durable application data, not an audit log, authorization
system, secrets vault, or compliance product. This library validates bounded
envelopes and redacts its own diagnostics; applications and deployments remain
responsible for data classification, access control, encryption, retention,
erasure, monitoring, and legal review.

## Data classification

Treat payloads, metadata, aggregate identifiers, correlation identifiers,
tenant values, partition values, snapshots, projection state, outbox rows,
queue records, Kafka records, backups, traces, and test fixtures as separate
data stores with separate exposure paths.

Do not put credentials, access tokens, encryption keys, or unnecessary personal
data in events. Prefer opaque aggregate and message identifiers. An event name,
aggregate type, tenant, or partition can itself reveal sensitive business
information even when payloads are encrypted.

Event immutability amplifies a poor classification decision: later upcasting
changes how retained bytes are interpreted but does not erase the original
bytes from the event store, archive, replica, backup, broker, or exported
diagnostic artifact.

## Package guarantees

Core constructors bound identifiers, payloads, metadata, and container sizes;
own mutable input; reject malformed stored input; and return stable error
categories. Core error strings and first-party telemetry do not include payload
bytes, metadata values, event identities, hostile input, panic values,
credentials, or driver diagnostics.

Those guarantees do not sanitize application callback errors, custom store
logs, database logs, broker logs, or values an application deliberately adds
to telemetry. Review every replacement boundary against the same policy.

## Authorization and isolation

`Tenant` and `Partition` are routing metadata only. They do not enforce row
security, topic isolation, authorization, encryption domains, or noisy-neighbor
limits. Enforce identity and authorization before aggregate load and behavior,
and enforce the same boundary on projection queries, replay tools, repair tools,
snapshot access, outbox administration, and broker consumers.

Where PostgreSQL row-level security or separate schemas/databases are used,
test caller-owned transactions and maintenance operations under the exact
production roles. A global reader crosses streams by design and therefore
requires stricter operational authorization than an aggregate read.
Projection runners therefore require a per-batch `ReplayGuard` before any
replay callback or history read. Use it to enforce current authority and record
an idempotent operational audit entry. `PermitReplay` is appropriate only when
equivalent controls are proven outside the runner.

## Encryption and keys

Use authenticated transport to PostgreSQL, Kafka, queue backends, telemetry
collectors, and backup storage. Configure encryption at rest through the
deployment or an application-owned envelope codec. The core intentionally does
not own keys or silently encrypt payloads.

Application encryption must preserve deterministic event identity and explicit
schema versions while bounding ciphertext before allocation. Record key IDs,
not keys. Define rotation, restore, replay, and cryptographic-shredding
procedures before writing protected history; losing a required historical key
makes reconstitution fail.

## Retention, deletion, and erasure

Snapshots and projections are derived acceleration data and never justify
deleting authoritative history. An event that says data was deleted is a
business transition, not physical erasure of earlier bytes.

Before retention or erasure, inventory every copy:

1. primary and replica event tables;
2. snapshots, projections, checkpoints, caches, and search indexes;
3. pending, delivered, retried, and dead-letter outbox rows;
4. queue and Kafka records, compacted topics, and consumer state;
5. backups, archives, exported fixtures, logs, traces, and benchmark corpora;
6. keys whose destruction is part of the approved erasure design.

Define legal holds, minimum history needed for invariants, projection rebuild
limits, broker retention, backup expiry, restore behavior, and evidence of
completion. Destructive history repair or erasure must be separately named,
authorized, reviewed, auditable, rehearsed on a restored copy, and followed by
integrity and replay verification.

## Integrity and recovery

Restrict direct writes to event, stream-head, global-position, snapshot,
checkpoint, and outbox tables. Prefer compensating events for domain correction
and rebuilding derived state for projection defects. If authoritative repair is
unavoidable, preserve the original evidence and message identities, document
the mapping, take a consistent backup, pause affected writers and consumers,
repair all ordering invariants, and rebuild every affected derived consumer.

Commit ambiguity is not permission to retry with new message IDs. Reconcile
the prepared IDs and complete expected batch against both event and outbox
storage before acknowledging, retrying, or repairing.

## Operational review checklist

- threat-model application callbacks and every custom adapter;
- scan dependencies, licenses, secrets, images, generated files, and SBOMs;
- use least-privilege database and broker roles with credential rotation;
- bound contexts, pools, batches, retries, decompression, and allocations;
- test backup, restore, replay, promotion, key rotation, and erasure;
- alert on conflicts, corrupt history, poison deliveries, checkpoint stalls,
  outbox age, dead letters, broker lag, and commit ambiguity;
- keep production payloads and credentials out of tests and support bundles.

The [database structure guide](database-structure.md),
[PostgreSQL operations](../postgres/README.md), [outbox guide](outbox.md),
[Kafka guide](kafka.md), and [telemetry guide](telemetry.md) define the adjacent
guarantee boundaries.
