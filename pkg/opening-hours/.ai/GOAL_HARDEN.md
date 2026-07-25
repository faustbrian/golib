# Hardening Goal: Opening Hours And Availability Foundation

## Objective

Prove that `opening-hours` is correct, deterministic, immutable, bounded,
timezone-safe, interoperable, and panic-free across every daily range state,
overnight boundary, exception conflict, DST transition, parser input, query,
composition operation, and persistence round trip.

## Required Audits

### Schedule State Audit

- Exhaust inherited, empty, closed, full-day, single-range, multi-range,
  adjacent, overlapping, duplicate, collapsed, and overnight states.
- Verify zero values, constructors, copies, equality, canonical ordering, and
  metadata do not alter availability accidentally.
- Mutation-test every inclusion, exclusion, boundary, and normalization branch.
- Prove caller maps, slices, labels, and exception inputs cannot mutate values.

### Exception And Precedence Audit

- Exhaust replacement, closure, addition, subtraction, range, and exact-date
  exceptions under every conflict and priority combination.
- Prove precedence is stable regardless of insertion or map iteration order.
- Verify overnight spillover interaction with exceptions on both civil dates.
- Reject ambiguous conflicts unless an explicit documented policy resolves them.

### Timezone And Transition Audit

- Test DST gaps, folds, offset changes, midnight transitions, skipped dates,
  repeated local times, and historical timezone rules.
- Prove local-to-instant conversion always applies an explicit ambiguity policy.
- Differential-test applicable conversions against Go's timezone implementation.
- Verify next/previous transition and open-duration calculations around every
  discontinuity and search-horizon edge.

### Algebra And Query Audit

- Property-test normalization, overlay, union, intersection, subtraction,
  canonicalization, idempotence, conservation, and stable ordering.
- Prove `IsOpen`, daily ranges, next open/close/transition, previous transition,
  and bounded duration queries agree with the represented interval set.
- Attack range fragmentation and exception/output cardinality limits.
- Prove every search advances and terminates or returns a typed bounded result.

### Parsing And Persistence Audit

- Fuzz JSON, text, import builders, SQL, pgx, and optional interchange adapters
  with invalid UTF-8, duplicates, trailing data, oversized values, and precision
  boundaries.
- Prove accepted encodings round-trip without changing ranges, timezone,
  exceptions, precedence, precision, or effective dates.
- Validate legacy Track, Postal, Location, and Spatie compatibility fixtures.
- Reject lossy mappings unless the caller explicitly selects and observes them.

### Concurrency, Security, And Resource Audit

- Race/stress concurrent reads, compiled queries, encoders, and observation
  callbacks while proving immutable ownership.
- Enforce parse, range, exception, metadata, fragment, search, allocation, and
  output budgets under hostile input.
- Prove no hidden goroutine, global registry, unbounded cache, callback deadlock,
  sensitive telemetry label, unsafe, cgo, or `go:linkname` remains.
- Leak-test optional indexed or cached resources and callback failure paths.

## Required Deliverables

- Formal range-state, overnight, exception-precedence, and DST behavior tables.
- Legacy compatibility and persistence round-trip matrices.
- Fuzz, mutation, race, leak, timezone, PostgreSQL, and benchmark evidence.
- Enforced resource budgets and worst-case complexity documentation.
- Updated API, adoption, migration, security, performance, FAQ, and
  troubleshooting documentation.

## Release Blockers

- Any incorrect open/closed result, missed or duplicate range, unstable
  precedence, timezone ambiguity, non-terminating search, silent lossy mapping,
  mutation, race, deadlock, leak, panic, or unbounded expansion.
- Any provider-specific business policy hidden in the generic core.
- Any exported behavior not documented with its boundaries and failure modes.
- Missing meaningful 100% coverage, mutation evidence, or green blocking CI.

## Completion Criteria

- State, precedence, algebra, timezone, parsing, persistence, and migration
  suites pass.
- Searches and composition survive enforced limits and adversarial stress.
- Race, fuzz, mutation, leak, vulnerability, compatibility, and performance
  gates pass.
- NilAway runs visibly as advisory without blocking findings.
- No release blocker remains and the changelog is current.
