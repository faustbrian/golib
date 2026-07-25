# Goal: `wire`

## Objective

Build an open source Go package for structured wire-format handling with JSON,
XML, SOAP, YAML, TOML, MessagePack, CBOR, and BSON as first-class concerns.

This package is intended to become a production-grade interoperability
foundation for services that need to:

- parse and emit JSON safely
- parse and emit XML safely
- work with SOAP envelopes and faults
- parse and emit YAML, TOML, MessagePack, CBOR, and BSON safely
- normalize ugly vendor payloads without scattering format logic across apps

The first version uses dedicated packages so each supported format remains
explicit instead of becoming a generic “supports every format” abstraction.

## Why This Exists

The Go migration candidates already show recurring transport and payload
concerns:

- JSON and JSON-RPC payload handling
- XML-based carrier or vendor integrations
- SOAP or SOAP-like integrations
- envelope normalization
- awkward charset or payload-shape behavior at third-party boundaries

The organization wants one serious OSS package that can become the trusted
place for this work instead of solving the same wire-format problems inside
every service.

## Product Position

`wire` should be:

- open source
- format-aware
- transport-neutral at the core
- framework-agnostic
- suitable for production interoperability work
- explicit about supported formats and guarantees

It should not be a vague dumping ground for random serialization helpers.

## First-Version Scope

### In Scope

- JSON helpers where the standard library alone is not enough for package
  goals
- XML encoding and decoding helpers
- SOAP envelope handling
- SOAP fault handling
- YAML and TOML document encoding and decoding
- MessagePack, CBOR, and BSON encoding and decoding
- namespace-aware XML handling where needed
- charset and payload normalization helpers where needed
- request and response body helpers
- validation and error reporting primitives
- interop helpers for ugly vendor payload shapes
- fixture-driven interoperability testing

### Potentially In Scope After Core Stabilizes

- WSDL tooling guidance or adapter support
- schema generation helpers
- client helpers for common transport patterns

### Out of Scope For The First Version

- every possible payload format
- general HTTP client framework behavior
- generic RPC framework behavior
- application-specific business mapping
- queue semantics
- persistence concerns

## Non-Goals

- Do not become a replacement for `encoding/json` or `encoding/xml` without
  clear justification.
- Do not turn this into a generic “all codecs forever” abstraction layer.
- Do not add formats just because they seem adjacent.
- Do not hide wire-format semantics behind clever abstractions that make
  debugging harder.

## Core Requirements

### 1. Excellent Wire-Format Interop

The package must be strong enough to handle real production wire-format work,
not only clean textbook payloads.

That includes:

- malformed or awkward vendor payloads where safe handling is possible
- namespace-heavy XML
- SOAP fault shapes
- strict and compatibility-aware YAML and TOML documents
- deterministic MessagePack, CBOR, and BSON output
- body extraction and normalization
- predictable error reporting

### 2. Honest Scope Boundaries

The package must clearly document what it supports for:

- JSON
- XML
- SOAP
- YAML
- TOML
- MessagePack
- CBOR
- BSON

and what it does not support yet.

It must not overclaim broad “format support” without a concrete support matrix.

### 3. Deterministic Behavior

Serialization and normalization behavior must be stable and testable. The same
input should produce the same output under the same configuration.

### 4. Strong Error Model

Consumers must be able to distinguish:

- parse failures
- validation failures
- unsupported format behavior
- SOAP fault behavior
- transport-envelope errors versus content-shape errors

### 5. Extensibility Without Chaos

The package should allow growth into more formats later, but the first version
must not distort its API around speculative future support.

### 6. No Hidden Divergences

If the package intentionally normalizes or deviates from a raw format shape for
interoperability reasons, that behavior must be explicit and documented.

## First-Version Deliverables

### Package Surface

- JSON helpers justified beyond the standard library
- XML encode/decode helpers
- SOAP envelope and fault primitives
- YAML and TOML document helpers
- MessagePack, CBOR, and BSON object/document helpers
- format detection or routing helpers where clearly useful
- normalization helpers
- structured error types

### Verification

- JSON fixtures
- XML fixtures
- SOAP fixtures
- YAML, TOML, MessagePack, CBOR, and BSON fixtures
- malformed payload fixtures
- interoperability regression suite
- benchmarks for representative parse and encode paths

## Documentation Deliverables

- README
- quickstart
- architecture overview
- full public API reference
- supported-format matrix
- behavior and limitation matrix
- detailed adoption guide
- end-to-end examples
- scenario cookbook
- FAQ
- troubleshooting guide
- migration notes
- versioning and release guide
- contribution guide

The documentation must be good enough that a new user can:

- understand what support each wire-format package actually provides
- adopt the package in a real service quickly
- find examples for common interop scenarios
- understand limitations without reading source code

## API Design Principles

- prefer explicit APIs over magical format auto-detection
- keep raw access available when higher-level helpers are used
- avoid reflection-heavy abstractions unless clearly justified
- make interop behavior auditable
- keep wire-format errors understandable

## Testing Standard

This package should be treated as critical interoperability infrastructure.

Meaningful 100% coverage for production package code is required.

That requirement does not mean “touch every line somehow.” It means tests must
exercise and prove the behavior of:

- happy paths
- edge cases
- malformed inputs
- branch behavior
- error paths
- JSON behavior
- XML behavior
- SOAP behavior
- YAML behavior
- TOML behavior
- MessagePack behavior
- CBOR behavior
- BSON behavior
- normalization behavior

Coverage games are not acceptable. Hitting lines without proving behavior does
not satisfy this goal.

Testing must include:

- unit tests for core format helpers
- fixture-driven interop tests
- malformed input coverage
- regression tests for discovered vendor edge cases
- fuzzing for parsing surfaces
- benchmarks for representative parse and encode paths
- clear proof that every supported behavior is covered

## Versioning And Compatibility

`CHANGELOG.md` MUST record every user-visible behavior and compatibility change.

- use semantic versioning
- document every breaking change clearly
- treat public behavior and wire-format handling as compatibility-sensitive
- do not casually change normalization or fault-handling behavior

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

This package should be publishable as serious infrastructure:

- no Shipit-specific names in public APIs
- no hidden local assumptions
- no vague claims about format support
- no undocumented interoperability quirks

## Execution Plan

### Phase 1: Definition

- define the public package layout
- define JSON scope
- define XML scope
- define SOAP scope
- define YAML, TOML, MessagePack, CBOR, and BSON scope
- define what is intentionally out of scope

### Phase 2: Core Implementation

- implement JSON helpers
- implement XML helpers
- implement SOAP envelope and fault primitives
- implement core error model
- implement dedicated bidirectional YAML, TOML, MessagePack, CBOR, and BSON
  packages

### Phase 3: Interop Hardening

- add fixture suites
- add fuzzing
- add benchmarks
- add regression coverage for ugly real-world payloads

### Phase 4: Open Source Readiness

- finalize technical API documentation
- finalize adoption documentation
- finalize FAQ and troubleshooting content
- finish GitHub Actions and release automation
- publish roadmap
- release `v1`

## Acceptance Criteria

This goal is achieved when:

- every documented format is production-credible
- supported behavior is documented honestly and precisely
- meaningful `100%` production-code coverage is documented and enforced
- GitHub Actions quality gates and release automation are in place
- user-facing docs are complete enough for direct adoption
- the repo is suitable for open source release and real service adoption

## Hard Warnings

- Do not add further formats without matching the existing production standard.
- Do not over-abstract the API around hypothetical future formats.
- Do not hide wire-format quirks that users need to understand.
- Do not ship vague “interop support” claims without explicit behavior
  coverage and documentation.
