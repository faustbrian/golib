# Goal: Audit and Harden `json-schema`

## Mission

Perform an evidence-driven specification, security, interoperability,
compatibility, and resource-safety audit of `json-schema`, then implement
every justified change required for a trustworthy JSON Schema implementation.

Do not assume that 100% coverage, a green official fixture run, a Bowtie
report, or a previous release proves correctness. Reconstruct compliance from
normative specifications, trace every claim through production code and
executable evidence, and attack evaluator behavior that official fixtures do
not fully exercise.

The release target is complete compatibility with every official JSON Schema
Test Suite case for Draft 3, Draft 4, Draft 6, Draft 7, Draft 2019-09, and
Draft 2020-12, with no known specification divergence.

## Authoritative Inputs

- Every released Core and Validation specification supported by the package.
- Official meta-schemas and vocabulary meta-schemas.
- The pinned official JSON Schema Test Suite revision and remote fixtures.
- Official output schemas and examples.
- Normative referenced URI, JSON Pointer, Unicode, regular-expression,
  numeric, date/time, hostname, IP, UUID, media type, and content standards.
- Bowtie's harness protocol and cross-implementation reports.
- The package's `.ai/GOAL.md`, public API, source, tests, fuzzers, benchmarks,
  matrices, documentation, workflows, changelog, and release artifacts.

Use primary sources and stable section links. Every finding MUST classify the
source of the requirement as normative specification, official fixture,
interoperability expectation, security policy, package policy, or convenience
behavior.

## Audit Rules

- Establish a reproducible baseline before changing production behavior.
- Add a failing regression before every behavioral fix.
- Never weaken, skip, patch, or reinterpret an official fixture to make it
  pass.
- Keep official fixtures byte-identical to their pinned upstream revision.
- Resolve conflicting implementations from normative text, not popularity.
- Treat unknown or ambiguous specification behavior as an investigation, not
  permission to invent convenient semantics.
- Preserve compatibility unless existing behavior is non-conformant, unsafe,
  or demonstrably defective.
- Document every breaking correction and migration requirement.
- Keep hardening commits focused and update the changelog for each
  user-visible change.

## Phase 1: Baseline And Traceability

1. Inventory every exported type, function, method, option, error, dialect,
   vocabulary, keyword, format, resolver, cache, limit, output, goroutine,
   mutable state location, dependency, fixture, fuzzer, and benchmark.
2. Rebuild the supported-dialect matrix directly from each normative
   specification.
3. Map every official fixture to its evaluator path and expected assertion.
4. Verify the suite revision, checksums, case counts, licenses, remote corpus,
   and updater behavior.
5. Run all local format, module, test, conformance, coverage, race, fuzz,
   mutation, benchmark, API, documentation, vulnerability, static analysis,
   and workflow checks.
6. Record every skip, flake, panic, timeout, suppression, platform difference,
   and environmental blocker.
7. Produce a threat model for malicious schemas, instances, resolvers,
   remote servers, custom formats, custom vocabularies, and concurrent callers.
8. Record every documented claim without executable evidence and every
   implemented behavior absent from documentation.

Do not proceed from aggregate pass/fail summaries. Retain per-draft, per-file,
per-group, and per-case evidence.

## Dialect Audit

For Draft 3, Draft 4, Draft 6, Draft 7, Draft 2019-09, and Draft 2020-12,
prove independently:

- dialect identification and caller-selected defaults;
- boolean and object schema support where applicable;
- meta-schema loading and schema validation;
- identifier, base URI, anchor, and fragment behavior;
- every validation keyword and accepted schema form;
- every applicator and annotation rule;
- reference sibling behavior;
- unknown keyword behavior;
- vocabulary declaration and required-vocabulary behavior;
- format annotation and assertion behavior;
- content vocabulary behavior;
- evaluated properties/items propagation;
- output-location and diagnostic behavior;
- dialect transitions and forbidden cross-draft leakage.

Build a differential matrix for every keyword whose spelling, schema form,
default, annotation, or interaction changed between drafts. Include at least
identifier keywords, definitions, dependencies, exclusive bounds, array tuple
forms, contains limits, property names, conditionals, recursive references,
dynamic references, unevaluated keywords, and format policy.

No shared implementation optimization may erase a dialect difference.

## Parser And Data Model Audit

Prove behavior for:

- empty, malformed, truncated, and trailing JSON;
- duplicate object members;
- invalid UTF-8 and Unicode edge cases;
- deep JSON values and very wide arrays or objects;
- exact integer, decimal, exponent, negative zero, and arbitrary-precision
  semantics;
- numbers outside `float64` and machine-integer ranges;
- boolean schemas and invalid top-level schema values;
- non-mutation and ownership of caller-provided maps, slices, raw bytes, and
  custom data models;
- deterministic decoding and compilation;
- reader limits, cancellation, and partial reads;
- stable typed errors without embedding entire sensitive inputs.

Do not permit `encoding/json` defaults to introduce silent precision loss or
duplicate-member ambiguity without an explicit, tested package policy that is
compatible with the specifications.

## Resource And Reference Audit

Prove URI and reference behavior for:

- relative and absolute identifiers;
- base URI changes at every nested resource boundary;
- percent encoding, normalization, empty fragments, and equivalent URIs;
- JSON Pointer escape sequences and array indexes;
- plain-name anchors;
- embedded and compound resources;
- canonical and non-canonical identifiers;
- self references and multi-resource cycles;
- recursive anchors and references;
- dynamic anchors, dynamic references, and dynamic scope shadowing;
- references into unknown or invalid resources;
- duplicate identifiers and anchors;
- retrieval aliases and cache identity;
- official remote fixtures;
- resolver cancellation, timeout, redirects, body limits, and failure causes.

The core MUST perform no implicit network access. Audit optional network
resolvers for SSRF, DNS rebinding assumptions, redirect escapes, credential
leakage, proxy behavior, decompression bombs, response-body cleanup, and
connection reuse.

Prove that cyclic graphs terminate correctly without rejecting valid cycles
or allowing recursion exhaustion.

## Keyword And Evaluation Audit

For every keyword and vocabulary:

- verify compilation-time schema-form validation;
- verify runtime assertion behavior;
- verify annotation behavior;
- verify interactions with sibling applicators;
- verify short-circuit behavior does not corrupt annotations or outputs;
- verify deterministic diagnostics;
- verify exact instance and keyword locations;
- verify no caller-owned input mutation;
- verify evaluation budgets account for all work;
- verify custom keyword callbacks cannot corrupt evaluator state.

Pay particular attention to:

- `allOf`, `anyOf`, `oneOf`, `not`, and conditionals;
- `contains`, `minContains`, and `maxContains`;
- `unevaluatedProperties` and `unevaluatedItems` across nested applicators;
- `dependentSchemas`, dependencies, and property dependencies;
- `additionalProperties`, `propertyNames`, and pattern properties;
- tuple and homogeneous item evaluation across drafts;
- `uniqueItems` equality across every JSON type and exact number form;
- numeric divisibility and range comparisons without floating-point error;
- annotation collection from successful versus failed branches;
- content encoding, media type, and nested content schema;
- default, examples, deprecated, read-only, and write-only annotations.

## Regular Expression Audit

Audit every gap between the JSON Schema regular-expression requirements and
Go's standard `regexp` implementation.

- Build an explicit ECMA-262 compatibility matrix.
- Test Unicode character classes, escapes, code points, anchors, multiline
  behavior, lookarounds, backreferences, and unsupported syntax.
- Prove `pattern` and `patternProperties` behavior for non-ASCII input.
- Reject unsupported required syntax explicitly rather than silently changing
  meaning.
- Prevent catastrophic evaluation, excessive compilation, and unbounded
  regex caches.
- Differentially test against a conforming ECMA-262 implementation where
  useful.

The package MUST NOT claim full specification compliance while retaining an
undocumented regular-expression semantic gap merely because current official
fixtures pass.

## Format And Content Audit

For every format supported by every dialect:

- identify the normative source and dialect applicability;
- distinguish annotation from assertion;
- test ASCII and Unicode boundaries;
- test valid, invalid, ambiguous, obsolete, and security-sensitive values;
- test custom format registration, replacement, panic, latency, cancellation,
  concurrency, and resource budgets;
- ensure format errors do not expose secrets;
- verify content encoding and media type behavior;
- verify nested content schema evaluation where supported.

Do not broaden or narrow RFC semantics for convenience. Any intentionally
optional stricter application policy belongs outside the core format
implementation.

## Vocabulary And Extension Audit

Prove:

- immutable instance-owned dialect and vocabulary registries;
- duplicate and conflicting registration behavior;
- required unknown vocabulary failure;
- optional unknown vocabulary handling;
- keyword name collisions;
- vocabulary dependency and ordering behavior;
- custom keyword schema validation;
- compile and evaluation callback panic containment;
- callback cancellation and budget accounting;
- registry reuse across goroutines;
- no global registration races or cross-test contamination;
- custom behavior cannot silently redefine built-in dialect semantics.

Extension APIs MUST be powerful enough for real vocabularies without exposing
mutable evaluator internals.

## Output And Diagnostic Audit

For Flag, Basic, Detailed, and Verbose output, prove:

- validity and omission rules;
- instance locations;
- keyword locations;
- absolute keyword locations;
- nested error and annotation structure;
- branch and applicator representation;
- dialect-aware output behavior;
- deterministic ordering policy;
- JSON serialization against official output schemas;
- non-aliasing of internal buffers;
- diagnostic count and size limits;
- redaction of schema and instance values.

Human-friendly diagnostics MAY supplement standard output but MUST not replace
or distort it.

## Concurrency And Lifecycle Audit

Prove:

- compiled schemas are immutable and safe for parallel validation;
- registries and resolvers have documented concurrency contracts;
- caches are bounded, synchronized, observable, and explicitly owned;
- cancellation terminates evaluator and resolver work promptly;
- custom callbacks cannot deadlock internal locks through re-entry;
- no goroutine, timer, ticker, response body, file, or temporary resource
  leaks;
- panic containment does not hide context cancellation or corrupt future use;
- concurrent validation results do not alias shared mutable state;
- repeated compile/validate cycles do not cause unbounded retained memory.

Run race tests under representative shared-schema and adversarial resolver
loads.

## Algorithmic Complexity Audit

Construct adversarial tests and benchmarks for:

- deeply nested combinators;
- exponential branch combinations;
- large `oneOf` and `anyOf` sets;
- nested unevaluated tracking;
- large unique arrays;
- many pattern properties and regexes;
- long numeric values and exponents;
- huge object member sets;
- long reference chains and cyclic graphs;
- many embedded resources and anchors;
- verbose output amplification;
- custom keyword and format callbacks;
- resolver fan-out and repeated aliases.

Every input-controlled growth dimension MUST have a documented limit,
complexity expectation, cancellation point, or other evidence-backed bound.
Limit errors MUST remain distinguishable from ordinary invalid-instance
results.

## Official Suite Audit

- Verify the pinned suite is authentic and complete.
- Recount drafts, files, groups, cases, optional cases, and remote fixtures.
- Compare counts against upstream before every pin update.
- Run every case without exclusions.
- Confirm optional suites are enabled rather than silently ignored.
- Verify draft selection matches the fixture directory.
- Verify remote fixture URIs resolve exactly as the suite requires.
- Detect accidental fixture mutation in CI.
- Ensure the runner itself has tests for expected-valid, expected-invalid,
  malformed fixture, missing remote, duplicate case, and count regression.
- Publish exact per-dialect and aggregate results.

Zero failures with skipped capabilities is not full compatibility. The final
report MUST state failure and skip counts explicitly.

## Bowtie And Differential Audit

- Validate the harness protocol and implementation metadata.
- Run all supported dialects through Bowtie.
- Compare results with mature implementations.
- Minimize every disagreement to a focused local regression.
- Classify disagreements as local defect, peer defect, suite defect,
  specification ambiguity, or harness defect.
- Link upstream issues where the suite or specification is involved.
- Do not adopt majority behavior without normative justification.

## API And Compatibility Audit

- Inventory nil traps, unsafe zero values, ambiguous options, hidden defaults,
  ownership confusion, and errors that cannot be inspected.
- Verify exported documentation matches runtime behavior.
- Treat accepted schemas, validation results, output shape, ordering, error
  classification, default limits, resolver behavior, and format policy as
  SemVer-governed behavior.
- Prefer additive corrections where possible.
- Document every unavoidable breaking conformance correction with exact before
  and after behavior and migration examples.
- Ensure optional integration packages do not create dependency cycles.
- Verify core users do not inherit heavy adapter dependencies.

## Test Quality Requirements

- Maintain meaningful 100% production statement coverage.
- Review branch and behavioral coverage, not only statements.
- Use mutation testing to prove assertions detect realistic defects.
- Add a focused regression before every production fix.
- Fuzz parsers, compilers, references, URIs, pointers, regexes, formats,
  custom vocabularies, evaluation, outputs, and round trips.
- Seed fuzzers from official fixtures and every historical regression.
- Add race, leak, cancellation, panic, aliasing, and resource-bound tests.
- Keep tests deterministic and independent of public network services.
- Ensure examples compile and prove documented behavior.
- Benchmark representative and hostile compile and validate workloads with
  `-benchmem`.
- Reject coverage-only tests that cannot detect a behavioral mutation.

## Documentation Audit

Verify that documentation completely and accurately covers:

- supported drafts and exact compliance meaning;
- all public APIs;
- dialect selection and defaults;
- schema compilation and validation lifecycle;
- exact JSON number handling;
- reference resolution and secure loading;
- vocabularies, custom keywords, and formats;
- standard validation outputs;
- limits, cancellation, and error classification;
- concurrency and ownership;
- official suite provenance and results;
- Bowtie integration;
- performance methodology;
- security model;
- adoption, examples, cookbook, FAQ, troubleshooting, and migration;
- compatibility, deprecation, release, and contribution policies.

Every compliance badge or statement MUST link to current executable evidence.

## Required Verification

Run and report the repository's actual commands for:

- formatting;
- module tidiness and verification;
- unit and integration tests;
- every per-dialect official fixture suite;
- the aggregate official suite;
- meta-schema validation;
- meaningful 100% coverage;
- race testing;
- fuzz smoke and extended fuzzing;
- mutation testing;
- Bowtie harness and interoperability;
- benchmarks with allocations;
- API compatibility;
- documentation and matrix validation;
- `go vet`, Staticcheck, strict lint, owned analysis, NilAway warnings,
  vulnerability scanning, security analysis, dependency review, license
  review, secret scanning, and workflow validation.

Hosted CI is final external confirmation, not a substitute for complete local
evidence. Complete every unaffected local gate before reporting an external
blocker.

## Required Deliverables

1. A hardening report with severity, category, normative source, evidence,
   reproduction, impact, and disposition for every finding.
2. Rebuilt per-dialect Core, Validation, vocabulary, keyword, format,
   reference, and output conformance matrices.
3. Exact official suite manifests and Bowtie reports.
4. A threat model and resource-limit matrix.
5. Focused regressions, fuzz corpora, fixes, and benchmarks.
6. Updated API, security, conformance, performance, adoption,
   troubleshooting, compatibility, migration, and changelog documentation.
7. A release-readiness verdict with exact commands, results, residual risks,
   and semantic-version recommendation.

## Release Blockers

- Any official fixture failure or unexplained skip.
- Any unproven supported-dialect normative requirement.
- Any known reference, vocabulary, annotation, format, output, numeric, regex,
  or meta-schema divergence.
- Any uncontrolled network access, SSRF path, resource-exhaustion path,
  unbounded cache, panic, race, deadlock, leak, or precision loss.
- Any misleading compliance statement or badge.
- Missing meaningful 100% coverage.
- A failing required local or GitHub Actions gate.

## Completion Criteria

Hardening is complete only when:

- every official case for all six released dialects passes with zero failures
  and zero unexplained skips;
- every normative supported-dialect requirement has traceable implementation,
  test, fixture, and documentation evidence;
- meta-schema, reference, dynamic scope, vocabulary, annotation, format, and
  output behavior has no known gap;
- regular-expression and exact-number semantics have explicit compliance
  evidence beyond fixture coverage;
- all high- and medium-severity findings are fixed or rejected with primary
  evidence and documented rationale;
- every untrusted-input growth dimension is bounded or otherwise proven safe;
- compiled schemas and all shared paths are race-free and lifecycle-safe;
- Bowtie results are reproducible and disagreements are dispositioned;
- meaningful coverage, mutation, fuzz, race, security, API, documentation,
  and benchmark gates pass without unexplained skips;
- the final report states exactly what was verified without overstating
  compliance or production readiness.

Passing only the official suite, reaching line coverage, or documenting known
gaps is not hardening completion.
