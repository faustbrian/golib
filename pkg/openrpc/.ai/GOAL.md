# Goal: Complete OpenRPC Specification Foundation

## Objective

Build a serious, production-grade open-source Go implementation of the complete
OpenRPC Specification with no intentional specification divergences or omitted
normative features.

The package MUST provide lossless document models, strict validation, parsing,
canonical and preserving serialization, references, JSON Schema integration,
runtime expressions, service discovery, builders, overlays, compatibility
tooling, and first-class integration with JSON-RPC services. It MUST remain
usable independently from `jsonrpc` while providing an official adapter for
it.

## Specification Commitment

- The current stable OpenRPC specification at implementation time is
  authoritative.
- Every normative MUST, MUST NOT, SHALL, REQUIRED, SHOULD, and MAY is inventoried
  in a traceable conformance matrix.
- OpenRPC patch/minor compatibility follows the specification's versioning
  rules; supported versions are explicit and tested.
- The official meta-schema, examples, specification extensions, and errata are
  tracked with provenance and reproducible updates.
- No exported API may silently weaken, reinterpret, or omit a specification
  requirement.
- Unsupported future versions fail explicitly rather than being accepted under
  guessed semantics.
- Intentional implementation choices concern Go API shape only, never document
  or protocol compliance.

## Complete Document Object Model

- OpenRPC document/root object and specification version.
- Info, contact, license, external documentation, and server objects.
- Server variables, defaults, enums, and substitution semantics.
- Method, tag, content descriptor, error, link, example pairing, and example
  objects.
- Components and every reusable component map defined by the specification.
- Reference objects in every location where the specification permits them.
- JSON Schema Draft 7 schema objects with boolean schemas and lossless keywords.
- Specification extensions on every extensible object.
- Required versus optional fields, explicit null handling where legal, defaults,
  deprecation, summaries, descriptions, and rich text behavior.
- Unknown standard fields are preserved or rejected only through explicit mode;
  extension fields remain lossless.

## JSON Schema Integration

- Preserve complete Draft 7 JSON Schema values used by OpenRPC without reducing
  them to reflection-generated Go subsets.
- Validate schema objects and references against the dialect and OpenRPC rules.
- Support inline schema, boolean schema, component reference, external reference,
  recursive structures, examples, formats, composition, and annotations.
- Schema inference from Go types MAY be an optional convenience but MUST NOT be
  required for document construction or treated as complete automatically.
- Generated schemas require deterministic naming, collision handling, recursion
  protection, custom overrides, and explicit unsupported-type diagnostics.
- JSON Schema dependency choice MUST be justified, pinned, compatibility-tested,
  and isolated behind package-owned contracts where replacement risk warrants.

## References And Resolution

- Parse, classify, normalize, and resolve internal and external references.
- Full JSON Pointer escaping and resolution behavior.
- URI base, relative resolution, fragments, cycles, aliases, and repeated targets
  have explicit semantics.
- Resolver interfaces support files, embedded documents, HTTP through optional
  adapters, and caller-provided stores without mandatory network behavior.
- Core parsing and validation perform no hidden network or filesystem access.
- Reference depth, fetched bytes, document count, redirects, schemes, hosts,
  recursion, and resolution time are bounded by policy.
- Bundling and dereferencing preserve semantics and report cycles or loss.

## Runtime Expressions And Links

- Parse, validate, and evaluate every runtime-expression form defined by the
  specification.
- Link parameter and server-variable expressions use typed evaluation context.
- Missing values, invalid pointers, unsupported payload shapes, and coercion
  behavior are explicit.
- Evaluation is deterministic, bounded, panic-free, and never executes code.
- Runtime expressions and JSON Pointer behavior are exhaustively tested against
  official and independently derived vectors.

## Parsing, Validation, And Serialization

- Strict JSON parser with duplicate-key detection and bounded diagnostics.
- Structural, type, semantic, reference, schema, expression, uniqueness, and
  cross-object validation phases.
- Validation results include stable machine-readable codes, JSON pointers,
  severity, specification references, and concise safe messages.
- Fail-fast and collect modes share identical rule semantics.
- Canonical serialization is deterministic for hashing, fixtures, and generated
  output while respecting JSON object semantics.
- Preserving parse/serialize mode retains specification extensions and unknown
  future fields according to policy.
- Invalid UTF-8, huge numbers, excessive nesting, duplicate patterned fields,
  unknown fields, trailing data, and resource exhaustion fail predictably.
- Parsing an untrusted document MUST NOT panic or trigger implicit I/O.

## Builders, Registries, And Composition

- Typed builders prevent invalid intermediate states where practical without
  hiding required specification concepts.
- Immutable or ownership-safe document construction with deterministic output.
- Method and component registries detect duplicate names and reference
  collisions.
- Merge, overlay, filter, and compose operations define conflict, ordering,
  component renaming, and reference-rewrite semantics precisely.
- Security-filtered documents can expose an empty method list while remaining
  valid where the specification permits it.
- Reflection and code-first helpers remain optional; design-first documents are
  first-class and never forced through Go handler registration.

## Service Discovery

- Complete OpenRPC service discovery method support as defined by the
  specification.
- Static and dynamically generated document providers.
- Context-aware filtering for authorization or deployment visibility through a
  caller-owned policy contract.
- Discovery output caching, revisioning, ETag/hash, and invalidation are explicit
  optional concerns without hidden mutable globals.
- Integration with `jsonrpc` preserves notification, request, response,
  error, batch, and transport semantics.
- Discovery adapters do not make `openrpc` depend on a specific server,
  router, HTTP framework, or transport.

## Code Generation And Tooling

- Generators MAY produce Go client/server contracts, validators, documentation,
  examples, or fixtures only after the core model and validation are complete.
- Generated output is deterministic, formatted, reproducible, and includes a
  source-document fingerprint and tool version.
- Name collisions, reserved words, nullable/optional distinctions, schema
  composition, recursive types, errors, links, and unsupported constructs are
  handled explicitly.
- Generators MUST fail rather than silently narrowing a schema or method.
- A CLI MAY validate, format, bundle, diff, inspect, and generate documents;
  every command has stable exit codes and machine-readable output.

## Compatibility And Diffing

- Semantic diff identifies additive, compatible, conditionally compatible, and
  breaking changes across methods, parameters, results, errors, schemas,
  servers, links, examples, and components.
- Compatibility policy distinguishes `by-name` and `by-position` parameter
  structures.
- Reference changes are evaluated on resolved semantics without losing source
  location diagnostics.
- CI tooling can compare a candidate document against a released baseline.
- Diff classification is documented and configurable only where the
  specification leaves application semantics open.

## Interoperability

- Official adapter for `jsonrpc` method registration and service discovery.
- Optional `api-query` content-descriptor adapters without placing query
  semantics in OpenRPC core.
- `validation`, `wire`, `http-client`, and `localized` integration
  through optional packages without dependency cycles.
- Import/export and validation against official meta-schema and ecosystem
  documents from multiple independent OpenRPC implementations.
- Support design-first, code-first, and hybrid adoption with identical document
  compliance requirements.

## Concurrency And Observability

- Parsed and built documents are immutable or explicitly ownership-safe for
  concurrent validation and serialization.
- Resolver and registry concurrency has explicit ownership, cancellation,
  deduplication, and shutdown semantics.
- No hidden goroutine, process-global registry, background network fetch, or
  telemetry exporter.
- Optional hooks report bounded phase, outcome, diagnostic count, reference
  count, and duration without document payloads, schema values, URLs containing
  secrets, or unbounded labels.

## Security And Resource Bounds

- Bound input bytes, nesting, tokens, methods, parameters, components, schemas,
  diagnostics, pointers, references, resolver depth, fetched resources,
  expression length, generated output, and diff complexity.
- External resolution is disabled by default and protected against SSRF, local
  file disclosure, redirect abuse, decompression bombs, and scheme confusion.
- Threat-model malicious schemas, reference cycles, parser differentials,
  Unicode collisions, duplicate JSON keys, regex denial of service, runtime
  expression abuse, and generator code injection.
- Errors and hooks never expose full documents, credentials, or fetched bodies.
- Production code MUST NOT use unsafe, cgo, `go:linkname`, or unbounded globals.

## Non-Goals

- No replacement for JSON-RPC request execution, transports, authentication,
  authorization, business validation, API gateway, or generic OpenAPI tooling.
- No OpenRPC-like proprietary extension that diverges from the specification.
- No mandatory reflection, code generation, network resolver, CLI, or
  `jsonrpc` dependency for core document use.
- No claim that a structurally valid OpenRPC document proves the described
  service behaves correctly at runtime.

## Package Shape

- Root: complete document model, construction, errors, and version support.
- `parse`: strict and preserving JSON parsers with resource policies.
- `validate`: complete rule engine, diagnostics, conformance mapping, meta-schema.
- `jsonschema`: Draft 7 preservation, validation integration, and optional
  inference contracts.
- `reference`: pointer/URI resolution, stores, bundling, and dereferencing.
- `expression`: runtime-expression parsing and evaluation.
- `discovery`: transport-neutral service discovery contracts.
- `jsonrpc`: optional official `jsonrpc` adapter.
- `diff`: semantic API compatibility analysis.
- `generate`: optional deterministic generators after core conformance.
- `cmd/openrpc`: optional validation, formatting, bundling, diff, and generation.
- `openrpctest`: conformance fixtures, builders, assertions, and test server.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Every normative
specification requirement MUST map to executable conformance evidence; merely
executing lines does not satisfy this requirement.

Required evidence includes:

- requirement-by-requirement OpenRPC conformance matrix tied to tests
- official meta-schema, examples, specification extension, and discovery tests
- exhaustive object required/optional/default/extension/unknown-field matrices
- Draft 7 schema, reference, JSON Pointer, URI, runtime expression, and link
  interoperability vectors
- strict parser fuzzing for duplicate keys, UTF-8, numbers, depth, size,
  patterned fields, extensions, and malformed documents
- resolver fuzzing and SSRF/file/scheme/redirect/decompression security tests
- differential validation and round trips across independent ecosystem tooling
- property tests for canonicalization, preserving serialization, bundling,
  dereferencing, overlays, registries, and semantic diff
- mutation tests for every normative validation, required field, uniqueness,
  reference, expression, and compatibility decision
- race, cancellation, deduplication, leak, and immutable ownership tests
- benchmarks with allocations for parse, validate, serialize, resolve, bundle,
  diff, discovery, large schemas, and hostile bounded inputs

## Documentation Deliverables

- Five-minute design-first, code-first, validation, discovery, and `jsonrpc`
  integration quickstarts.
- Complete API reference for every exported model, validator, resolver, builder,
  diagnostic, policy, adapter, CLI command, and error.
- OpenRPC specification conformance matrix with direct requirement mapping.
- Adoption guides for existing JSON-RPC services, static documents, dynamic
  discovery, schema reuse, references, extensions, CI diffing, and generation.
- Cookbook examples for every OpenRPC object and realistic service scenario.
- Security model, resolver threat model, performance guide, FAQ,
  troubleshooting, compatibility, architecture, migration, roadmap,
  contribution guide, and maintained changelog.
- Every public API and user-facing scenario MUST be documented sufficiently for
  adoption without reading implementation source.

## Automation And Release

Use the latest stable Go release as the minimum at implementation time. Pin all
tools and dependencies. GitHub Actions MUST run formatting, vet, Staticcheck,
strict golangci-lint, advisory NilAway, tests, meaningful 100% coverage, race,
fuzz smoke, mutation checks, complete specification conformance, meta-schema and
ecosystem interoperability, vulnerability scans, benchmarks, docs, API
compatibility, and releases. Every blocking command MUST be reproducible locally
through documented `make` targets.

Repository setup MUST include README badges for every blocking workflow/job,
Dependabot, security policy, contribution guide, code of conduct, Apache/spec
attribution review, license, notice and third-party notices, release automation,
changelog, repository topics, and complete adoption documentation.

## Execution Plan

1. Pin authoritative specification inputs and build the normative requirement
   and object-field conformance matrices.
2. Implement the lossless document model, strict parser, serializer, versions,
   extensions, and diagnostics.
3. Implement full validation, Draft 7 schemas, references, runtime expressions,
   links, and service discovery.
4. Add builders, composition, semantic diff, `jsonrpc`, and optional adapters.
5. Complete security, fuzz, mutation, race, interoperability, and performance
   hardening.
6. Add optional generation/CLI surfaces only where they preserve full fidelity.
7. Publish complete adoption and specification documentation and release v1.

## Acceptance Criteria

- Every normative OpenRPC requirement has traceable executable evidence.
- Every specification object, field, extension point, reference, expression,
  and discovery behavior is represented without intentional gaps.
- Documents parse, validate, serialize, resolve, and round-trip without semantic
  loss where accepted.
- External resolution is explicit, bounded, cancellation-aware, and secure by
  default.
- `jsonrpc` integration supports complete discovery without coupling core.
- Meaningful 100% coverage and every required GitHub Actions gate pass.
