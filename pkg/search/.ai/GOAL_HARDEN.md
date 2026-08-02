# Goal Harden: `search`

## Mission

Audit shared search semantics, indexing correctness, pagination, migrations,
tenant isolation, failure classification, resource bounds, and rebuildability
before any adapter is considered production-ready.

## Required Audit

1. Inventory every query node, document value, cursor, bulk outcome, capability,
   migration operation, extension, limit, error, and adapter contract.
2. Threat-model query injection, expensive queries, tenant leakage, cursor
   tampering, response bombs, mapping explosion, field disclosure, poisoned
   documents, and backend credential leakage.
3. Prove unsupported features fail explicitly and no adapter silently changes
   query, sort, null, scoring, refresh, version, or pagination semantics.
4. Verify cursor-query binding, stable ordering, expiry, point-in-time loss,
   duplicate/missing hits, concurrent writes, and bounded traversal.
5. Exercise bulk partial failures, ambiguous outcomes, retries, duplicate
   events, stale versions, source deletion, reconciliation, and rebuild.
6. Verify index-definition fingerprints, migrations, resumability, alias
   cutover, rollback, mixed application versions, and cleanup safeguards.
7. Prove tenant scoping across indexing, querying, aliases, cursors, metrics,
   caches, and administrative operations.
8. Run hostile query/document fuzzing, race, leak, stress, soak, fault
   injection, and strict CPU/memory/network bounds.
9. Compare the in-memory fake and production adapters only for declared shared
   semantics; document every intentional fidelity limit.
10. Review APIs and docs for backend leakage, unstable contracts, and unsafe
    convenience defaults.

## Required Evidence

- adapter conformance suite and real-backend fixtures;
- exact 100% meaningful statement coverage and 100% viable mutation kills;
- race, fuzz, leak, stress, soak, outage, and malformed-response results;
- rebuild, reconciliation, migration, cutover, rollback, and restore exercises;
- comparable indexing/query/pagination benchmarks with equivalent semantics;
- security, tenancy, operations, docs, and clean-consumer review.

No test fake, mock transport, or successful HTTP status may substitute for
real-backend semantic proof.
