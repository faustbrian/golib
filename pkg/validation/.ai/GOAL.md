# Goal: Typed Application Validation

## Objective

Build a serious, transport-neutral validation package for Go applications that
can replace the consistent parts of Laravel validation and attribute-driven DTO
validation without recreating a reflection-heavy framework.

The package MUST make validation explicit, deterministic, composable, safe for
untrusted input, and straightforward to project into JSON-RPC, JSON:API, HTTP,
queue, configuration, and domain-boundary errors.

## Product Principles

- Typed validators and ordinary Go functions are the primary API.
- Validation reports MUST preserve stable field paths and machine-readable
  rule codes without exposing rejected secret values.
- Missing, null, empty, zero, malformed, and invalid values MUST remain
  distinguishable where the caller's model distinguishes them.
- Validation MUST be deterministic and side-effect free.
- Reflection and struct tags MAY be optional conveniences, never the only API.
- Domain invariants remain constructors and domain behavior; this package owns
  reusable input and value validation mechanics.

## Core Model

- Generic `Validator[T]` and function adapters.
- Immutable validation context carrying locale, operation, and bounded metadata.
- Structured violations with path, code, parameters, severity, and safe cause.
- Ordered validation reports with deterministic aggregation and deduplication.
- Field, item, key, index, and nested-object path segments.
- Explicit short-circuit and collect-all execution modes.
- Stable sentinel and typed errors compatible with `errors.Is` and `errors.As`.

## Required Validation Capabilities

- Required, prohibited, present, omitted, empty, and zero-value semantics.
- String length, Unicode-aware length, pattern, prefix, suffix, and membership.
- Numeric ranges, comparison, finite-number, precision, and multiple-of checks.
- Collection size, uniqueness, item validation, key validation, and nesting.
- Time, duration, date, ordering, and interval checks with explicit clocks.
- Cross-field equality, ordering, conditional requirement, and exclusion.
- URL, hostname, IP, CIDR, email syntax, UUID, and identifier primitives where
  standards-based behavior can be bounded and documented.
- Composition using all, any, not, conditional, and dependent validators.
- Custom typed validators with panic isolation only where explicitly enabled.
- Asynchronous or I/O validation MUST use a separate context-aware contract and
  MUST NOT be hidden inside ordinary deterministic validators.

## Struct Integration

- Optional typed struct plans may map fields to validators without requiring a
  global registry.
- Optional tags MUST have a documented grammar, startup compilation, duplicate
  detection, and strict unknown-rule rejection.
- Tag plans MUST be cacheable, immutable, bounded, and safe under concurrency.
- Embedded structs, pointers, optionals, maps, slices, aliases, and generics
  MUST have explicit semantics.
- Cycles, excessive depth, inaccessible fields, and unsupported kinds MUST fail
  predictably rather than panic or recurse indefinitely.
- Code generation MAY be offered if benchmarks prove it materially improves
  safety or performance; generated and reflective paths require conformance.

## Error Projection And Integration

- Adapters for JSON-RPC invalid-params errors and stable data payloads.
- Adapters for JSON:API error objects and source pointers.
- HTTP problem/error projection without requiring a router.
- `config` integration through its small validation contract.
- `service` middleware hooks without owning transport routing.
- Optional `log` and `telemetry` observation with bounded, non-sensitive
  labels.
- Translation hooks consume message catalogs supplied by applications; core
  MUST NOT embed application-facing prose as its semantic contract.

## Security And Resource Bounds

- Explicit maximum depth, collection size, violation count, path length,
  metadata size, regex cost, and custom-validator concurrency.
- No rejected secret, password, token, payload, or full object in errors, logs,
  traces, metrics, or default formatting.
- Regular expressions MUST be precompiled and bounded by Go's RE2 behavior.
- Caller-owned values MUST NOT be mutated.
- Panics from package-owned validation are release blockers.

## Non-Goals

- No ORM, request binder, serializer, dependency injection container, or DTO
  framework.
- No database uniqueness or existence rules in core.
- No business-specific country, postal, carrier, address, or authorization
  policy.
- No automatic coercion or normalization disguised as validation.
- No Laravel rule-string compatibility layer.
- No global singleton validator.

## Package Shape

- Root: validators, violations, reports, paths, composition, limits, errors.
- `rules`: reusable typed standard rules.
- `structplan`: optional compiled struct and tag integration.
- `validationhttp`, `validationrpc`, and `validationjsonapi`: projections.
- `validationtest`: fixtures, assertions, conformance, and mutation helpers.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Tests MUST prove
semantics and defect detection rather than merely execute lines.

Required evidence includes:

- truth tables for presence, null, empty, and zero semantics
- table and property tests for every standard validator
- Unicode, nesting, cycle, tag grammar, path, and malformed-input fuzzing
- mutation testing of required, comparison, composition, and projection logic
- race tests for compiled plans and concurrent validation
- differential tests between reflective and generated paths if both exist
- benchmarks for scalar, nested, collection, collect-all, and hostile workloads
- allocation and maximum-resource budgets

## Documentation Deliverables

- Five-minute quickstart using typed validators.
- Complete API reference and rule catalog.
- Guides for structs, composition, cross-field rules, async validation, errors,
  localization, JSON-RPC, JSON:API, HTTP, config, and custom validators.
- Adoption guide from Laravel validation and `cline/struct` validation.
- Security model, performance guide, FAQ, troubleshooting, compatibility,
  migration, examples, and maintained changelog.
- Every exported API and realistic user-facing scenario MUST be documented.

## Automation And Release

Use the latest stable Go release as the minimum version at implementation time.
Pin all tools and dependencies. GitHub Actions MUST run formatting, vet,
Staticcheck, strict golangci-lint, advisory NilAway, tests, meaningful 100%
coverage, race tests, fuzz smoke, mutation checks, vulnerability scans,
benchmarks, documentation examples, API compatibility, and release automation.
All blocking CI commands MUST be reproducible locally through documented
`make` targets.

## Execution Plan

1. Specify value presence, paths, reports, errors, composition, and limits.
2. Implement typed scalar, collection, temporal, and cross-field validators.
3. Implement optional compiled struct plans and transport projections.
4. Complete fuzz, mutation, race, resource, and performance hardening.
5. Publish full adoption and API documentation and release v1.

## Acceptance Criteria

- Validation semantics are explicit, deterministic, typed, and bounded.
- Transport projections preserve stable rule identity and exact field paths.
- Reflective convenience cannot bypass limits or alter typed behavior.
- No input value or secret leaks through diagnostics.
- Meaningful 100% coverage and every required GitHub Actions gate pass.
- A new consumer can adopt the package without reading its source.
