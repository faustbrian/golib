# Goal: Deterministic General-Purpose Rule Engine

## Objective

Build `rule-engine` as a deterministic, typed, inspectable engine for
evaluating facts and propositions. It MUST support Location's rule use cases
without absorbing authorization, feature flags, validation, or workflow
orchestration.

## Core Model

- Immutable fact context with explicit paths, value types, ownership, and
  missing/null distinctions.
- Stable rule identifiers, rules, rule sets, priorities, tags, namespaces, and
  conflict strategies.
- Typed operands, variables, literals, propositions, logical combinators, and
  comparison/membership/string/numeric/time/collection operators.
- Compile phase producing an immutable execution plan and diagnostics.
- Evaluation phase producing decision, matched rules, bounded explanation,
  errors, duration, and optional derived facts.
- Deterministic short-circuit and ordering semantics.
- Forward chaining only with explicit iteration bounds and cycle detection.
- Canonical serialization and hashing for stored rule definitions.

## Extensibility

- Small typed operator and fact-resolver contracts.
- Optional DSL modules for JSON AST first; JMESPath, SQL-like, or GraphQL-like
  inputs only with exact documented grammars and security limits.
- Optional cache interface for compiled immutable plans.
- `math`, `temporal`, and `measurement` adapters for exact domain
  values without forcing dependencies on core consumers.
- `authorization` and `feature-flags` MAY adapt the engine, but retain
  their own domain semantics and fail-closed policies.

## Boundaries

- No roles, permissions, access-control defaults, feature rollout semantics,
  validation error model, side effects, database queries, or action execution.
- Rules evaluate supplied facts; they do not discover models or call arbitrary
  methods through reflection.
- No `eval`, dynamic Go loading, embedded JavaScript, global operator registry,
  or hidden I/O.
- Formula/arithmetic expression execution remains separately scoped unless
  needed as a bounded typed operator module.

## Security And Bounds

- Bound rules, AST depth, operands, collection sizes, strings, regex cost,
  iterations, derived facts, diagnostics, cache, and evaluation time.
- Context cancellation during material compilation/evaluation.
- Reject duplicate IDs, unknown operators, type confusion, cycles, ambiguous
  coercion, NaN/infinity policy violations, and oversized paths before work.
- Never include sensitive fact values in logs or diagnostics by default.

## Verification And Documentation

Require meaningful 100% production coverage, operator truth tables, compiler
and evaluator properties, canonicalization, deterministic ordering, DSL
fixtures, fuzzing, race, mutation, complexity, cancellation, and benchmarks
against maintained rule engines with equivalent behavior.

Document model, operators, types/coercion, compilation, evaluation, rule sets,
extensions, DSLs, limits, security, performance, migration from Shipit/Cline
Ruler, integration, cookbook, FAQ, compatibility, and changelog. CI/local gates
follow ecosystem standards.

## Acceptance Criteria

- Location rules can migrate without business-semantic drift.
- Compiled rules are immutable, deterministic, bounded, and concurrency safe.
- Authorization and feature flags remain distinct products.
- Meaningful 100% coverage and every blocking gate pass.
