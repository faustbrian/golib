# Hardening Goal: Authorization Engine

## Objective

Prove that `authorization` is fail-closed, deterministic, bounded, explainable,
tenant-safe, and consistent under hostile policies, concurrent updates,
distributed caches, and rolling deployments.

## Required Audits

### Decision Semantics

- Exhaustively test allow, deny, not-applicable, default, priority, and combining
  behavior for single and composed models.
- Mutation-test every branch that can turn deny into allow.
- Prove explain output matches the actual decision without changing evaluation.
- Verify batch decisions are equivalent to independent decisions.

### ACL And RBAC

- Test global, tenant, resource-type, and resource-instance scopes.
- Exercise conflicting grants, explicit denies, duplicate assignments, role
  inheritance, diamonds, cycles, deep graphs, deletion, and revocation.
- Prove tenant assignments and caches cannot cross domains.
- Bound traversal, effective-permission expansion, and resource listing.

### ABAC

- Fuzz types, missing/null values, Unicode, large collections, numeric edges,
  time zones, CIDRs, nesting, and malformed predicates.
- Prove deterministic cost budgets and fail-closed budget exhaustion.
- Audit every operator for type confusion, coercion surprises, panics, and
  non-determinism.
- Ensure predicates cannot perform I/O, mutate inputs, or execute arbitrary code.

### Persistence And Distribution

- Test transaction failure at every policy and revision write boundary.
- Prove snapshot atomicity, monotonic revisions, optimistic concurrency, and
  rollback behavior.
- Exercise lost invalidations, duplicate events, reordering, Valkey outage,
  PostgreSQL failover, reconnects, stale caches, and rolling versions.
- Define maximum stale-policy windows and fail-closed behavior for sensitive
  revocations.
- Verify migration and serialized policy compatibility across releases.

### Security And Resource Safety

- Threat-model confused deputy, tenant escape, privilege escalation, policy
  injection, denial of service, audit forgery, and sensitive attribute leakage.
- Bound policy count, graph depth, predicate cost, trace size, batch size, cache,
  reload concurrency, and diagnostic output.
- Race-test policy updates, evaluations, cache invalidation, and shutdown.
- Verify no policy or explanation data leaks secrets through logs or telemetry.

## Required Deliverables

- Formal decision and precedence tables for ACL, RBAC, ABAC, and composition.
- Threat model, tenant-isolation proof matrix, and findings report.
- Mutation, fuzz, race, failure-injection, PostgreSQL, and Valkey evidence.
- Versioned compatibility corpus for policies, snapshots, and migrations.
- Benchmark baselines and enforced resource budgets.
- Updated API, modeling, operations, security, migration, FAQ, and
  `CHANGELOG.md` documentation.

## Release Blockers

- Any authorization bypass, tenant escape, non-deterministic decision, stale
  revocation beyond contract, or decision/explanation mismatch.
- Unbounded policy evaluation, graph traversal, cache, trace, batch, or reload.
- Any race, deadlock, panic, corrupted snapshot, or partial policy activation.
- Any hidden Casbin-style string meta-model that bypasses typed validation.
- Missing meaningful 100% coverage, mutation evidence, or a green GitHub Actions
  gate.

## Completion Criteria

- Every decision rule has exhaustive, mutation-resistant evidence.
- Tenant, persistence, invalidation, rolling-upgrade, and hostile-policy suites
  pass.
- Race, fuzz, vulnerability, compatibility, and performance gates pass.
- No release blocker remains and `CHANGELOG.md` is current.
