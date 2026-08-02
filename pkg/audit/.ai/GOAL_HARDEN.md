# Goal Harden: `audit`

## Mission

Perform an adversarial correctness, immutability, privacy, durability,
integrity, concurrency, and operations audit of `audit` and every first-party
adapter. Resolve every justified finding before release.

## Required Audit

1. Inventory every public API, record field, sink, adapter, schema, migration,
   canonical encoding, retention path, export path, integrity primitive,
   diagnostic, metric, and dependency.
2. Build a threat model covering forged actors, tenant confusion, field
   injection, secret leakage, record omission, duplication, reordering,
   truncation, backdating, tampering, compromised writers, compromised readers,
   storage outage, disk exhaustion, and malicious exports.
3. Prove no mutation is possible through retained maps, slices, pointers, or
   adapter aliasing after validation or append.
4. Prove fail-open, fail-closed, and durable-buffer policies are explicit and
   cannot accidentally degrade into silent loss.
5. Verify record IDs, ordering, timestamps, correlation, delegation, anonymous
   actors, deleted identities, and tenant scope under retries and concurrency.
6. Verify canonicalization and integrity chains against independent fixtures;
   test key rotation, checkpoints, missing links, reordered records, duplicate
   records, altered records, partial archives, and restored backups.
7. Audit redaction before every persistence, error, log, trace, test artifact,
   export, panic, and metric boundary.
8. Exercise PostgreSQL deadlocks, serialization failures, failover, ambiguous
   commits, migration interruption, partition rollover, restore, retention,
   legal hold, and reconciliation.
9. Stress bounded batches, streaming queries, slow consumers, cancellation,
   shutdown, large tenants, high-cardinality subjects, and concurrent writers.
10. Verify least-privilege deployment and demonstrate that ordinary writers
    cannot alter or erase accepted records.

## Required Evidence

- deterministic golden records and independent integrity verification;
- PostgreSQL version matrix required by repository policy;
- race, leak, fuzz, stress, soak, fault-injection, and resource-bound results;
- exact 100% meaningful statement coverage and 100% viable mutation kills;
- benchmarks for append, batch append, filtered pagination, export, redaction,
  and integrity verification with equivalent competitor or baseline behavior;
- backup/restore and mixed-version rolling-deployment exercises;
- security review of algorithms, key handling, privacy, and diagnostic output;
- complete docs, examples, migration notes, and clean-consumer proof.

No skipped, warning-only, unavailable, or flaky mandatory gate may be called a
pass. Every exclusion MUST name its owner, reason, risk, and expiry.
