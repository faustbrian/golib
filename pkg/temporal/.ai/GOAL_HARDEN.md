# Hardening Goal: Temporal Period And Interval Algebra

## Objective

Prove that `temporal` is mathematically correct, deterministic, immutable,
bounded, interoperable, and panic-free across every bound combination, interval
relation, set operation, circular midnight case, parser input, arithmetic edge,
PostgreSQL mapping, and PHP compatibility fixture.

## Required Audits

### Bounds And Relation Audit

- Exhaust all endpoint orderings and four-by-four bound combinations for every
  Allen relation and convenience predicate.
- Prove relations are mutually coherent, inverses match, and exhaustive cases
  cannot fall through silently.
- Mutation-test every equality, comparison, inclusion, and boundary branch.
- Verify empty, singleton, reversed, adjacent, and identical endpoint behavior.

### Set Algebra Audit

- Property-test intersection, union, merge, subtraction, difference, complement,
  gap, normalization, idempotence, and applicable commutative/associative laws.
- Prove output is stably ordered, normalized, disjoint where contracted, and
  conserves the represented set.
- Attack input/output cardinality and interval-fragment explosion.
- Verify immutable operations never alias mutable caller state.

### Iteration And Arithmetic Audit

- Fuzz zero, negative, minimum, maximum, overflowing, fractional, and excessive
  steps and durations.
- Prove forward/backward iteration and splitting always progress and terminate
  within configured limits.
- Distinguish fixed duration from calendar movement in every adapter.
- Test monotonic-bearing `time.Time`, serialized instants, and location changes.

### Time-Of-Day And Circular Audit

- Exhaust midnight, day-end, full-day, collapsed, ordinary, and wrapping
  intervals under every bound mode.
- Prove complement, intersection, union, difference, shift, split, and step
  behavior around midnight.
- Mutation-test wrapping, precision, rounding, and day-boundary decisions.
- Verify DST only enters when applying local values through `calendar`.

### Parsing And Persistence Audit

- Fuzz ISO 8601, ISO 80000, Bourbaki, JSON, text, SQL, range, and multirange
  encodings with invalid UTF-8, trailing data, huge values, and precision edges.
- Prove every accepted encoding round-trips without bound or precision loss.
- Reject or explicitly classify PostgreSQL mappings that cannot preserve
  semantics.
- Enforce parser, sequence, output, iteration, and allocation budgets.

### PHP Compatibility And Deferred Gap Audit

- Generate differential fixtures for every non-chart public PHP behavior.
- Classify every intentional Go divergence with rationale and migration guidance.
- Inventory all `Period\Chart` behavior and verify documentation consistently
  marks temporal charting unsupported and deferred.
- Ensure no release or README claims complete PHP compatibility while that gap
  remains.

## Required Deliverables

- Formal bound/relation truth tables and set-algebra property report.
- PHP compatibility matrix and classified divergence report.
- Deferred temporal-chart capability inventory and roadmap entry.
- Fuzz, mutation, race, PostgreSQL, overflow, and benchmark evidence.
- Resource budgets and updated API, migration, security, and FAQ documentation.

## Release Blockers

- Any wrong relation, lost or invented set member, non-normalized contracted
  output, infinite iteration, overflow, bound corruption, parser ambiguity,
  lossy unmarked persistence, mutation, race, panic, or unbounded expansion.
- Any silent confusion between fixed and calendar duration.
- Any false claim of complete PHP compatibility while charting is deferred.
- Missing meaningful 100% coverage, mutation evidence, or green blocking CI.

## Completion Criteria

- Bound, relation, algebra, circular, parsing, persistence, and compatibility
  suites pass.
- Every iterator and set operation satisfies enforced resource limits.
- Race, fuzz, mutation, vulnerability, interoperability, and performance gates
  pass.
- NilAway runs visibly as advisory without blocking findings.
- Temporal charting remains a documented future gap with a viable extension seam.
- No release blocker remains and the changelog is current.
