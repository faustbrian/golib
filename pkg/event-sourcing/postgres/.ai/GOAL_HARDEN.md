# Goal Harden: PostgreSQL Event Store

## Mission

Prove durable ordering, concurrency, reconciliation, and resource safety across
database failures, process death, and rolling deployment.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Model every stream, append, transaction, iterator, snapshot, projection, and
  migration state plus all not-committed/unknown/committed transitions.
- Race concurrent expected-version appends, duplicate message IDs, snapshot
  writers, checkpoint writers, pause/resume/reset, and shared transactions.
- Kill connections and PostgreSQL processes before, during, and after statement
  execution and commit; prove reconciliation never creates duplicate events.
- Test deadlocks, serialization failures, lock timeout, statement timeout,
  cancellation, failover, pool exhaustion, slow readers, and partial iteration.
- Verify exact plans and indexes with realistic stream/global read volumes;
  detect table scans, lock amplification, sequence contention, and bloat.
- Test migrations from every supported prior schema, concurrent deployment jobs,
  rollback limitations, PostgreSQL 14-18, backup/restore, and extension absence.
- Fuzz envelopes, metadata, identifiers, bounds, timestamps, iterator options,
  and driver errors; assert redaction and bounded allocation.
- Exercise SIGTERM and Kubernetes pod replacement while appending, projecting,
  and draining; document duplicate windows and readiness behavior.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed by meaningful tests with no unresolved durability, race,
migration, performance, or compatibility finding.
