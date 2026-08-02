# Changelog

## Unreleased

### Added

- Generic immutable outer-to-inner policy composition with explicit logical
  and physical-attempt scopes.
- Typed common outcomes, stable error categories, bounded metadata, attempt
  lineage, timelines, and panic-safe observers.
- Caller-owned total-context enforcement that prevents custom policies from
  extending or detaching deadlines without moving operations to goroutines.
- Process-local retry-plus-hedge work budgets with per-execution, concurrent,
  rolling-window, resource-cardinality, expiry, and exact-completion bounds.
- Exact statement coverage, mutation, race, fuzz, model, lifecycle, benchmark,
  API, security, documentation, and clean-consumer gate definitions.
