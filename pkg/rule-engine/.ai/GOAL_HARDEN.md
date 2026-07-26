# Hardening Goal: Rule Engine

## Objective

Prove parser, compiler, operators, evaluation, chaining, caching, diagnostics,
and extension seams deterministic and safe under hostile rules and facts.

## Required Audits

- Exhaust every operator across compatible, incompatible, missing, null, zero,
  boundary, Unicode, numeric, temporal, and collection values.
- Verify no undocumented coercion or truthiness.
- Property-test logical laws, canonicalization, stable hashing, short-circuit
  order, priorities, and conflict strategies.
- Attack deep/wide ASTs, regexes, huge collections, path traversal, cycles,
  forward-chaining loops, diagnostic amplification, and cache floods.
- Fuzz JSON AST and every optional DSL; differential-test parser/serializer
  round trips.
- Race compiled plans, contexts, caches, and custom operators.
- Mutation-test every type guard, operator, combinator, ordering, limit, and
  error classification.
- Prove integrations cannot silently turn rule errors into authorization or
  feature enablement.

## Release Blockers

- Wrong decision, nondeterminism, type confusion, arbitrary execution, hidden
  I/O, unbounded evaluation, cache poisoning, sensitive fact disclosure, race,
  or fail-open integration.

## Completion Criteria

- Truth-table, property, hostile, fuzz, race, mutation, differential, and
  benchmark suites pass with meaningful 100% coverage.
