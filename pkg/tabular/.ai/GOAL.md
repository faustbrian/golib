# Goal: `tabular`

## Objective

Build an open source Go package for tabular data ingestion and emission that is
strong enough for real production imports, exports, and batch processing.

The package should start with the format family that already matters in the
current migration candidates:

- CSV
- XLS
- XLSX
- fixed-width text files
- ZIP-backed source archives where extraction is part of normal ingest flow

This package exists because tabular import work is a different concern from
wire/protocol payload handling. It should not be forced into `wire`.

## Why This Exists

The current service family, especially `postal`, already has real ingestion
requirements around:

- configurable delimiters
- streamed spreadsheet processing
- fixed-width parsing
- encoding conversion
- ZIP extraction before processing
- row normalization
- large-file handling

Those concerns are cohesive enough to justify a dedicated package rather than
re-implementing them per service.

## Product Position

`tabular` should be:

- open source
- production-grade
- ingest-oriented
- format-aware
- streaming-first where practical
- explicit about supported formats and tradeoffs

It should not become a generic ETL platform or data warehouse toolkit.

## First-Version Scope

### In Scope

- delimited text parsing
- configurable delimiter support
- quoted-field handling
- spreadsheet ingestion for XLS and XLSX
- fixed-width parsing
- byte-position extraction helpers
- encoding normalization helpers
- ZIP extraction helpers directly relevant to tabular ingest
- row normalization helpers
- structured error types
- fixture-driven ingestion tests

### Potentially In Scope After Core Stabilizes

- TSV as a named first-class convenience
- export helpers
- schema mapping helpers
- column validation helpers
- streaming archive helpers beyond ZIP

### Out of Scope For The First Version

- database loading
- workflow orchestration
- queueing
- application-specific business transforms
- every archive format
- every office document format

## Non-Goals

- Do not become a spreadsheet editor.
- Do not become a full ETL framework.
- Do not hide important format semantics behind magical auto-detection.
- Do not add formats with weak justification.

## Core Requirements

### 1. Strong CSV And Spreadsheet Support

The package must handle real CSV and spreadsheet ingestion use cases, not only
small happy-path examples.

That includes:

- configurable delimiters
- empty-file handling
- malformed-row handling
- header extraction and normalization
- row streaming where supported
- practical XLS/XLSX ingestion

### 2. Strong Fixed-Width Support

Fixed-width parsing is a first-class concern, not a side utility.

That includes:

- byte-position extraction
- field trimming
- encoding-aware conversion
- deterministic parsing
- explicit error reporting for invalid layouts

### 3. Archive-Aware Ingest Support

ZIP extraction can be part of the ingest contract where source datasets are
normally distributed as archives. That behavior should be explicit and
well-tested rather than left to ad hoc application utilities.

### 4. Honest Format Boundaries

The package must clearly document:

- which formats are supported
- which features within those formats are supported
- where streaming is available
- where full materialization is unavoidable
- where format limitations remain

### 5. Deterministic Row Semantics

Given the same source input, parser configuration, and normalization rules, the
package must produce the same rows and the same errors consistently.

### 6. No Hidden Data Corruption

If normalization, trimming, encoding conversion, delimiter handling, or header
mapping changes incoming data, that behavior must be explicit and documented.

## First-Version Deliverables

### Package Surface

- CSV and delimited text helpers
- XLS/XLSX ingest helpers
- fixed-width parsing helpers
- ZIP extraction helpers for ingest
- row normalization helpers
- structured error types
- explicit parser configuration types

### Verification

- CSV fixtures
- XLS/XLSX fixtures
- fixed-width fixtures
- ZIP ingest fixtures
- malformed-input fixtures
- regression suite for delimiter, header, encoding, and row-shape issues
- benchmarks for representative ingestion paths

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

- choose the right parser quickly
- understand what each format helper guarantees
- adopt the package without reverse-engineering internals
- find realistic examples for common ingest scenarios

## API Design Principles

- prefer explicit parser configuration over clever auto-detection
- keep row access and error reporting understandable
- expose low-level primitives where necessary
- keep ingest behavior auditable
- avoid hidden conversions

## Testing Standard

This package should be treated as critical ingest infrastructure.

Meaningful 100% coverage for production package code is required.

That requirement does not mean “touch every line somehow.” It means tests must
exercise and prove the behavior of:

- happy paths
- edge cases
- malformed inputs
- branch behavior
- error paths
- delimiter behavior
- spreadsheet behavior
- fixed-width behavior
- archive behavior
- normalization behavior
- encoding behavior

Coverage games are not acceptable. Hitting lines without proving behavior does
not satisfy this goal.

Testing must include:

- unit tests for core parsers and helpers
- fixture-driven ingest tests
- malformed input coverage
- regression tests for discovered format edge cases
- fuzzing where parsing surfaces justify it
- benchmarks for representative ingest paths
- clear proof that every supported behavior is covered

## Versioning And Compatibility

`CHANGELOG.md` MUST record every user-visible behavior and compatibility change.

- use semantic versioning
- document every breaking change clearly
- treat row-shape behavior, normalization, and error semantics as
  compatibility-sensitive
- do not casually change parsing or extraction behavior

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
- no undocumented ingest quirks

## Execution Plan

### Phase 1: Definition

- define the public package layout
- define CSV and delimited-text scope
- define XLS/XLSX scope
- define fixed-width scope
- define ZIP ingest scope
- define what is intentionally out of scope

### Phase 2: Core Implementation

- implement CSV and delimited-text helpers
- implement spreadsheet helpers
- implement fixed-width helpers
- implement ZIP ingest helpers
- implement core error model

### Phase 3: Ingest Hardening

- add fixture suites
- add fuzzing where justified
- add benchmarks
- add regression coverage for ugly real-world files

### Phase 4: Open Source Readiness

- finalize technical API documentation
- finalize adoption documentation
- finalize FAQ and troubleshooting content
- finish GitHub Actions and release automation
- publish roadmap
- release `v1`

## Acceptance Criteria

This goal is achieved when:

- CSV/XLS/XLSX/fixed-width/ZIP ingest support is production-credible
- supported behavior is documented honestly and precisely
- meaningful `100%` production-code coverage is documented and enforced
- GitHub Actions quality gates and release automation are in place
- user-facing docs are complete enough for direct adoption
- the repo is suitable for open source release and real service adoption

## Hard Warnings

- Do not turn this into a generic ETL platform.
- Do not over-expand the format list before the first format set is solid.
- Do not hide normalization or encoding changes from users.
- Do not ship vague ingest guarantees without fixture-backed proof.
