# Goal: `jsonrpc`

## Objective

Build an open source Go package for JSON-RPC 2.0 that is strong enough to
serve as the shared RPC foundation for Shipit services and credible enough to
use as a public dependency.

The package should support both server and client use cases and must be
designed for production APIs, not only toy examples.
It is expected to become a full-spec open source package with no intentional
spec divergences or compliance gaps.

## Why This Exists

Multiple Shipit services already revolve around JSON-RPC-style transports or
RPC-like service boundaries, including:

- `track`
- `postal`
- `location`

The organization wants a reusable Go RPC foundation that avoids service-by-
service reimplementation and does not depend on thin, abandoned, or poorly
observable RPC helpers.

## Product Position

`jsonrpc` should be:

- open source
- framework-agnostic
- compatible with plain `net/http`
- usable for both server and client implementations
- full-spec compliant
- explicit about protocol behavior
- suitable for internal service-to-service and public API use

It should not force a specific router, validator, logger, or transport stack.

## Scope

### In Scope

- full JSON-RPC 2.0 request and response handling
- notifications
- batch requests and batch responses
- server method registration and dispatch
- request ID handling
- protocol-compliant error handling
- context propagation hooks
- typed middleware integration points for `authentication`,
  `authorization`, and `idempotency` without implementing those concerns
- typed client helpers
- observability hooks
- request/response validation primitives

### Potentially In Scope After Core Stabilizes

- WebSocket transport helpers
- stream-friendly batch helpers
- schema or OpenRPC generation
- client retry helpers
- router integrations

### Out of Scope For The First Version

- application-specific auth schemes
- authentication, authorization, or idempotency engines
- service discovery
- retries with business semantics baked in
- queueing or background job orchestration
- SOAP, XML-RPC, or JSON:API support in the same package

## Non-Goals

- Do not try to turn this into a general service mesh framework.
- Do not hide protocol behavior behind excessive reflection magic.
- Do not couple the package to Redis, Postgres, or Kubernetes.
- Do not assume every user wants generated code or generics-heavy APIs.

## Core Requirements

### 1. Full Protocol Correctness

The package must implement JSON-RPC 2.0 cleanly and explicitly.

This includes:

- correct distinction between requests and notifications
- correct batch behavior
- correct handling of parse errors, invalid requests, method-not-found,
  invalid params, and internal errors
- correct ID echoing rules
- no intentional protocol deviations for local convenience

### 2. Clear Separation Between Transport And Protocol

The core package should understand JSON-RPC, not HTTP-only JSON-RPC.

HTTP helpers are useful, but the central abstractions should separate:

- wire transport
- JSON-RPC envelope parsing
- dispatch
- method execution
- response shaping

### 3. Strong Error Model

The package must make it easy to:

- return protocol-compliant errors
- map internal application errors to RPC errors
- preserve useful context for logging and tracing

Error handling must be predictable and easy to audit.

### 4. Production Observability

The package should make it straightforward to attach:

- request logging
- tracing
- metrics
- correlation IDs

without forcing any one observability vendor.

### 5. Good Performance

The first version must be efficient enough for high-traffic service use:

- avoid unnecessary decoding and re-encoding
- avoid per-request reflection churn where possible
- keep batch handling efficient
- minimize allocation-heavy abstractions on hot paths

### 6. No Spec Divergences

The package must not intentionally diverge from JSON-RPC 2.0 to fit one
application's transport habits or error preferences.

Any optional convenience behavior must be clearly identified as optional and
must not weaken protocol compliance.

## First-Version Deliverables

### Server

- method registry
- dispatcher
- middleware or interceptor chain
- HTTP adapter
- batch support
- protocol-compliant error responses

### Client

- typed request builder
- notification support
- batch request support
- response validation
- transport abstraction for HTTP and custom transports

### Support Utilities

- request ID helpers
- structured error builders
- envelope validators
- content-type helpers

## Documentation Deliverables

- README
- quickstart
- architecture overview
- full public API reference
- examples for client and server
- end-to-end usage examples
- scenario cookbook
- adoption guide
- FAQ
- troubleshooting guide
- middleware or interceptor guidance
- compatibility policy
- versioning and release guide
- contribution guide

The documentation must be good enough that a new user can:

- understand the protocol guarantees
- stand up a server quickly
- integrate a client quickly
- understand batch, notification, and error behavior without reading source
- adopt the package in production with clear examples and caveats

## API Design Principles

- prefer explicit request and response types
- keep middleware semantics simple and composable
- avoid hidden global registries
- allow low-level control without forcing everyone to use low-level APIs
- keep the package debuggable under load

## Testing Standard

The package should be tested like a critical runtime dependency.

Meaningful 100% coverage for production package code is required.

That requirement does not mean “touch every line somehow.” It means tests must
exercise and prove the behavior of:

- happy paths
- edge cases
- malformed inputs
- branch behavior
- error paths
- protocol semantics
- notification and batch behavior

Coverage games are not acceptable. Hitting lines without proving behavior does
not satisfy this goal.

Required test coverage includes:

- unit tests for envelope validation and dispatch
- protocol fixtures for valid and invalid requests
- batch edge cases
- malformed JSON handling
- notification semantics
- regression tests for every protocol bug found
- fuzzing for parser and dispatcher surfaces
- benchmarks for single-call and batch workloads
- clear proof that every required protocol rule is covered

## Versioning And Compatibility

`CHANGELOG.md` MUST record every user-visible behavior and compatibility change.

- semantic versioning
- avoid casual breaking changes to error or response semantics
- document every breaking change
- treat protocol behavior as compatibility-sensitive

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

The package must be publishable as a clean public dependency:

- no Shipit-specific exported names
- no local service assumptions in public APIs
- no documentation that depends on internal context

## Execution Plan

### Phase 1: Protocol Definition

- define public types
- lock error model
- define server/client boundaries
- define middleware model

### Phase 2: Core Implementation

- implement request parsing
- implement dispatch
- implement server HTTP adapter
- implement client core

### Phase 3: Hardening

- add exhaustive protocol fixtures
- add fuzzing
- add benchmarks
- refine public surface before `v1`

### Phase 4: Open Source Readiness

- finalize docs
- finalize technical API documentation
- finalize adoption documentation
- finalize FAQ and troubleshooting content
- add contribution guide
- finish GitHub Actions and release automation
- publish roadmap
- release `v1`

## Acceptance Criteria

This goal is achieved when:

- the package can honestly claim full JSON-RPC 2.0 compliance
- both server and client usage are production-credible
- observability and middleware hooks are clean enough for real services
- protocol conformance is documented and tested
- meaningful `100%` production-code coverage is documented and enforced
- GitHub Actions quality gates and release automation are in place
- user-facing docs are complete enough for direct adoption
- the repo is suitable for open source publication and external use

## Hard Warnings

- Do not merge protocol and business semantics together.
- Do not let HTTP assumptions dominate the core package.
- Do not publish a `v1` before batch, notification, and error semantics are
  proven.
- Do not ship local convenience deviations as if they were protocol-compliant.
- Do not add queue, workflow, or service discovery concerns to this package.
