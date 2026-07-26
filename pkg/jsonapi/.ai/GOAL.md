# Goal: `jsonapi`

## Objective

Build an open source Go package that implements the JSON:API specification
seriously enough to serve as a production foundation for Shipit services and
other external users.

This package exists because the current Go JSON:API ecosystem is not strong
enough to treat as an obvious long-term dependency for multiple high-traffic
services. The goal is not a thin serializer helper. The goal is a real,
maintained foundation that can become a trusted default.
It is expected to become a full-spec open source package with no intentional
spec divergences or compliance gaps.

For this package, that means:

- full compliance with the JSON:API core specification
- full compliance with every official JSON:API extension or profile the
  package claims to support
- strong documented support for the official JSON:API recommendations where
  applicable, while keeping recommendations distinct from normative spec
  requirements

## Product Position

`jsonapi` should be:

- open source
- framework-agnostic
- transport-agnostic at the core
- usable from plain `net/http`
- suitable for production APIs with strict compatibility requirements
- full-spec compliant
- explicit about what parts of the spec are implemented and verified

It should not assume a specific router, ORM, validator, or dependency
injection framework.

## Why This Exists

The package is being considered before a broader move of several PHP/Laravel
services to Go. The initial service set includes at least:

- `track`
- `postal`
- `location`

Even if those first services do not all require JSON:API immediately, the
organization wants a deliberate answer to the question: if JSON:API becomes a
required transport in Go, what package do we trust?

`jsonapi` should answer that question with something maintainable and
reliable rather than forcing every service to re-implement the same behaviors
or depend on abandoned packages.

## Scope

### In Scope

- complete support for the core JSON:API specification
- complete support for the official Atomic Operations extension
- complete support for the official Cursor Pagination profile
- request and response document modeling
- resource objects, relationships, links, meta, errors, and included data
- compound documents
- sparse fieldsets
- sorting and pagination parameter handling hooks
- filter parameter handling hooks
- content negotiation for `application/vnd.api+json`
- strict document validation
- deterministic serialization
- deserialization into typed application-facing structures
- extension points for project-specific conventions
- support for the official JSON:API recommendations where they apply to server
  behavior, naming, URL design, filtering, links, asynchronous processing, and
  related consistency rules
- high-quality test fixtures and conformance tests
- clear compatibility promises and versioning

### Potentially In Scope After Core Stabilizes

- additional extension support patterns beyond the official listed extension
- additional profile support patterns beyond the official listed profile
- OpenAPI or schema generation helpers
- client helpers
- middleware for popular routers
- code generation helpers for resource definitions

### Out of Scope For The First Version

- ORM integration
- framework-specific controllers
- generated admin tooling
- project-specific auth conventions
- project-specific pagination semantics
- magical reflection-heavy behavior that hides transport rules

## Non-Goals

- Do not optimize for minimal LOC if it harms correctness.
- Do not try to become a web framework.
- Do not couple the package to Redis, Postgres, queues, or job processing.
- Do not assume that every team wants the same filtering or pagination rules.
- Do not ship a vague “supports JSON:API” claim without spec-backed evidence.

## Core Requirements

### 1. Full Spec Fidelity

The package must implement the full JSON:API specification intentionally,
including all behavior required to honestly present the package as spec
compliant.

For the first serious public version, that must include:

- the JSON:API core specification
- the official Atomic Operations extension
- the official Cursor Pagination profile

Features must be documented as:

- implemented
- explicitly unsupported
- planned but not yet implemented

Do not leave behavior ambiguous.
Do not ship a version advertised as compliant while known gaps remain.

Recommendations must also be documented clearly:

- which recommendations are implemented
- which recommendations are intentionally not implemented
- where a recommendation is not normative but is still adopted for consistency

### 2. Deterministic Output

The same input document structure must produce the same serialized output.
Tests should treat response stability as a first-class compatibility concern.

### 3. Explicit Validation

Invalid JSON:API documents must fail clearly and predictably. Error reporting
must be good enough to use the package at an API boundary without forcing
callers to reverse-engineer parsing failures.

### 4. Good Performance

The package must be efficient enough for production use, but correctness wins
over speculative micro-optimization in the first version.

Performance work should focus on:

- reducing unnecessary allocations
- avoiding reflection-heavy hot paths when possible
- minimizing repeated map reshaping
- keeping large compound-document handling predictable

### 5. Extensibility Without Framework Lock-In

The core package should expose clean interfaces and composition points so
services can:

- plug in field selection
- plug in filter parsing
- map domain errors to JSON:API errors
- adapt pagination strategies

without forking the library.

### 6. No Spec Divergences

The package must not intentionally diverge from JSON:API because a divergent
behavior is more convenient for one application.

If an application needs a project-specific behavior, it must live behind an
explicit extension seam and must not be misrepresented as standard JSON:API
behavior.

The same applies to official extensions and profiles. If the package claims
support for them, that support must be complete and non-divergent.

## First-Version Deliverables

### Package Surface

The first version should include:

- document types
- resource and relationship builders
- strict marshal and unmarshal support
- top-level document validation
- error document support
- link and meta handling
- negotiation helpers for JSON:API content types
- request parameter parsing primitives

### Conformance Suite

The repository should include:

- table-driven unit tests
- golden fixtures
- malformed input fixtures
- round-trip tests
- spec behavior coverage matrix
- benchmark suite for representative documents
- explicit proof that every required spec behavior is covered
- extension and profile coverage matrices
- recommendation coverage notes

## Documentation Deliverables

The repository must ship with:

- README
- quickstart
- architecture overview
- full public API reference
- supported-features matrix
- conformance matrix
- extension and profile support matrix
- recommendations support matrix
- extension guide
- detailed adoption guide
- end-to-end examples
- use-case cookbook
- FAQ
- troubleshooting guide
- migration notes
- compatibility policy
- versioning and release guide
- contribution guide

The documentation must be good enough that a new user can:

- understand what the package guarantees
- understand what parts of JSON:API are implemented
- adopt the package in a real project without reverse-engineering internals
- find concrete examples for common scenarios quickly
- answer common integration questions without reading source code

## API Design Principles

- prefer explicit typed APIs over magical tags where practical
- avoid hiding important transport behavior behind implicit reflection
- make zero values safe where reasonable
- keep the package easy to audit
- expose low-level primitives and optional higher-level helpers

If tags or reflection are used, they must remain understandable and optional.

## Testing Standard

This package should be treated as infrastructure, not app glue.

Meaningful 100% coverage for production package code is required.

That requirement does not mean “touch every line somehow.” It means tests must
exercise and prove the behavior of:

- happy paths
- edge cases
- malformed inputs
- branch behavior
- error paths
- protocol and document invariants
- extension and profile semantics

Coverage games are not acceptable. Hitting lines without proving behavior does
not satisfy this goal.

Testing must include:

- unit tests for all core document operations
- protocol-level fixtures for valid and invalid examples
- regression tests for every discovered edge case
- fuzzing for parsing and decoding surfaces
- benchmarks for common response shapes
- clear coverage for every required spec rule
- clear coverage for Atomic Operations behavior
- clear coverage for Cursor Pagination behavior
- clear documentation coverage for official recommendations support

The package should not claim full-spec support until the conformance matrix
proves it.

## Versioning And Compatibility

`CHANGELOG.md` MUST record every user-visible behavior and compatibility change.

- use semantic versioning
- avoid breaking wire-format behavior casually
- document every breaking change clearly
- treat serialized output shape as part of compatibility

Once `v1` is released, avoid redesigning core abstractions unless the existing
API is genuinely defective.

## Repository Automation And Quality Gates

The repository must include GitHub Actions workflows for:

- test execution
- formatting checks
- linting
- static analysis
- fuzzing or fuzz-target verification strategy
- benchmark execution strategy
- documentation validation where practical
- dependency and security scanning
- tagged release automation

At minimum, pull requests must have automated checks that prove:

- the code builds
- tests pass
- meaningful `100%` production-code coverage is maintained
- formatting is enforced
- lint and static-analysis gates are green
- documentation examples do not silently rot

Release workflows must be explicit and reproducible.

## Open Source Standard

This package should be good enough to open source without embarrassment.

That means:

- no internal Shipit assumptions in public APIs
- no app-specific names in exported symbols
- no documentation that assumes local context
- no “temporary” public surface that is really project glue

## Execution Plan

### Phase 1: Definition

- lock the supported first-version feature matrix
- define the public package layout
- decide how strict validation should behave
- define benchmark document shapes

### Phase 2: Core Implementation

- implement document model
- implement serializer and parser
- implement validation rules
- implement top-level error handling

### Phase 3: Conformance Hardening

- add exhaustive fixtures
- add fuzzing
- add benchmarks
- resolve API rough edges before `v1`

### Phase 4: Open Source Readiness

- finalize README and examples
- finalize technical API documentation
- finalize adoption documentation
- finalize FAQ and troubleshooting content
- publish roadmap
- add contribution guidelines
- finish GitHub Actions and release automation
- tag first public release

## Acceptance Criteria

This goal is achieved when:

- the package can honestly claim full JSON:API spec compliance
- the package can honestly claim full support for the official Atomic
  Operations extension
- the package can honestly claim full support for the official Cursor
  Pagination profile
- the public API is stable enough for real service adoption
- conformance coverage is documented and verified
- meaningful `100%` production-code coverage is documented and enforced
- performance is reasonable for high-traffic APIs
- GitHub Actions quality gates and release automation are in place
- user-facing docs are complete enough for direct adoption
- the repo is suitable for open source publication and external use

## Hard Warnings

- Do not let this become an endless framework design exercise.
- Do not add speculative app-specific convenience layers too early.
- Do not claim “full JSON:API support” without a concrete support matrix.
- Do not ship intentional spec deviations disguised as extensions.
- Do not blur recommendations into normative spec rules.
- Do not start with extensions before the core spec is solid.
