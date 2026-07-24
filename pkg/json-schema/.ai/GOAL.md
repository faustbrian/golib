# Goal: `json-schema`

## Objective

Build an open source Go implementation of JSON Schema that is complete,
interoperable, secure under hostile input, and suitable as the canonical JSON
Schema foundation for the Go libraries and service ecosystem.

This is not a thin wrapper around another validator and not a partial helper
that supports only the common keywords. It MUST be a serious implementation
of every released JSON Schema Core and Validation dialect represented by the
official JSON Schema Test Suite, with no skipped official cases, known
specification divergences, or undocumented gaps.

The package MUST achieve full compatibility with the official JSON Schema
Test Suite for:

- Draft 3;
- Draft 4;
- Draft 6;
- Draft 7;
- Draft 2019-09; and
- Draft 2020-12.

Support for an unreleased `draft-next` MAY be developed behind an explicitly
experimental API and test lane. It MUST NOT weaken, silently alter, or be
presented as part of stable released-dialect compliance.

## Product Position

`json-schema` should be:

- a standalone, independently versioned module in `golib`;
- imported as package `jsonschema`;
- framework-agnostic and transport-agnostic;
- usable with the Go standard library alone at its core where practical;
- safe for validating untrusted schemas and untrusted instances;
- deterministic and concurrency-safe after compilation;
- explicit about dialect, vocabulary, resolver, format, output, and resource
  policies;
- extensible without global mutable registries;
- suitable for OpenRPC, API query, configuration, service, and JSON:API
  integrations without depending on those higher-level packages;
- credible as a public OSS dependency outside the owning service ecosystem.

It MUST NOT require a web framework, service container, database, cache,
telemetry provider, filesystem abstraction, or network access.

## Authoritative Sources

Implementation and conformance decisions MUST be derived from primary
sources, including:

- https://json-schema.org/specification
- https://json-schema.org/draft/2020-12/json-schema-core
- https://json-schema.org/draft/2020-12/json-schema-validation
- the corresponding official Core and Validation specifications for every
  supported historical draft;
- official meta-schemas and vocabulary meta-schemas;
- https://github.com/json-schema-org/JSON-Schema-Test-Suite
- the official JSON Schema output schemas and examples;
- applicable URI, IRI, JSON Pointer, Relative JSON Pointer, media type,
  Unicode, date/time, hostname, IP address, UUID, and regular-expression
  specifications referenced normatively by a supported dialect;
- Bowtie's implementation protocol and compliance reports as independent
  interoperability evidence, never as a replacement for the specification.

The existing PHP package at `/Users/brian/Developer/cline/json-schema` MAY be
used as behavioral and adoption reference. It MUST NOT override normative
specification behavior, and its architecture MUST NOT be translated blindly
into Go.

Pin the exact revision of every imported official fixture or meta-schema.
Record provenance, license, checksums, update procedure, and local deviations.
Official fixtures MUST remain unmodified; local regression fixtures belong in
a separate clearly named tree.

## Scope

### Stable V1 Scope

- complete dialect selection and processing for every released suite listed
  above;
- schema parsing and validation, including boolean schemas where supported;
- validation of schemas against the correct official meta-schema;
- immutable compiled schemas reusable across goroutines;
- validation of raw JSON and caller-provided JSON data models without number
  precision loss;
- complete Core and Validation keyword behavior for each dialect;
- identifiers, base URI changes, anchors, recursive anchors, dynamic anchors,
  references, recursive references, and dynamic references according to the
  active dialect;
- embedded schemas, schema resources, compound schema documents, bundling,
  and canonical resource identification;
- complete applicator, validation, unevaluated, format, content, metadata,
  and core vocabulary semantics where defined;
- annotations and evaluated-location tracking required for correct applicator
  and unevaluated behavior;
- dialect and vocabulary registration through explicit instance-owned
  registries;
- custom keyword and vocabulary extension points that cannot violate built-in
  dialect invariants accidentally;
- custom format registration and correct distinction between format
  annotation and format assertion;
- all standard formats required or defined by supported drafts, with explicit
  dialect-aware behavior;
- standard Flag, Basic, Detailed, and Verbose validation output for dialects
  that define the output model;
- deterministic diagnostic ordering and stable machine-readable locations;
- secure, explicit schema loading and reference resolution;
- local, in-memory, embedded, filesystem, and caller-defined retrieval through
  narrowly scoped resolver interfaces where appropriate;
- cancellation and resource-limit policies;
- complete Go documentation, adoption material, examples, and conformance
  evidence.

### Additional Utilities After Core Compliance

The following MAY be included as optional subpackages only after the stable
validator is complete and their contracts are independently justified:

- schema construction and immutable builder APIs;
- schema generation from Go types or example instances;
- schema bundling and unbundling;
- schema migration between released drafts;
- semantic schema diffing and compatibility classification;
- schema merging and composition helpers;
- code generation;
- integrations for `config`, `openrpc`, `api-query`, `jsonapi`,
  and `service`.

These utilities MUST NOT be presented as normative JSON Schema behavior and
MUST NOT complicate or destabilize the core evaluator.

### Explicitly Separate Specifications

JSON Hyper-Schema and Relative JSON Pointer are distinct specifications. If
implemented, they MUST receive their own explicit scope, conformance matrix,
tests, documentation, and compliance claim. Core and Validation compliance
MUST NOT imply unsupported Hyper-Schema behavior.

### Non-Goals

- Do not become a general Go validation framework.
- Do not replace `validation` for programmatic domain validation.
- Do not add JSON:API, JSON-RPC, OpenRPC, OpenAPI, or business policy to the
  core package.
- Do not make `wire` responsible for schema evaluation.
- Do not enable implicit network retrieval.
- Do not expose unbounded global schema or regular-expression caches.
- Do not hide dialect selection, format assertion, or remote resolution behind
  surprising defaults.
- Do not claim complete compliance based only on line coverage or the official
  fixture suite.

## Architecture

### Dependency Direction

The core module should depend on the standard library wherever practical.
Any third-party dependency MUST have a documented necessity, maintenance and
security evaluation, license review, and replacement strategy.

The intended ecosystem direction is:

```text
openrpc -----------+
api-query ---------+
config ------------+--> json-schema
jsonapi -----------+
service -----------+
```

`json-schema` MUST NOT import those consumers. Integrations that would add
heavy or optional dependencies belong in consumer modules or separate adapter
modules.

### Core Components

The design should separate:

- JSON ingestion and exact data representation;
- schema document parsing;
- dialect discovery;
- schema resource indexing;
- URI and fragment resolution;
- vocabulary and keyword compilation;
- immutable evaluation plans;
- instance evaluation;
- annotation collection;
- output construction;
- format and content assertion;
- resolver policy;
- resource-budget enforcement.

Compilation and evaluation errors MUST remain distinguishable. Invalid JSON,
invalid schemas, unsupported required vocabularies, unavailable resources,
resource-limit exhaustion, cancellation, and invalid instances MUST not be
collapsed into one ambiguous error.

### Public API Principles

- Prefer explicit constructors and immutable options.
- Make zero values safe or reject them immediately with typed errors.
- Avoid reflection in evaluator hot paths unless measured evidence supports
  it.
- Compiled schemas MUST be safe for concurrent validation.
- Callers MUST retain control of network, filesystem, cache, and logging
  behavior.
- Returned diagnostics MUST not alias mutable evaluator buffers.
- Public errors MUST support `errors.Is` and `errors.As` where classification
  is useful.
- APIs MUST preserve exact JSON number semantics. Converting arbitrary JSON
  numbers to `float64` is not acceptable.
- Validation MUST not mutate schemas or instances supplied by callers.
- Package initialization MUST not register global behavior or start work.

## Dialect And Vocabulary Requirements

For every supported dialect, maintain a normative matrix covering:

- identifier and base URI behavior;
- reference and fragment behavior;
- schema and subschema location rules;
- supported vocabularies and required-vocabulary behavior;
- every keyword and its schema form;
- sibling behavior around reference keywords;
- annotation collection and propagation;
- evaluated properties and items;
- output-location construction;
- unknown keyword behavior;
- format behavior;
- content behavior;
- meta-schema validation;
- compatibility differences from adjacent drafts.

Draft-specific semantics MUST not leak across dialects. In particular, prove
the different meanings and interactions of `id`/`$id`, definitions/`$defs`,
dependencies/dependent schemas and required properties, tuple keywords,
exclusive bounds, contains limits, recursive references, dynamic references,
unevaluated keywords, and format behavior.

Required custom vocabularies that are unknown to the implementation MUST fail
as required by the active specification. Optional or unknown vocabulary
behavior MUST remain spec-correct and documented.

## Reference Resolution

Reference handling is a correctness and security boundary.

The implementation MUST support:

- correct URI-reference resolution and normalization;
- dialect-specific base URI changes;
- JSON Pointer fragments and plain-name anchors where supported;
- canonical and non-canonical resource identifiers;
- nested and embedded schema resources;
- recursive and dynamic scope;
- reference cycles without infinite recursion;
- repeated references without repeated unbounded compilation;
- deterministic retrieval and resource identity;
- official suite remote fixtures through an injected resolver;
- caller-controlled scheme allowlists, host policies, size limits, redirects,
  timeouts, and authentication for any optional HTTP resolver.

The core MUST default to no network access. A missing resource MUST return a
typed resolution error. Resolver failures MUST preserve a safe cause without
leaking credentials or full sensitive documents.

## Regular Expressions And Formats

JSON Schema regular-expression semantics are based on the specification's
ECMA-262 requirements and recommendations, not automatically on Go's RE2
syntax. The implementation MUST either provide the required compatible
semantics or document and resolve every difference before claiming full
compliance. Passing the current fixture corpus alone is not sufficient proof.

Test Unicode code points, surrogate-related input handling, escaping,
anchoring, lookarounds or unsupported constructs, catastrophic-pattern
resistance, and dialect-specific expectations.

Format implementations MUST be derived from the standards referenced by each
dialect. Distinguish annotation from assertion. Format registration MUST be
instance-scoped, concurrency-safe, deterministic, and protected by evaluation
budgets where custom callbacks are used.

## Validation Output

Support the standard output forms where applicable:

- Flag;
- Basic;
- Detailed; and
- Verbose.

Output MUST accurately represent validity, keyword location, absolute keyword
location, instance location, nested causes, annotations, and errors according
to the active output specification. Ordering policy MUST be deterministic and
documented without inventing semantic ordering where the specification does
not define one.

Provide ergonomic typed diagnostics without making the JSON-compatible
standard output impossible to reproduce exactly.

## Resource Safety

All work triggered by schemas, instances, resolvers, formats, and custom
vocabularies MUST be bounded by explicit policy. Include limits for:

- input bytes;
- JSON nesting depth;
- schema resources and subschemas;
- reference depth and total reference work;
- dynamic and recursive scope depth;
- object members and array items;
- combinator branches;
- generated annotations and diagnostics;
- unique-item comparisons;
- regular-expression count, size, compilation, and evaluation;
- numeric precision and exponent size;
- resolver documents, redirects, and retrieval count;
- custom keyword and format work;
- total evaluation operations.

Limit failures MUST be typed and distinguishable from invalid-instance
results. The evaluator MUST remain panic-free, race-free, cancellation-aware,
and free of unbounded goroutine creation.

## Official Conformance Suite

Vendor or reproducibly retrieve a pinned official JSON Schema Test Suite
revision. The default local conformance command MUST run offline after normal
repository setup.

Requirements:

- run every mandatory fixture for all six released dialects;
- run every optional fixture whose capability is part of this goal;
- implement every optional capability represented by the official suite so
  the complete suite passes without exclusions;
- run remote-reference fixtures against the official `remotes` corpus;
- preserve official fixture files byte-for-byte;
- maintain a generated result manifest with draft, file, group, case count,
  pass count, skip count, failure count, suite revision, and checksum;
- require zero failures and zero unexplained skips;
- prevent fixture updates from silently reducing case counts;
- review upstream fixture changes before updating the pin;
- keep local regressions separate and trace them to issues or findings.

Full suite compatibility means all official cases pass. An allowlist of known
failures is not an acceptable release state.

## Bowtie Interoperability

Provide and maintain a Bowtie-compatible implementation harness so the
package can be tested through the ecosystem's cross-language compliance
tooling.

- Support every stable dialect implemented by the package.
- Run Bowtie locally or in CI against the pinned suite.
- Publish reproducible compliance results.
- Differentially investigate disagreements with mature independent
  implementations.
- Resolve disagreements from normative text; majority behavior is evidence,
  not authority.

## Testing Standard

Meaningful 100% production statement coverage is REQUIRED. Coverage must
prove behavior rather than merely execute lines.

Testing MUST include:

- official conformance fixtures for every dialect;
- unit tests for every keyword and dialect transition;
- meta-schema self-validation and schema-validity tests;
- boolean, object, and embedded schema tests;
- exact-number and arbitrary-precision boundaries;
- URI, anchor, JSON Pointer, recursive, and dynamic reference matrices;
- annotation and unevaluated-location tests;
- standard output golden fixtures;
- custom dialect, vocabulary, keyword, resolver, and format contract tests;
- malformed JSON, duplicate member, invalid UTF-8, deep nesting, and trailing
  input tests;
- aliasing, immutability, concurrency, cancellation, leak, and panic tests;
- adversarial resource-limit and algorithmic-complexity tests;
- regression tests for every defect discovered;
- fuzzing for JSON parsing, schema compilation, URI resolution, pointer
  evaluation, references, keyword evaluation, outputs, and round trips;
- race tests covering shared compiled schemas, registries, resolvers, and
  caches;
- benchmarks with `-benchmem` for representative and adversarial schemas;
- differential tests against independent implementations where useful;
- examples that compile and execute as tests.

Fuzz corpora MUST include official fixtures, historical draft differences,
deep recursive schemas, reference cycles, regex edge cases, huge numbers,
large objects and arrays, combinator explosions, and every fixed regression.

## Performance Requirements

Correctness and bounded behavior take priority over benchmark wins. After
correctness is proven:

- compile schemas once and reuse immutable plans;
- avoid repeated URI parsing and reference indexing;
- avoid decoding raw JSON more than necessary;
- avoid reflection and allocation churn in evaluator hot paths;
- use bounded caches with explicit ownership only where measurements justify
  them;
- benchmark compile and validate phases separately;
- report throughput, latency, allocations, peak memory, and scaling by schema
  and instance size;
- compare representative performance with maintained Go implementations;
- define regression budgets from reproducible baselines rather than guesses.

No optimization may change specification behavior, diagnostic correctness,
number precision, annotation semantics, cancellation, or resource limits.

## Documentation Deliverables

The package MUST ship with:

- README and quick start;
- complete public API documentation;
- architecture and evaluator lifecycle documentation;
- one conformance matrix per dialect;
- vocabulary and keyword matrices;
- official suite provenance and result documentation;
- Bowtie integration and published result instructions;
- dialect selection and migration guidance;
- resolver and secure remote-loading guide;
- custom vocabulary, keyword, and format guides;
- validation output guide;
- limits and resource-budget reference;
- security and threat-model documentation;
- performance methodology and benchmark results;
- adoption guide and realistic end-to-end examples;
- cookbook, FAQ, and troubleshooting guide;
- compatibility, deprecation, and versioning policy;
- changelog and release guide;
- contribution guide for adding fixtures and dialect behavior.

Documentation MUST state exactly what “full compliance” means and link claims
to executable evidence. It MUST distinguish normative behavior, implementation
policy, optional capability, and convenience API.

## Repository Automation

Local commands and root monorepo CI MUST provide:

- format and module checks;
- unit and integration tests;
- per-dialect official conformance jobs;
- a complete all-dialects suite job;
- meaningful 100% coverage enforcement;
- race testing;
- fuzz smoke and scheduled extended fuzzing;
- mutation testing;
- benchmarks and regression reporting;
- Bowtie harness validation;
- API compatibility checks;
- documentation and conformance-matrix checks;
- `go vet`, Staticcheck, strict `golangci-lint`, `govulncheck`, `gosec`, and
  the owned `analysis` policy suite;
- NilAway as a visible non-blocking warning until promoted by evidence;
- dependency, license, secret, and workflow-security checks;
- tagged module release automation using the monorepo-prefixed tag.

All commands MUST be runnable locally. CI configuration MUST not be the only
place where a quality gate can be executed.

## Ecosystem Integration

After stable compliance is proven:

- `openrpc` SHOULD replace its private limited Draft 7 schema model with
  this package or a deliberately narrow adapter over it;
- `api-query` SHOULD replace its external JSON Schema dependency where the
  owned package supplies the required behavior;
- `config` MAY expose optional schema validation;
- `jsonapi` MAY expose optional schema helpers without making JSON Schema a
  core JSON:API requirement;
- `service` MAY use it through explicit request/configuration integration;
- `validation` MUST remain the programmatic domain validation package;
- `wire` MUST remain responsible for wire encoding and decoding rather
  than schema semantics.

These migrations MUST occur only after conformance, API stability, and
performance are established. Do not make consumers depend on an incomplete
validator merely to remove third-party dependencies quickly.

## Release Requirements

The module path will be:

`github.com/faustbrian/golib/pkg/json-schema`

Releases MUST use directory-prefixed tags such as:

`json-schema/v1.0.0`

Do not publish `v1.0.0` until:

- every released-dialect official suite passes completely;
- all mandatory and optional suite fixtures pass without exclusions;
- Bowtie integration is reproducible;
- meaningful 100% production coverage is achieved;
- race, fuzz, mutation, vulnerability, API, documentation, and benchmark
  gates pass;
- resource policies and security behavior are documented and tested;
- every public API has complete documentation and examples;
- no known specification divergence or ambiguous compliance claim remains.

## Completion Criteria

This goal is complete only when:

- all six released official JSON Schema suites pass with zero failures and
  zero unexplained skips;
- every supported Core and Validation requirement maps to implementation,
  tests, fixtures, and documentation;
- schemas validate against the correct official meta-schemas;
- references, dynamic scope, vocabularies, annotations, formats, and outputs
  are implemented without known gaps;
- compiled schemas are immutable and safe for concurrent use;
- hostile schemas and instances cannot cause unbounded work, panic, race,
  deadlock, leak, uncontrolled network access, or silent precision loss;
- Bowtie can execute the implementation for every supported dialect;
- meaningful 100% coverage and all repository quality gates pass;
- documentation enables adoption without reading implementation source;
- integrations remain optional and dependency direction remains inward;
- the package can honestly claim complete compatibility with the pinned
  official JSON Schema Test Suite and full supported-dialect compliance.

File presence, line coverage, passing only common fixtures, or support for
Draft 2020-12 alone is not completion.
