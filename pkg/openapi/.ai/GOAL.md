# Goal: Complete OpenAPI Specification Foundation

## Objective

Build `openapi` as a serious, production-grade, open-source Go
implementation of the complete OpenAPI Specification. It MUST provide
version-aware, lossless models, bounded JSON and YAML parsing, complete
normative validation, reference resolution, deterministic serialization,
composition, semantic diffing, and first-class JSON Schema integration without
intentional specification divergences or undocumented gaps.

This is not a thin document struct, validator wrapper, annotation scanner, or
framework-specific generator. Design-first documents MUST be first-class, and
all optional code-first or generation features MUST preserve the complete
specification model.

## Specification Commitment

Stable support MUST cover:

- OpenAPI 3.2.0;
- OpenAPI 3.1.2, including all normative behavior of the 3.1 line;
- OpenAPI 3.0.4, including all normative behavior of the 3.0 line; and
- OpenAPI/Swagger 2.0 through an explicitly separated legacy dialect.

Patch releases within a supported minor line MUST follow OpenAPI compatibility
rules without assuming they are textually identical. Earlier 3.0.x and 3.1.x
patch documents MUST be accepted where the governing specification permits
them and tested against version-specific differences.

The implementation MUST:

- pin exact authoritative specification revisions at implementation time;
- inventory every normative MUST, MUST NOT, SHALL, REQUIRED, SHOULD, and MAY;
- map every normative requirement to implementation, tests, and documentation;
- track official schemas, examples, registries, errata, and fixture provenance;
- treat normative prose as authoritative over informative schemas and examples;
- distinguish specification requirements from package policy and convenience;
- reject unsupported future versions instead of guessing their semantics; and
- maintain explicit support and conformance matrices per specification line.

Arazzo and OpenAPI Overlay are separate specifications. They MAY receive
dedicated packages and goals but MUST NOT be implied by OpenAPI compliance.

## Product Position

`openapi` MUST be:

- independently useful without a router, service framework, or HTTP runtime;
- design-first, code-first, and hybrid friendly without favoring reflection;
- lossless for all supported standard fields and specification extensions;
- immutable or ownership-safe after parsing or construction;
- deterministic and safe for concurrent read, validation, and serialization;
- explicit about version, dialect, resolver, validation, and resource policy;
- usable with JSON or YAML without semantic drift;
- secure for untrusted descriptions and referenced resources; and
- credible as a public dependency outside this library ecosystem.

It MUST NOT require hidden network access, filesystem access, global mutable
registries, background goroutines, telemetry exporters, or application
framework initialization.

## Complete Version-Aware Model

Provide complete typed models for every object and field in each supported
specification, including:

- root documents, info, contact, license, terms, external documentation, tags,
  and specification extensions;
- servers, server variables, paths, path items, operations, parameters, request
  bodies, responses, headers, examples, links, callbacks, and webhooks;
- media types, encodings, discriminators, XML metadata, OAuth flows, security
  schemes, and security requirements;
- components and every reusable component map;
- reference objects and legal sibling behavior by specification version;
- OpenAPI Schema Objects with their version-specific JSON Schema dialects;
- OpenAPI 3.2 additions and changed semantics without narrowing them into a
  3.1-shaped model;
- legacy Swagger 2.0 host, base path, schemes, consumes, produces, definitions,
  parameters, responses, security definitions, and collection formats; and
- all required, optional, nullable, defaulted, patterned, and extension fields.

Do not force all versions into one lossy superset. Shared abstractions MAY
reduce duplication only where semantics are genuinely identical. Version-
specific behavior MUST remain visible and testable.

The model MUST distinguish absent, explicit zero, explicit empty, and explicit
null where the applicable specification distinguishes them. Unknown standard
fields MUST follow an explicit strict or preserving policy. `x-` extension
values MUST remain lossless.

## JSON Schema Integration

Use `json-schema` as the canonical schema foundation through an explicit,
acyclic integration boundary.

- OpenAPI 3.1 and 3.2 Schema Objects MUST preserve their complete JSON Schema
  dialect behavior plus OpenAPI vocabularies and base-dialect rules.
- OpenAPI 3.0 Schema Objects MUST implement the exact extended subset defined
  by OpenAPI 3.0 rather than pretending to be a standard JSON Schema draft.
- Swagger 2.0 schema behavior MUST remain separately versioned and validated.
- `jsonSchemaDialect`, per-schema `$schema`, base dialects, vocabularies,
  references, dynamic scope, formats, and annotations MUST be correct.
- Boolean schemas, arbitrary keywords, exact numbers, nullable behavior,
  discriminators, read/write direction, examples, defaults, and composition
  MUST retain their version-specific meaning.
- Schema validation and OpenAPI semantic validation MUST produce distinguishable
  diagnostics without duplicate or contradictory findings.

Schema inference from Go types MAY be an optional convenience. It MUST NOT be
required for document construction or represented as complete when reflection
cannot preserve the source schema semantics.

## Parsing And Data Representation

Provide bounded JSON and YAML ingestion with one semantic data model.

Parsing MUST define and test:

- duplicate object or mapping keys;
- invalid UTF-8, Unicode normalization boundaries, and non-string YAML keys;
- exact integer, decimal, exponent, and negative-zero handling;
- YAML anchors, aliases, merge keys, tags, directives, and multi-document input;
- comments, source locations, scalar styles, ordering, and preserving mode;
- trailing data, empty input, byte-order marks, and malformed streams;
- maximum bytes, tokens, depth, width, aliases, scalar size, and diagnostics;
- cancellation and short-reader behavior; and
- JSON/YAML semantic equivalence and deterministic diagnostics.

Strict parsing and source-preserving parsing MAY be separate modes. Preserving
mode MUST not weaken validation or permit ambiguous semantics. Parsing untrusted
input MUST NOT panic or perform implicit IO.

## Validation

Implement complete, phased validation for every supported version:

- syntax and data-model validity;
- required fields, types, formats, patterns, ranges, and fixed values;
- object-specific and cross-object normative rules;
- path template and path parameter correspondence;
- operation identifiers, parameters, headers, media types, and response keys;
- parameter location, style, explode, allow-empty, and serialization rules;
- callbacks, webhooks, links, runtime expressions, and operation references;
- server variables, expansion, defaults, and relative URL rules;
- security schemes, OAuth flows, scopes, mutual TLS, OpenID Connect, and
  security requirement composition;
- component names, uniqueness, reference targets, and object-type compatibility;
- Schema Object dialect and vocabulary behavior;
- XML, encoding, discriminator, example, default, and direction constraints;
- specification extension naming and placement; and
- all version transitions and legacy rules.

Official schemas MAY be used as supporting evidence but MUST NOT replace prose-
derived semantic validation.

Diagnostics MUST include stable machine-readable codes, severity, source
location, JSON Pointer or equivalent path, specification version and section,
concise safe messages, and wrapped causes where relevant. Fail-fast and collect
modes MUST share rule semantics and deterministic ordering.

## References And Resource Resolution

Implement complete URI, URI-reference, JSON Pointer, anchor, and Reference
Object semantics for every supported version.

- Resolve internal, relative, embedded, file, and remote resources only through
  explicit resolver interfaces.
- External resolution MUST be disabled by default.
- Base URI changes, retrieval URI, canonical URI, fragments, aliases, redirects,
  anchors, dynamic anchors, and schema resource boundaries MUST remain distinct.
- Legal cycles MUST resolve without infinite recursion; invalid cycles MUST
  produce bounded diagnostics.
- Reference Object sibling behavior MUST follow the active OpenAPI version.
- Repeated references MAY be deduplicated only without changing identity,
  diagnostics, cancellation, or provenance.
- Bundling and dereferencing MUST preserve semantics or report exact loss.

Network and file resolver adapters MUST expose allowlists and limits for
schemes, hosts, paths, redirects, bytes, documents, depth, duration, and
concurrency. They MUST defend against SSRF, local-file disclosure, redirect
escapes, credential forwarding, DNS rebinding assumptions, decompression bombs,
and cache poisoning.

## Serialization And Canonicalization

Provide deterministic JSON and YAML serialization for each supported version.

- Preserve all standard fields, extensions, exact numbers, references, and
  meaningful absent/null/empty distinctions.
- Canonical output MUST define ordering, scalar representation, escaping, and
  normalization suitable for hashing and reproducible fixtures.
- Source-preserving output MUST document exactly which comments, styles,
  anchors, order, and source details survive edits.
- Parse/serialize/parse MUST preserve semantic equivalence.
- Serialization MUST reject values impossible in the selected OpenAPI version.
- Output limits and cancellation MUST prevent unbounded generation.

Canonicalization is a package policy, not an OpenAPI compliance claim, and MUST
be documented as such.

## Construction, Traversal, And Composition

- Provide explicit constructors and typed builders without hiding required
  specification concepts.
- Prefer immutable updates or unambiguous ownership and defensive-copy rules.
- Registries MUST reject duplicate operation IDs, component names, and other
  collisions where required.
- Deterministic visitors and walkers MUST preserve source and reference context.
- Merge and composition MUST define conflict, ordering, provenance, component
  renaming, reference rewriting, path overlap, security, and extension policy.
- Filtering MUST preserve a valid description or return structured diagnostics.
- No builder may silently generate a narrower document than requested.

Reflection, tags, annotations, router inspection, and handler registration MUST
remain optional adapters. The core MUST never require application discovery.

## Runtime Expressions And Parameter Serialization

Implement complete parsing and validation for OpenAPI runtime expressions used
by callbacks and links. Evaluation, if provided, MUST use typed caller-owned
request/response context, remain deterministic and bounded, and never execute
code.

Provide specification-accurate parameter serialization and deserialization
utilities for path, query, header, cookie, and applicable legacy locations.
Support every legal style/explode/allowReserved combination, deep objects,
arrays, objects, delimiters, encoding, and ambiguity policy. These utilities
MUST remain independent from a specific router or HTTP framework.

## Semantic Diff And Compatibility

Provide semantic diff primitives that classify changes as additive, compatible,
conditionally compatible, breaking, or unknown across:

- paths, operations, parameters, request bodies, responses, callbacks,
  webhooks, links, servers, security, tags, and extensions;
- media types, encodings, schemas, examples, defaults, and discriminators;
- references based on resolved semantics while retaining source provenance; and
- version upgrades and legacy conversion.

Compatibility rules MUST distinguish request and response direction. Policy-
dependent classifications MUST remain explicit and configurable. Source text
diffs MUST NOT masquerade as semantic compatibility analysis.

## Conversion And Migration

Conversions among Swagger 2.0, OpenAPI 3.0, 3.1, and 3.2 MUST be optional,
directional, and loss-aware.

- Never claim lossless conversion where source concepts have no target form.
- Emit structured loss and manual-action diagnostics.
- Preserve vendor extensions unless a caller policy rejects them.
- Keep original provenance available for audits.
- Provide migration guidance and fixtures for common conversions.

Conversion is not a prerequisite for parsing or validating any supported
version.

## Optional Code Generation And CLI

After core conformance is complete, optional packages MAY generate Go client
and server contracts, validators, examples, fixtures, or documentation.

Generation MUST be deterministic, formatted, reproducible, injection-safe, and
fingerprinted with source and tool versions. It MUST handle naming collisions,
reserved words, optional/nullable distinctions, recursive schemas, composition,
content negotiation, authentication, errors, and unsupported constructs
explicitly. It MUST fail rather than silently narrow behavior.

An optional CLI MAY validate, format, bundle, resolve, diff, convert, inspect,
and generate. Commands MUST expose stable exit codes, machine-readable output,
resource limits, and no hidden network access.

## Interoperability

Provide optional, dependency-safe integrations with:

- `json-schema` for complete Schema Object behavior;
- `router`, `http-middleware`, and `service` for runtime description
  adapters without coupling the core model to them;
- `validation` for optional application-level validation mapping;
- `http-client` for generated or description-driven clients;
- `authentication` and `authorization` for security metadata adapters;
- `wire` for explicit JSON/YAML representation boundaries; and
- `telemetry` for opt-in bounded instrumentation hooks.

Adapters MUST preserve dependency direction and MUST NOT introduce circular
imports or reduce OpenAPI concepts to the target framework's feature subset.

## Concurrency And Observability

- Parsed, built, and compiled descriptions MUST be immutable or explicitly
  ownership-safe for concurrent reads.
- Resolver, registry, cache, and generator concurrency MUST define ownership,
  cancellation, deduplication, and shutdown.
- No hidden goroutines, process-global registries, implicit caches, or exporters.
- Optional hooks MAY report bounded phase, outcome, diagnostic count, reference
  count, and duration.
- Hooks and errors MUST NOT expose document bodies, secrets, credentials, or
  unbounded URLs and labels.

## Security And Resource Bounds

Threat-model malicious descriptions, schemas, YAML, references, resolvers,
extensions, examples, Markdown/HTML, generators, and diff inputs.

Bound input bytes, tokens, nesting, aliases, strings, arrays, maps, operations,
parameters, schemas, references, fetched resources, redirects, diagnostics,
runtime expressions, generated output, composition, and diff complexity.

The package MUST defend against:

- SSRF, local file disclosure, path traversal, and credential leakage;
- reference, cycle, YAML alias, decompression, regex, and diagnostic bombs;
- parser differentials, duplicate keys, Unicode collisions, and number loss;
- Markdown/HTML/script injection in downstream renderers;
- template, identifier, comment, and source-code injection in generators;
- integer overflow, excessive allocation, stack exhaustion, and goroutine leaks;
  and
- sensitive data exposure through errors, hooks, logs, or fixtures.

Production code MUST NOT use unsafe, cgo, `go:linkname`, hidden mutable globals,
or unowned background work.

## Package Shape

Prefer focused packages such as:

- root package for shared contracts, versions, diagnostics, and document APIs;
- `oas32`, `oas31`, `oas30`, and `swagger20` for lossless versioned models;
- `parse` for bounded JSON/YAML parsing and source maps;
- `validate` for complete normative validation;
- `jsonschema` for OpenAPI Schema Object integration;
- `reference` for URI, pointer, resolver, bundle, and dereference behavior;
- `expression` for runtime expressions;
- `parameter` for serialization and deserialization;
- `compose` for merge, filtering, and provenance;
- `diff` for semantic compatibility analysis;
- `convert` for explicit loss-aware migrations;
- `generate` for optional code generation;
- `cmd/openapi` for optional tooling; and
- `openapitest` for fixtures, builders, conformance, and adapter test suites.

Package boundaries MUST follow measured cohesion and dependency direction rather
than this list mechanically.

## Testing And Quality Standard

Meaningful 100% production statement coverage is REQUIRED. Coverage MUST be
backed by assertions that detect normative, security, interoperability, and
failure defects rather than merely execute every line.

Required evidence includes:

- requirement-by-requirement conformance matrices for every supported version;
- official schemas, examples, registries, and independently derived fixtures;
- exhaustive object/field/conditional/version-difference test matrices;
- JSON/YAML semantic-equivalence and preserving-round-trip tests;
- official JSON Schema and OpenAPI Schema Object integration evidence;
- URI, JSON Pointer, reference, cycle, bundling, and resolver vectors;
- parameter serialization and runtime-expression vectors;
- property tests for parse/serialize, composition, conversion, and diff;
- differential tests against independent mature OpenAPI implementations;
- fuzz tests for parsers, validators, references, expressions, diff, and CLI;
- race, cancellation, leak, aliasing, and immutability tests;
- failure injection for resolvers, readers, writers, and generators;
- mutation testing for every normative and security-sensitive decision; and
- benchmarks with allocations for each independently comparable operation.

No official fixture may be modified or skipped to make the suite pass. Local
regressions MUST remain separate and traceable.

## Comparative Benchmarks

Compare fairly against `getkin/kin-openapi`, `pb33f/libopenapi`, applicable
`openapi` ecosystem packages, and direct JSON/YAML parser baselines.

- Separate Swagger 2.0, OpenAPI 3.0, 3.1, and 3.2 tracks.
- Separate decode, parse, model, validate, resolve, serialize, bundle, compose,
  diff, valid, invalid, cold, warm, local, and remote-reference behavior.
- Use identical documents, dialects, resolver state, limits, diagnostics, and
  output requirements.
- Validate correctness before ranking performance.
- Disqualify unsupported or incorrect candidates from ranked tracks while still
  publishing diagnostic results.
- Report latency distributions, throughput, allocations, peak memory, input
  corpus, hardware, Go version, commands, and raw data.

The package MUST aim to be competitive but MUST NOT weaken correctness,
security, or diagnostics to win a benchmark.

## Documentation Deliverables

Provide:

- README and five-minute design-first, code-first, parsing, validation, and
  reference quick starts;
- complete API documentation for every exported contract and option;
- version and conformance matrices with direct requirement mapping;
- guides for JSON/YAML, Schema Objects, references, parameters, callbacks,
  webhooks, links, security, extensions, composition, conversion, and diff;
- secure resolver and hostile-input guidance;
- code generation and CLI documentation if those features exist;
- architecture, specification decisions, compatibility, migration,
  interoperability, limits, performance, and benchmark methodology;
- cookbook, adoption guide, FAQ, troubleshooting, contribution, release,
  security, and maintained changelog documentation; and
- runnable examples for all principal user-facing scenarios.

Users MUST be able to adopt the package without reading implementation source.
Documentation MUST distinguish implemented, experimental, planned, deprecated,
and unsupported capabilities.

## Repository And CI Requirements

Set up cohesive repository infrastructure consistent with the other libraries:

- current supported Go version policy and reproducible tool versions;
- formatting, `go vet`, strict linting, Staticcheck, and advisory NilAway;
- unit, example, conformance, integration, race, fuzz-smoke, mutation, coverage,
  vulnerability, dependency, license, API, docs, and benchmark workflows;
- fixture provenance and checksum verification;
- generated-file and clean-worktree verification;
- README badges for every blocking workflow and important advisory workflow;
- local commands matching every CI job; and
- strict changelog maintenance for every user-visible change.

Tool configurations MUST be strict but coherent and MUST NOT enforce
contradictory rules.

## Non-Goals

- No HTTP router, server framework, controller binder, service container, API
  gateway, authentication runtime, or authorization engine.
- No mandatory reflection, annotation scanning, code generation, CLI, network
  resolver, YAML dependency, or framework adapter in the core.
- No claim that a valid OpenAPI description proves the described service
  behaves correctly.
- No proprietary dialect presented as OpenAPI compliance.
- No implicit remote loading or execution of examples, links, scripts, or
  embedded content.
- No automatic sanitization claim for Markdown or HTML merely because a
  description was parsed or validated.

## Completion Criteria

The package is complete only when every claimed specification version and
normative requirement has executable evidence; models are lossless and version-
correct; JSON and YAML are semantically equivalent under documented policy;
Schema Object behavior is complete; references are explicit, bounded, secure,
and cancellation-aware; diagnostics are stable; all promised utilities and
adapters satisfy their contracts; meaningful 100% coverage is achieved; and all
local and CI quality gates pass.

A package that parses common OpenAPI 3 documents, validates against official
schemas, or wraps an existing implementation does not satisfy this goal.
