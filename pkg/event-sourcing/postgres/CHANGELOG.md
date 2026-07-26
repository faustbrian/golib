# Changelog

All notable changes to this module are documented here.

## Unreleased

### Added

- real PostgreSQL server-restart and streaming-replica promotion evidence
  preserving history plus the next stream and global positions
- real PostgreSQL long-stream evidence for bounded pagination across 2,048
  sequential events
- real PostgreSQL schema-contract evidence for envelope constraints, the
  complete message index set, and indexed stream and global read plans
- real PostgreSQL contention evidence for one optimistic hot-stream winner,
  independent-stream success, unique positions, and gap-free global read order
- real PostgreSQL lock-timeout evidence proving a blocked append is not
  committed and can be retried after the locking transaction rolls back
- real PostgreSQL backend-termination evidence proving the existing pool can
  replace a lost connection and resume reads and ordered appends without
  losing durable history
- logical `pg_dump` and `pg_restore` evidence preserving event history,
  snapshots, projection checkpoints, stream heads, and global ordering
- public committed event-store conformance against a real PostgreSQL 18
  database
- public optional global-reader conformance against a real PostgreSQL 18
  database
- atomic PostgreSQL stream append with optimistic concurrency and duplicate
  message-ID protection
- bounded stream and global-position reads with caller-closed iterators
- caller-owned transaction composition through `NewTx`
- transactional global-position allocation that preserves committed ordering
  under concurrent writers
- reversible, engine-neutral schema migration and real PostgreSQL lifecycle
  coverage
- durable snapshot storage with atomic non-regression, exact-retry, conflict,
  deletion, and corrupt-input semantics
- durable projection checkpoints with pause, resume, optimistic advancement,
  expected reset, and same-transaction application read-model composition
- redacted unknown-commit classification for pool-owned snapshot and
  checkpoint transactions
