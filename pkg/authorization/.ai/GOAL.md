# Goal: Application-Oriented Authorization Engine

## Objective

Build a serious general-purpose open-source authorization engine supporting
ACL, RBAC, and ABAC for ordinary production applications through typed,
discoverable Go APIs.

The engine should retain the flexibility that makes broad policy engines useful
without adopting a Casbin-style meta-model, configuration language, positional
string tuples, or an API optimized for every theoretical authorization model.
Common application authorization MUST be straightforward to define, inspect,
test, persist, and explain.

## Product Principles

- Typed Go policy definitions are the primary interface.
- Secure defaults and comprehensible behavior take priority over policy-language
  cleverness.
- ACL, RBAC, and ABAC are first-class models with shared decision semantics.
- Tenant/domain scoping is built in rather than encoded through string
  conventions.
- Decisions are explainable and auditable without exposing sensitive context.
- Applications can use one model without understanding every other model.
- Business ownership and invariants remain in application/domain code unless
  intentionally supplied as typed attributes to an ABAC policy.

## Core Decision Model

- Subject, action, resource type, resource ID, tenant/domain, and environment.
- `Allow`, `Deny`, and `NotApplicable` outcomes with explicit combining rules.
- Default deny.
- Structured reason codes, matched-policy IDs, revisions, and evaluation trace.
- Batch decisions and partial evaluation for lists and query prefiltering where
  correctness can be guaranteed.
- Stable policy identities, revisions, activation windows, and metadata.
- Context-aware evaluation with strict cost and recursion bounds.

## ACL

- Direct subject-to-resource and subject-to-resource-type grants.
- User, service account, API key, group, and arbitrary typed subject kinds.
- Allow and explicit deny entries with documented precedence.
- Tenant-scoped and global entries with explicit inheritance rules.
- Efficient batch checks and resource listing without accidental full scans.

## RBAC

- Roles, permissions, subject-role assignments, and role inheritance.
- Tenant/domain-scoped assignments and permissions.
- Multiple roles, nested roles, cycle detection, and bounded inheritance depth.
- Resource-type and action permissions with optional resource constraints.
- Deterministic deny and priority behavior.
- Administrative APIs for assignment, revocation, inspection, and effective
  permission calculation.

## ABAC

- Typed subject, resource, request, and environment attributes.
- Programmatic predicates or a deliberately bounded expression representation;
  no arbitrary code execution or reflection-driven magic.
- Safe comparison, set membership, numeric/time, CIDR, and string operations.
- Explicit missing, null, invalid, and type-mismatch semantics.
- Cost budgets, nesting limits, deterministic evaluation, and no network or
  database I/O inside predicates.
- Reusable named conditions with versioned behavior.

## Policy Composition

- Explicit policies for deny-overrides, allow-overrides, first-applicable, and
  priority order.
- Model composition without converting every rule into positional string arrays.
- Policy validation before activation.
- Dry-run, explain, shadow-evaluation, and policy-diff support.
- Revisioned immutable snapshots so each decision evaluates one coherent policy
  view.

## Persistence And Distribution

- Storage-neutral repository and snapshot contracts.
- First-class PostgreSQL adapter with reviewed schema and migrations through
  `migrations`.
- Optional Valkey cache/invalidation adapter using native `valkey-go`.
- Transactional policy updates, monotonic revisions, optimistic concurrency,
  and deterministic reload.
- Multi-instance invalidation with polling fallback and no correctness reliance
  on lossy Pub/Sub alone.
- Import/export in a documented, versioned, human-reviewable format.

## Integration

- Consume `authentication` principals without making `authentication`
  depend on authorization.
- HTTP and JSON-RPC middleware/adapters that map decisions to transport errors.
- `log` audit events and `telemetry` metrics/traces with bounded labels.
- `cache` integration only through explicit adapters.
- Deterministic test engine, policy builders, fixtures, assertions, and decision
  snapshots.

## Non-Goals

- No authentication, credential validation, identity provider, or user lifecycle.
- No Casbin compatibility layer, Casbin model parser, or Casbin policy-file
  emulation.
- No general programming language, arbitrary plugin execution, or remote calls
  during evaluation.
- No attempt to implement every academic access-control model in v1.
- No automatic domain-object loading or ORM integration in the core.
- No authorization-as-a-service control plane in v1.

## Package Shape

- Root package: requests, decisions, policies, revisions, errors, engine.
- `acl`: typed ACL definitions and evaluator.
- `rbac`: roles, permissions, inheritance, and evaluator.
- `abac`: typed attributes, bounded predicates, and evaluator.
- `policy`: validation, composition, snapshots, diff, import/export.
- `postgres`: persistence adapter.
- `valkey`: cache and invalidation adapter.
- `authhttp` and `authrpc`: transport integration.
- `authorizationtest`: fixtures, assertions, and conformance tools.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Tests MUST prove
decision semantics, precedence, isolation, revisions, invalidation, persistence,
and hostile-policy behavior rather than merely execute lines.

Required verification includes:

- truth-table and property tests for every combining algorithm
- model conformance suites for ACL, RBAC, and ABAC
- cycle, recursion, cost, cardinality, and malformed-policy fuzzing
- race tests for concurrent evaluation, reload, update, cache, and invalidation
- real PostgreSQL and Valkey integration matrices
- differential snapshot tests across revisions and rolling deployments
- benchmarks for cold/warm decisions, batches, inheritance, predicates, reload,
  and policy sizes with documented limits
- mutation testing for security-critical decision branches

## Documentation Deliverables

- Complete API reference and five-minute ACL, RBAC, and ABAC quickstarts.
- Application-oriented guides for tenant roles, resource ACLs, attributes,
  explicit deny, ownership checks, API keys, services, and batch decisions.
- Policy modeling, precedence, explanation, persistence, revision, invalidation,
  migration, testing, performance, and operations guides.
- Decision trees explaining when to use ACL, RBAC, ABAC, or application code.
- Security model, threat model, FAQ, troubleshooting, compatibility, governance,
  examples, contribution guide, and maintained `CHANGELOG.md`.
- Documentation MUST make ordinary application use possible without learning a
  policy meta-language.

## Automation And Release

GitHub Actions MUST run formatting, vetting, linting, tests, exact meaningful
coverage, race tests, fuzz smoke tests, mutation checks, PostgreSQL and Valkey
integration matrices, vulnerability scans, benchmarks, docs, examples, and API
compatibility checks. Policy format compatibility is SemVer-governed.

## Execution Plan

1. Specify requests, decisions, snapshots, combining rules, limits, and errors.
2. Implement ACL and RBAC engines with in-memory storage and test tooling.
3. Implement bounded typed ABAC and policy composition/explanation.
4. Implement PostgreSQL persistence, Valkey invalidation, revisions, and import.
5. Complete hostile-policy, race, mutation, performance, and security hardening.
6. Publish complete application-oriented documentation and stable release.

## Acceptance Criteria

- ACL, RBAC, and ABAC are independently useful and coherently composable.
- Common tenant-role and resource-permission flows require concise typed Go, not
  a separate model language.
- Every decision is deterministic, bounded, explainable, revisioned, and
  default-deny.
- Distributed policy updates cannot silently produce indefinite stale access.
- Meaningful 100% coverage and every GitHub Actions gate pass.
- Documentation enables real-world adoption without source inspection.
- `CHANGELOG.md` and policy-format compatibility records are current.
