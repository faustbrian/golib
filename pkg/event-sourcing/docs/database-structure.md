# Database structure and capacity

Event history is authoritative infrastructure. Choose its physical layout from
measured stream shape, replay requirements, operational ownership, and failure
recovery rather than from aggregate type aesthetics.

## First-party layout

The PostgreSQL adapter uses one shared `event_sourcing.messages` table, one
`streams` head row per aggregate stream, and one transactional global-position
allocator. The shared table keeps one migration path, one global replay source,
and one set of envelope constraints. Its indexes support:

- exact message-ID uniqueness;
- ordered reads within an aggregate stream;
- ordered global reads for replay and projections; and
- recorded-time inspection paired with global position.

Global positions are allocated through one locked row. This prevents a reader
from checkpointing past an uncommitted earlier position, but it also creates a
known write-serialization point. Capacity measurements must include that lock.

## Alternative layouts

Alternative layouts are custom `EventStore` implementations, not switches in
the first-party adapter. They must preserve atomic append, expected-version
semantics, immutable message identity, stable ordering, bounded reads, and
error categories, and should run the public store conformance suite.

### Table per aggregate type

This can isolate retention, permissions, and write hotspots by bounded-context
or aggregate type. It multiplies migrations, indexes, monitoring, replay
queries, and restore coordination. A global reader needs an explicit ordering
mechanism across tables; timestamps are not a substitute for a committed
global position.

### Table per aggregate ID

This can physically isolate a very small, fixed stream population. For normal
application cardinalities it creates unbounded schema objects, migration and
catalog pressure, difficult connection planning, and impractical global
replay. The library does not recommend or implement this layout.

### Document per stream

A document can make one-stream retrieval direct, but growing histories create
large rewrites, document-size ceilings, and contention on one value. Atomic
append and version checks must still be proven. Global replay requires a
separate ordered log, so the document cannot replace event identity and order.

## Long and hot streams

The PostgreSQL integration suite appends 2,048 events to one stream in bounded
batches and reads them back in 127-event pages, proving no missing, duplicate,
or reordered stream version. The contention suite separately races writers on
one new stream and proves one optimistic winner while independent streams all
commit. These are correctness fixtures, not capacity claims.

Measure real aggregate distributions, including median, tail, and maximum
stream length. A hot stream is constrained by its stream-head lock even when
other streams remain parallel. Snapshots reduce reconstitution reads but do
not reduce append contention or make event deletion safe.

## Partitioning

The shipped migration creates an ordinary shared table; it does not silently
choose a PostgreSQL partition key. Converting it to a partitioned design is an
application-owned migration because aggregate locality, global ordering,
retention, and restore procedures determine the safe key.

Partitioning by recorded time can help bounded historical scans and archival,
but a single aggregate may cross partitions. Partitioning by tenant or a
stable routing key can improve locality, but global replay still needs a total
order and operational queries must remain bounded. Hash partitioning does not
remove hot-stream serialization.

Before deployment, prove on a production-shaped copy that:

1. every unique and foreign-key invariant remains enforceable;
2. stream and global reads prune or select indexes as intended;
3. concurrent append preserves one stream version sequence;
4. projection checkpoints cannot pass unavailable history; and
5. backup and restore preserve all partitions at one consistent recovery point.

## Retention and archive

Routine retention must not delete authoritative event rows merely because a
snapshot or projection exists. Before archival or deletion, identify legal,
privacy, replay, audit, and disaster-recovery requirements and record the
policy outside the library.

An archive operation must be separately named and audited. It must define
whether archived streams remain readable, how missing or archived state is
reported, how global positions behave, and how projections rebuild. The core
store contract intentionally does not pretend those application policies are
portable.

## Backup, restore, and repair

Back up `streams`, `positions`, and `messages` to one consistent PostgreSQL
recovery point. Snapshots and projection checkpoints are derived, but restoring
them inconsistently can make an application appear ahead of its history; either
restore them consistently or delete and rebuild them.

The pinned integration suite performs logical `pg_dump` and `pg_restore`,
compares event envelopes and derived state, and appends the next stream and
global versions. That proves the fixture's logical restore path, not managed
service failover, point-in-time recovery, encryption, or replica promotion.
Each deployment must drill those provider-specific paths.

History repair is exceptional. Preserve the original evidence, use a reviewed
and auditable procedure, never reuse a message ID for different content, and
re-run affected projections from a known checkpoint. Prefer compensating events
or rebuilding derived data over editing authoritative history.

## Capacity evidence

Record PostgreSQL version, schema, configuration, hardware, pool limits,
payload corpus, stream-length distribution, concurrency, durability settings,
sample counts, latency distributions, throughput, and allocations. Compare
equivalent durability and serialization work. An in-memory result or an
unordered insert is not a baseline for the committed ordered adapter.
