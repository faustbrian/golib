# Goal: PostgreSQL Event Store

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Objective

Build `event-sourcing/postgres` as the durable PostgreSQL adapter for streams,
snapshots, projections, and caller-owned transactions. It MUST preserve the
core event-sourcing contracts while exposing PostgreSQL commit ambiguity and
optimistic concurrency honestly.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Append immutable messages with unique IDs, exact stream expectations, stable
  per-stream order, and monotonic global positions in short transactions.
- Distinguish not-committed from commit-unknown outcomes and provide documented
  reconciliation by durable identity before retry.
- Offer pool-owned operations and transaction-scoped staging that never commits
  or rolls back caller-owned transactions.
- Provide bounded, cancellable iterators with explicit row ownership and close.
- Store snapshots as replaceable derived acceleration data with exact-retry and
  regression/conflict detection.
- Store projection checkpoints and running/paused state with optimistic fencing;
  support atomic read-model/checkpoint staging in caller transactions.
- Ship embedded, forward-only, idempotent schema migrations without running them
  from constructors.
- Preserve envelope bytes and metadata exactly within core limits and canonical
  timestamp precision.

## Documentation And Completion

Document schema, indexes, locks, transaction ownership, isolation assumptions,
ordering, ambiguity recovery, backups, migrations, API, examples, adoption,
FAQ, compatibility, and operational tuning. CI MUST require PostgreSQL 14-18
integration, race, fuzz, security, API, docs, benchmarks, exactly 100%
statement coverage, and exactly 100% of viable mutants killed meaningfully.
