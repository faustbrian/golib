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

During rollout, fail closed when new scope is required. A compatibility default
tenant hides missing propagation and can turn deployment mistakes into data
leaks.
