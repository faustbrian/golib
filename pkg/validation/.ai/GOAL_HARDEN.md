# Hardening Goal: Typed Application Validation

## Objective

Prove that `validation` remains deterministic, panic-free, bounded,
non-mutating, and secret-safe across hostile values, deep object graphs,
concurrent plan use, custom validators, and every transport projection.

## Required Audits

### Semantic Audit

- Exhaustively test missing, null, empty, zero, malformed, prohibited, and valid
  states for every applicable type.
- Mutation-test all branches that can suppress or misplace a violation.
- Prove collect-all output is stable regardless of map iteration or concurrency.
- Prove composition truth tables and short-circuit behavior.
- Verify cross-field paths and conditional rules cannot read the wrong value.

### Struct And Reflection Audit

- Fuzz tag grammar, embedded fields, aliases, pointers, interfaces, maps,
  slices, arrays, generics, inaccessible fields, and malformed plans.
- Detect cycles and enforce depth, field-count, tag-length, and cache bounds.
- Race-test first compilation, cache hits, concurrent validation, and teardown.
- Prove reflective and generated paths are behaviorally identical if both ship.

### Hostile Input And Resource Audit

- Fuzz Unicode, invalid UTF-8, huge strings, large collections, deep nesting,
  numeric edges, non-finite floats, dates, URLs, networks, and identifiers.
- Enforce violation-count, metadata, path, regex, memory, and runtime budgets.
- Prove cancellation and deadlines for context-aware I/O validators.
- Verify caller-owned maps, slices, pointers, and custom values never mutate.

### Security And Projection Audit

- Threat-model secret disclosure, validation bypass, path confusion, rule-code
  collision, log injection, denial of service, and custom-validator panic.
- Verify all errors and observations remain safe for passwords and tokens.
- Prove JSON-RPC, JSON:API, and HTTP projections preserve exact locations,
  stable codes, ordering, and escaping.
- Ensure translation cannot change machine semantics or inject unsafe output.

## Required Deliverables

- Normative semantic tables for presence, composition, paths, and aggregation.
- Threat model, resource-budget table, and hardening findings report.
- Fuzz corpus, mutation report, race evidence, and transport conformance matrix.
- Benchmark baselines for representative and hostile validation plans.
- Updated API, security, performance, migration, FAQ, and troubleshooting docs.

## Release Blockers

- Any validation bypass, wrong field path, non-deterministic report, mutation of
  caller data, secret leak, panic, race, deadlock, or unbounded input behavior.
- Any tag or reflection behavior that differs silently from typed validation.
- Any transport projection that loses violation identity or location.
- Missing meaningful 100% coverage, mutation evidence, or green blocking CI.

## Completion Criteria

- Every semantic state and standard rule has mutation-resistant evidence.
- Hostile, nested, reflective, concurrent, and projection suites pass.
- Race, fuzz, vulnerability, compatibility, and performance gates pass.
- NilAway runs as a visible advisory check without blocking findings.
- No release blocker remains and the changelog is current.
