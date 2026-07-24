# Changelog

All notable changes to this project are documented in this file.

The format is based on Keep a Changelog, and this project adheres to Semantic
Versioning.

## [Unreleased]

### Added

- An explicit request-target byte budget enforced before dispatch matching.
- Blocking architecture checks for production goroutines and process-global
  HTTP registration.
- Explicit validated route descriptors with typed bounded errors and limits.
- Immutable `ServeMux`-backed compilation, standard path values, host patterns,
  deterministic method handling, and safe route introspection.
- Transactional nested groups and visible router, group, route, and mount
  middleware composition.
- Safe named path and absolute URL generation with typed remainder segments.
- Explicit handler mounts, automatic OPTIONS control, redirect policy, and
  customizable 404 and 405 handlers.
- `routertest` consumer helpers, differential compatibility fixtures, fuzzing,
  race tests, mutation checks, benchmarks, and full adoption documentation.
- Pinned local and GitHub Actions release gates with signed-tag verification and
  build provenance attestations.
- Expanded conflict, path-value, middleware, mount, and URL-security truth
  tables with executable panic, cancellation, and partial-write evidence.
- Expanded standard-library differential coverage across all standard method
  classes and nested group fuzz properties.
- Documented migration guidance for every unsupported `ServeMux` pattern and
  resource-boundary difference.
- Recorded the complete local release-gate results and refreshed the measured
  performance baseline.
- Proved malformed requests bypass custom miss handlers and route middleware.

### Fixed

- Bound middleware identifiers, exclusions, mount prefixes, schemes, route
  name lookups, and request methods before expensive work.
- Bound diagnostic input before UTF-8 normalization and control sanitization.
- Accept the complete `ServeMux` wildcard identifier set during named-route
  generation.
- Preserve `ServeMux` canonical redirects before route and method miss
  classification.
- Strip encoded literal mount prefixes in decoded path space while preserving
  the escaped suffix for mounted handlers.
- Convert only controlled `ServeMux` registration errors while allowing
  runtime faults to propagate.
- Reject slash-only generated wildcard values that cannot round-trip through
  `ServeMux` as one segment.
- Reject middleware chains that resolve to a nil handler during compilation.
- Reject unsupported CONNECT routes and bound documentation, empty-group
  metadata, and trusted absolute-URL authorities during startup.
- Bound method tokens, wildcard identifiers, URL parameter input, and raw
  query input before allocation-heavy parsing or encoding.
- Count host and path wildcards together against the per-route budget.
- Reject oversized route collection fields before copying caller-owned values,
  including middleware exclusion lists.
- Bound the total segment values supplied to remainder URL parameters.
- Reject remainder constructors above a fixed segment ceiling without copying
  the oversized caller slice.
- Validate router-wide middleware against the final option limits before
  registration while retaining defensive copies and option-order independence.
- Validate the complete `ServeMux` pattern set before constructing any
  middleware, keeping conflict failures free of partial handler graphs.
- Apply named inherited middleware exclusions to group layers as well as
  router-wide layers.
- Enforce path and name budgets on composed nested groups even when a group
  callback registers no routes.
- Propagate remaining route and group capacity into child builders and reject
  exhausted group counts before invoking another callback.
- Return `ErrLimitExceeded` consistently for syntactically valid route names,
  hosts, paths, and group prefixes that exceed configured byte budgets.
- Evaluate rejected redirects against escaped paths so encoded separators and
  dot text remain wildcard data instead of false canonicalization misses.
- Match rejected subtree roots with standard patterns across wildcard, Unicode,
  percent-escape, exact-root, and implied HEAD semantics.
- Sanitize rendered diagnostics to bounded single-line valid UTF-8 without
  splitting multibyte characters.
- Align default 404 and non-automatic 405 responses with `http.ServeMux`.
- Document and freeze every dispatch difference caused by automatic OPTIONS,
  unsupported method misses, host extensions, redirect policy, and CONNECT.
