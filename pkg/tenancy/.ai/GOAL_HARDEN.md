# Goal Harden: `tenancy`

## Mission

Prove that `tenancy` fails closed, propagates identity without ambiguity, and
cannot leak state across tenants, goroutines, requests, pooled connections,
messages, caches, searches, workflows, or telemetry.

## Required Audit

1. Inventory every identity representation, context helper, extractor,
   injector, namespace encoder, persistence seam, adapter, analyzer, error,
   diagnostic, and administrative escape hatch.
2. Threat-model spoofed headers, confused deputies, malformed identifiers,
   conflicting sources, tenant enumeration, privilege promotion, stale pooled
   state, cache collisions, message replay, cross-tenant retries, and logs.
3. Prove context cancellation and deadlines survive propagation and that
   identity cannot be overwritten silently.
4. Verify HTTP/RPC proxy and service-to-service trust boundaries, including
   direct backend access and duplicate headers.
5. Prove PostgreSQL query, transaction, RLS, prepared-statement, connection
   pool, rollback, cancellation, and failover paths reset scope correctly.
6. Verify every first-party integration produces collision-free tenant
   namespaces and rejects absent or conflicting scope.
7. Exercise system-wide operations, support impersonation, imports, migrations,
   fan-out, partial failure, resume, and audit attribution.
8. Fuzz identifiers and wire metadata; race and stress concurrent reuse;
   leak-test every asynchronous adapter.
9. Run property and model-based tests against multiple tenants and randomized
   operation sequences, including failure injection between every boundary.
10. Review documentation and analyzers for dangerous implied guarantees or
    bypasses.

## Required Evidence

- exact 100% meaningful statement coverage and 100% viable mutation kills;
- PostgreSQL/RLS and connection-pool interoperability tests;
- HTTP, RPC, queue, event, cache, search, workflow, audit, and telemetry
  composition fixtures;
- race, fuzz, stress, soak, leak, and hostile-input results;
- latency/allocation benchmarks for context, namespace, and enforcement paths;
- security review and cross-tenant isolation test report;
- clean-consumer examples and migration guidance.

No isolation claim may rely only on unit mocks, code review, or a single happy
path. Every supported boundary requires executable negative proof.
