# Migrating ad hoc tenant fields

1. Inventory every tenant-bearing request, job, event, query, cache key,
   idempotency record, search document, workflow, log, metric, and admin path.
2. Parse legacy strings into `TenantID` at trusted boundaries. Reject invalid
   values; do not trim, case-fold, or silently map them.
3. Change application and persistence APIs to require `Scope` or `TenantID`.
   Add `AssertTenant` at consumer-owned enforcement seams.
4. Replace concatenated keys with `NamespaceEncoder` and dual-read only when a
   bounded migration needs it. Never fall back to an unscoped legacy key after
   a scoped miss.
5. Introduce explicit propagation on one authenticated hop at a time. Reject
   conflicting legacy and new sources.
6. Add PostgreSQL predicates or RLS using a non-owner application role, then
   test pool reuse and rollback before removing legacy guards.
7. Convert cross-tenant loops to `IterateTenants`, with audit and resume state.
8. Remove legacy discovery only after negative cross-tenant tests pass.

Namespace format v2 uses a `tn2_` prefix and lowercase hexadecimal output so
the same opaque namespace is accepted by first-party cache, queue, search,
workflow, and telemetry providers, including OpenSearch index names. Existing
`tn1_` URL-base64 keys do not alias v2 keys. Deploy a bounded dual-read from v2
to v1, write only v2, backfill or expire v1 state, and remove the v1 read only
after provider-specific negative isolation checks pass. A v2 miss MUST NOT
fall back to an unscoped or differently scoped legacy key.

During rollout, fail closed when new scope is required. A compatibility default
tenant hides missing propagation and can turn deployment mistakes into data
leaks.

The clean-consumer fixture executes an external-module rollout slice: an
authenticated HTTP hop, direct-backend and confused-deputy rejection, an
explicit PostgreSQL predicate, duplicate JSON-RPC rejection, and scoped cache
keys whose misses never fall back to a legacy or another tenant's key. It also
composes the first-party cache, search contract, queue/event, workflow, audit,
and telemetry providers and exercises durable administrative fan-out, partial
failure, idempotent resume, imports, migrations, and support attribution.

Earlier `RLSPlan` consumers that executed only `Create` or `Drop` must update
their migrations. `Create` is now the restrictive isolation half and requires
`CreateGrant` first; `Drop` removes only the restrictive half and requires
`DropGrant` first for fail-closed rollback. Treat the pair as one migration
unit. Code that executes only one field still compiles but no longer represents
a complete plan.
