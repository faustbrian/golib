# Goal: Typed API Query Contracts

## Objective

Build a transport-neutral package for typed field selection, filtering,
relationship inclusion, sorting, cursor pagination, and bounded query planning
across JSON-RPC and conventional HTTP APIs.

The package MUST replace repeated Laravel RPC resource/query-builder conventions
without becoming an ORM, exposing arbitrary SQL, or conflating JSON:API's
normative query semantics with a general application query contract.

## Product Principles

- Servers explicitly declare allowed fields, filters, sorts, relationships,
  operators, page sizes, costs, and cursor versions.
- Client input compiles into an immutable validated query plan.
- Unknown, duplicate, conflicting, unauthorized, and excessive query elements
  fail predictably.
- Query plans are storage-neutral; adapters translate only reviewed plans.
- Cursors are opaque, authenticated where required, versioned, and bounded.
- Query behavior, cost, and compatibility are public API contracts.

## Core Model

- Typed schema with resource name, fields, relationships, filters, sorts, and
  pagination capabilities.
- Request model preserving absent versus explicitly empty query components.
- Immutable compiled plan with selected fields, filter expression, includes,
  sort terms, page request, projected cost, and schema revision.
- Stable paths and errors for invalid element, unsupported operation, conflict,
  authorization rejection, cost limit, cursor failure, and version mismatch.
- Generic typed values and codecs without an unbounded `any` meta-model.
- Deterministic canonical representation for caching, signing, and tests.

## Field Selection And Relationships

- Explicit allowlists and default field sets.
- Required server fields that cannot be accidentally omitted from execution.
- Relationship/include paths with maximum depth, count, cycle detection, and
  per-edge authorization hooks.
- Distinguish execution fields from response projection to prevent incomplete
  joins or authorization leakage.
- No automatic reflection over domain objects as the default contract.

## Filtering

- Typed operators: equality, inequality, ordering, membership, ranges, null,
  string operations, and logical composition only where declared.
- Explicit missing, null, empty, repeated, and type-mismatch behavior.
- Bounded expression depth, node count, values, strings, and membership lists.
- No raw field names, SQL fragments, regular expressions, or functions supplied
  by clients unless specifically modeled and safely compiled.
- Authorization can remove capabilities before compilation; rejected filters
  MUST not reveal inaccessible schema details unnecessarily.

## Sorting And Pagination

- Ordered multi-column sorts with explicit direction and null ordering.
- Every cursor-paginated plan MUST have a deterministic total order with stable
  tie-breakers.
- Cursor payload includes schema/version identity, sort definition, direction,
  typed positions, and expiration/policy where configured.
- Cursor encoding supports signing/authentication and secret rotation without
  exposing internal values unnecessarily.
- Forward and backward pagination, first/last-page boundaries, has-more, and
  next/previous cursor semantics are explicit.
- Offset pagination MAY exist as a separate capability with documented limits.

## Transport Integration

- JSON-RPC parameter and OpenRPC schema/content-descriptor adapters.
- HTTP query parser with strict duplicate, encoding, and size semantics.
- JSON:API integration only through `jsonapi`, which remains authoritative
  for JSON:API names, syntax, extensions, and recommendations.
- Stable response page metadata and cursor envelope helpers.
- `validation` integration for structured query violations.

## Persistence Adapters

- Core MUST NOT import PostgreSQL, SQLC, an ORM, or a query builder.
- Optional PostgreSQL/SQLC helpers translate reviewed typed plans into explicit
  application-owned query variants or safe expression builders.
- Adapters MUST use allowlisted identifiers and parameterized values.
- Authorization, tenant predicates, and mandatory constraints cannot be removed
  or replaced by client filters.
- Query cost estimates are conservative and must not claim database optimizer
  certainty.

## Security And Resource Bounds

- Bound request bytes, fields, includes, depth, filter nodes, values, sort terms,
  page size, cursor size, decode work, canonical output, and error count.
- Threat-model SQL injection, field exfiltration, relationship traversal,
  tenant predicate removal, cursor forgery/replay, expensive query attacks,
  Unicode confusion, and schema probing.
- No raw cursor, protected filter value, or inaccessible field in diagnostics.
- Caller-owned schemas and requests MUST NOT mutate after compilation.

## Non-Goals

- No ORM, repository, database executor, arbitrary SQL language, GraphQL engine,
  search engine, authorization engine, or generic expression programming
  language.
- No JSON:API specification reimplementation.
- No automatic endpoint or model discovery.
- No hidden query execution or lazy relationship loading.

## Package Shape

- Root: schema, request, plan, fields, includes, filters, sorts, pages, errors.
- `cursor`: versioned typed cursor encoding, signing, and rotation.
- `apiqueryhttp`, `apiqueryrpc`, and `apiqueryjsonapi`: transports.
- `apiquerypgx`: optional safe PostgreSQL primitives and SQLC guidance.
- `apiquerytest`: builders, fixtures, assertions, and conformance suites.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Required evidence:

- exhaustive schema/request/plan semantic tables
- filter logic, sorting, total-order, forward/backward, and cursor properties
- parser, expression, Unicode, duplicate, depth, and cursor fuzzing
- mutation testing for mandatory predicates, authorization, limits, ordering,
  cursor verification, and page boundaries
- race tests for shared schemas, compiled plans, and signing-key rotation
- cross-transport canonical-plan conformance
- real PostgreSQL integration proving injection resistance and page stability
- benchmarks for compile, canonicalization, cursor, deep filters, and large
  schemas with allocation and cost budgets

## Documentation Deliverables

- Five-minute schema and JSON-RPC quickstart.
- Complete field, filter, include, sort, cursor, error, and adapter API reference.
- Guides for OpenRPC, HTTP, JSON:API composition, SQLC, authorization, tenant
  constraints, cursor rotation, versioning, and performance.
- Laravel/Cline RPC migration guide, security model, FAQ, troubleshooting,
  examples, compatibility policy, and maintained changelog.
- Every exported API and user-facing scenario MUST be documented.

## Automation And Release

Use the latest stable Go release as the minimum at implementation time. Pin all
tools and dependencies. GitHub Actions MUST run formatting, vet, Staticcheck,
strict golangci-lint, advisory NilAway, tests, meaningful 100% coverage, race,
fuzz smoke, mutation checks, PostgreSQL integration, vulnerability scans,
benchmarks, docs, API compatibility, and releases. All blocking commands MUST
be reproducible locally through documented `make` targets.

## Execution Plan

1. Specify schema, request, plan, errors, bounds, and canonical form.
2. Implement fields, relationships, filters, sorts, and cost validation.
3. Implement versioned cursor pagination and signing.
4. Implement RPC, HTTP, JSON:API bridge, and optional PostgreSQL helpers.
5. Complete hostile-query, mutation, compatibility, and performance hardening.
6. Publish full adoption documentation and release v1.

## Acceptance Criteria

- Client input can only produce capabilities declared by the server schema.
- Mandatory tenant and authorization constraints cannot be bypassed.
- Cursor pagination is stable, deterministic, versioned, and tamper-resistant.
- Core remains transport and persistence neutral.
- Meaningful 100% coverage and every required CI gate pass.
