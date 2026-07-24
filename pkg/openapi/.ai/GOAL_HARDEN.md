# Goal: Audit and Harden openapi

## Mission

Perform an evidence-driven specification, security, interoperability,
compatibility, concurrency, and resource-safety audit of `openapi`, then
implement every justified correction required for a trustworthy full OpenAPI
implementation.

Do not assume that meaningful 100% coverage, green official schemas, passing
examples, or a successful self-round trip proves correctness. Reconstruct every
compliance claim from normative prose and attack behavior that ordinary fixtures
do not exercise.

## Authoritative Inputs

- OpenAPI 3.2.0, 3.1.2, 3.0.4, and 2.0 normative specifications.
- Applicable earlier patch specifications accepted by the package.
- Official schemas, examples, registries, errata, and source repositories.
- Applicable JSON Schema dialect, vocabulary, URI, JSON Pointer, HTTP, media
  type, OAuth, OpenID Connect, XML, Unicode, and security specifications.
- Package goals, source, tests, fuzzers, benchmarks, docs, decisions, matrices,
  changelog, workflows, and release artifacts.
- Independent implementation behavior only as interoperability evidence, never
  as authority over normative text.

Pin exact revisions, licenses, checksums, and update procedures for all imported
evidence. Official artifacts MUST remain byte-identical to their pinned sources.

## Audit Rules

- Establish a reproducible baseline before behavior changes.
- Add a failing regression before each behavioral correction.
- Never weaken, skip, patch, or reinterpret official evidence to pass tests.
- Resolve implementation conflicts from normative text and recorded decisions.
- Treat ambiguity as an investigation requiring an explicit decision record.
- Preserve compatibility unless behavior is non-conformant, unsafe, or
  demonstrably defective.
- Document every breaking correction and migration path.
- Keep hardening commits focused and update the changelog for user-visible work.
- Separate observed facts, specification requirements, package policy, and
  inferences in all reports.

## Phase 1: Baseline And Traceability

Inventory every:

- supported version, object, field, conditional rule, extension location, and
  normative requirement;
- parser mode, serializer mode, builder, visitor, resolver, cache, composition,
  conversion, diff, generator, CLI command, and adapter;
- exported API, error code, option, default, limit, goroutine, lock, mutable
  state location, and dependency;
- official artifact, local fixture, conformance case, skip, suppression, fuzzer,
  benchmark, example, and documentation claim; and
- remote or filesystem access path, trust boundary, sensitive-data path, and
  resource-amplification opportunity.

Run all repository-standard format, build, test, conformance, coverage, race,
fuzz, mutation, benchmark, API, documentation, vulnerability, dependency,
license, and workflow checks. Retain per-version and per-rule evidence rather
than only aggregate summaries.

## Version And Model Audit

For OpenAPI 3.2, 3.1, 3.0, and Swagger 2.0 independently:

- reconcile every object and field against normative prose;
- exhaust required, optional, nullable, empty, patterned, fixed, defaulted,
  deprecated, and extension behavior;
- verify legal object locations and cross-object conditions;
- prove version detection and explicit caller-selected version behavior;
- reject unsupported future versions and invalid version combinations;
- test every patch-level clarification that affects behavior;
- verify lossless unknown-field and extension policies;
- prove exact-number and source-location preservation; and
- prevent shared implementation from leaking semantics across versions.

Generate field-presence and conditional-rule tests from a reviewed normative
matrix where practical. Generated evidence MUST remain traceable to source
sections and receive semantic review.

## Parser And Serialization Audit

Prove behavior for JSON and YAML across:

- empty, malformed, truncated, trailing, concatenated, and multi-document input;
- duplicate keys, invalid UTF-8, Unicode edge cases, and non-string mapping keys;
- exact integers, decimals, exponents, negative zero, and huge numbers;
- deep and wide structures, huge scalars, aliases, anchors, merge keys, tags,
  directives, and alias cycles;
- source locations, ordering, comments, scalar styles, and preserving mode;
- partial readers, cancellation, reader errors, writer errors, and output limits;
- semantic equivalence between JSON and YAML; and
- deterministic canonical and preserving round trips.

Differentially test independent JSON and YAML implementations where useful.
Reject ambiguous input instead of silently accepting parser-specific behavior.

## Normative Validation Audit

Rebuild validation from the requirement matrix and prove every rule for:

- metadata, servers, variables, paths, path items, and operations;
- parameters, request bodies, responses, headers, media types, and encodings;
- callbacks, webhooks, links, examples, runtime expressions, and operation
  references;
- components, reusable objects, name collisions, and reference target types;
- security schemes, OAuth flows, scopes, OpenID Connect, mutual TLS, and
  security requirement alternatives and conjunctions;
- tags, external documentation, XML, discriminators, extensions, and direction;
- all OpenAPI Schema Object dialect behavior; and
- every legacy Swagger 2.0 object and conversion-sensitive rule.

Test required/optional pairs, boundary values, sibling interactions, nested
contexts, and multiple simultaneous failures. Fail-fast and collect modes MUST
select identical rule outcomes and stable diagnostic order.

Verify every diagnostic code, path, source location, severity, specification
link, safe message, wrapping behavior, and maximum amplification. Diagnostics
MUST remain stable enough for automation without freezing human wording
unnecessarily.

## JSON Schema Dialect Audit

- Prove OpenAPI 3.2 and 3.1 base dialect and vocabulary behavior.
- Prove `jsonSchemaDialect` and per-schema `$schema` resolution.
- Run complete applicable `json-schema` conformance and integration cases.
- Verify OpenAPI keywords, annotations, formats, discriminator behavior,
  read/write direction, examples, defaults, and exact numbers.
- Prove OpenAPI 3.0's extended subset does not inherit accidental Draft 4 or
  later behavior.
- Prove Swagger 2.0 remains separate from OpenAPI 3.x schema semantics.
- Test boolean schemas, references, recursion, dynamic scope, composition,
  unevaluated behavior, and custom vocabularies where applicable.
- Prevent duplicate, contradictory, or missing diagnostics between OpenAPI and
  JSON Schema validation phases.

No full OpenAPI compliance claim is valid while Schema Object behavior contains
an undocumented dialect gap.

## Reference And Resolver Audit

Prove:

- URI parsing, normalization, resolution, percent encoding, and fragment rules;
- JSON Pointer escaping, array indexes, empty pointers, and invalid pointers;
- retrieval, canonical, base, and alias identities;
- nested resource boundaries, anchors, dynamic anchors, and schema scopes;
- internal, relative, external, file, and remote references;
- legal cycles, invalid cycles, repeated targets, and graph termination;
- Reference Object sibling differences by OpenAPI version;
- bundling, dereferencing, provenance, and explicit loss diagnostics;
- cancellation, deduplication, cache identity, and concurrent resolution; and
- cleanup after timeout, failure, redirect, short read, and cancellation.

Attack optional network and filesystem resolvers for SSRF, DNS rebinding
assumptions, redirect allowlist escapes, local and private addresses, proxy
behavior, credential forwarding, path traversal, symlink escape, decompression
bombs, content-type confusion, stale caches, and poisoned aliases.

Enforce bytes, documents, redirects, depth, fan-out, concurrency, duration,
diagnostic, and decompression limits before expensive work.

## Parameter And Runtime Expression Audit

For every legal parameter location and style/explode combination:

- test primitive, array, object, empty, reserved, Unicode, and ambiguous values;
- verify path, query, header, cookie, form, and legacy collection behavior;
- prove percent encoding, delimiters, ordering, duplicate keys, and deep objects;
- test round trips only where the specification provides invertible semantics;
- compare against independent implementations with identical policy; and
- reject unsupported combinations explicitly.

For runtime expressions, test every grammar production, request/response
source, header/query/path lookup, body pointer, missing value, invalid pointer,
escaping case, length limit, and cancellation path. Evaluation MUST never
execute code or trigger remote access.

## Construction, Composition, And Conversion Audit

- Prove builders cannot emit invalid objects silently.
- Test ownership, defensive copying, immutable updates, and caller mutation.
- Property-test deterministic visitation and serialization after edits.
- Exhaust merge conflicts across paths, methods, components, security, servers,
  tags, extensions, and references.
- Verify component renaming rewrites every legal reference and no unrelated
  textual value.
- Preserve provenance across composition and filtering.
- Test every version conversion for explicit loss, retained extensions,
  diagnostics, and repeatability.
- Reject impossible conversion rather than synthesizing misleading semantics.

## Diff And Compatibility Audit

Create directional request and response compatibility matrices for every
OpenAPI construct. Test:

- added, removed, narrowed, widened, reordered, renamed, and re-referenced
  operations and schemas;
- requiredness, defaults, nullability, enum, numeric, string, array, and object
  constraints;
- media types, encodings, parameters, responses, callbacks, webhooks, links,
  security, and server changes;
- resolved semantic equality despite source or reference layout changes;
- conditional and unknown classifications requiring caller policy; and
- deterministic output and bounded complexity on large or cyclic documents.

Differential results MAY inform gaps but MUST NOT override documented package
compatibility policy.

## Security Audit

Build and execute a threat model covering:

- malicious JSON, YAML, schemas, references, remote servers, and files;
- duplicate-key, Unicode, number, and parser-differential attacks;
- YAML aliases, references, cycles, regexes, decompression, diagnostics, and
  generated-output amplification;
- Markdown/HTML/script content consumed by downstream renderers;
- source-code, comment, identifier, path, and template injection in generators;
- secrets in URLs, headers, examples, defaults, extensions, errors, telemetry,
  fixtures, snapshots, and CLI output;
- integer overflow, allocation bombs, recursion, cancellation, panic, and
  goroutine leaks; and
- dependency compromise and malicious fixture updates.

Fuzz every untrusted parser, validator, expression, resolver boundary,
serializer, composition operation, diff operation, conversion, and CLI decoder.
Use adversarial corpora and complexity assertions, not only random bytes.

## Concurrency And Lifecycle Audit

- Race-test parsed and built models, validators, compiled schemas, resolvers,
  registries, caches, visitors, generators, and hooks.
- Verify caller inputs and returned maps, slices, byte buffers, and diagnostics
  do not alias mutable internal state unexpectedly.
- Test cancellation at every phase and every blocking boundary.
- Detect goroutine, file, response-body, timer, and temporary-file leaks.
- Define cache ownership, eviction, invalidation, close behavior, and behavior
  after close.
- Prove callbacks are not invoked under locks unless explicitly documented and
  safe.
- Run deterministic parallel repetitions to detect ordering and shared-state
  defects.

## Generator And CLI Audit

If generation or CLI functionality exists:

- run hostile identifiers, descriptions, examples, paths, schemas, and
  extensions through every output target;
- compile and test generated Go artifacts;
- prove deterministic output, source fingerprints, formatting, and clean
  regeneration;
- reject unsupported constructs without narrowing them silently;
- verify atomic file replacement, path containment, permissions, and cleanup;
- test stable exit codes and machine-readable diagnostics; and
- ensure network access remains disabled unless explicitly configured.

Optional tooling MUST NOT weaken core dependency, security, or conformance
guarantees.

## Interoperability Audit

- Parse and validate representative public descriptions from independent
  ecosystems without treating popularity as correctness.
- Differentially compare `getkin/kin-openapi`, `pb33f/libopenapi`, applicable
  `openapi` packages, and other maintained tools on matched behavior.
- Round-trip through independent tools where semantics can be preserved.
- Test every official adapter against the exact supported companion version.
- Verify optional adapters do not pull dependencies into core or create cycles.
- Maintain a documented matrix of true incompatibilities, implementation bugs,
  and deliberate package policies.

## Performance And Complexity Audit

- Benchmark parse, model, validate, resolve, serialize, bundle, compose,
  convert, diff, generation, valid input, invalid input, cold and warm behavior.
- Measure latency distributions, throughput, allocations, peak memory, resolver
  calls, diagnostic count, and cancellation latency.
- Use tiny, typical, large, cyclic, reference-heavy, schema-heavy, and hostile
  corpora with published provenance.
- Compare competitors only under matched dialect, validation depth, resolver
  state, limits, diagnostics, and outputs.
- Add regression budgets for proven hot paths and complexity boundaries.
- Never trade correctness, bounds, or diagnostics for benchmark rankings.

## Testing Discipline

Meaningful 100% production statement coverage remains REQUIRED. Strengthen it
with:

- normative requirement and field-matrix tests;
- official and independent fixtures with provenance;
- table, property, metamorphic, model, differential, and round-trip tests;
- parser, validator, resolver, expression, conversion, and diff fuzzing;
- race, cancellation, leak, aliasing, and deterministic-parallel tests;
- mutation tests targeting every normative branch and resource guard;
- failure injection for readers, writers, resolvers, caches, hooks, and files;
- cross-version and cross-platform tests; and
- reproducible benchmark and complexity evidence.

Coverage exclusions MUST be narrowly justified, generated-code specific, and
must not hide production behavior. A green line percentage without meaningful
assertions is a release blocker.

## Static Analysis And Supply Chain

Run repository-standard formatting, `go vet`, strict golangci-lint,
Staticcheck, govulncheck, dependency review, license review, API compatibility,
generated-file, and workflow checks. NilAway MUST run visibly as advisory and
MUST NOT fail CI under current policy.

Audit every dependency for necessity, ownership, maintenance, release process,
license, vulnerabilities, transitive graph, and replacement strategy. Pin all
tools and fixture sources reproducibly. Core MUST remain free of unnecessary
framework, router, network, generator, and competitor dependencies.

## Documentation Audit

Verify every public API, option, error, default, limit, supported version,
feature, extension, resolver, adapter, and CLI command is documented accurately.

Documentation MUST include:

- exact support and conformance status;
- specification decisions and unresolved ambiguities;
- JSON Schema dialect and version differences;
- parser, YAML, reference, security, resource-limit, and trust-boundary policy;
- examples for realistic design-first and code-first adoption;
- migration and compatibility guidance for corrected behavior;
- fair benchmark methodology and raw evidence; and
- known limitations without presenting planned work as implemented.

Compile and test every example. Check links, snippets, badges, generated docs,
and pkg.go.dev rendering. Update the changelog for every user-visible correction.

## Release Gates

Release MUST be blocked by any of the following:

- an unimplemented, untested, or undocumented claimed normative requirement;
- an unexplained official artifact failure or modified official fixture;
- version leakage or a lossy model for a claimed supported version;
- a JSON Schema dialect or OpenAPI Schema Object conformance gap;
- JSON/YAML semantic divergence under documented accepted input;
- implicit remote loading or an unresolved SSRF/file-disclosure path;
- unbounded recursion, aliases, references, diagnostics, generation, or diff;
- parser panic, race, goroutine/resource leak, cancellation failure, or mutable
  aliasing defect;
- unstable or unsafe diagnostics and sensitive-data exposure;
- silent conversion, composition, diff, or generation loss;
- misleading interoperability or benchmark claims;
- meaningful production coverage below 100%; or
- any blocking local or CI quality gate failure.

## Completion Criteria

Hardening is complete only when every supported OpenAPI version and normative
requirement is reconciled against implementation and executable evidence;
official and independent suites pass with no unexplained skips; parsing,
validation, schemas, references, serialization, composition, conversion, diff,
and optional tooling survive adversarial tests within enforced limits; race,
fuzz, mutation, leak, vulnerability, API, documentation, supply-chain, and
performance gates pass; all findings are resolved or explicitly release-
blocking; and the changelog and conformance evidence are current.
