# Changelog

## Unreleased

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

### Added

- Generic immutable outer-to-inner policy composition with explicit logical
  and physical-attempt scopes.
- Typed common outcomes, stable error categories, bounded metadata, attempt
  lineage, timelines, and panic-safe observers.
- Caller-owned total-context enforcement that prevents custom policies from
  extending or detaching deadlines without moving operations to goroutines.
- Process-local retry-plus-hedge work budgets with per-execution, concurrent,
  rolling-window, resource-cardinality, expiry, and exact-completion bounds.
- Context-coordinated physical-attempt admission for focused retry and hedge
  executors, including unique ordinals and nested-attempt reuse.
- Exact statement coverage, mutation, race, fuzz, model, lifecycle, benchmark,
  API, security, documentation, and clean-consumer gate definitions.
