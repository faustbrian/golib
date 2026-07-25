# Goal: Document Source Intent Across All Go Libraries

## Mission

Perform a repository-wide source documentation pass so maintainers can quickly
understand what each package and important type does, why non-obvious designs
exist, which invariants must remain true, and what can break when sensitive code
is changed.

This is an implementation task, not a comment-count exercise. Inspect every Go
module and package, understand behavior from code, tests, specifications, goals,
and existing documentation, then add or repair comments where they materially
reduce maintenance risk. Do not invent rationale that cannot be supported by
repository evidence.

Execute this goal independently from `.ai/GOAL_DOCUMENTATION.md`. That goal
owns repository and adoption documentation; this goal owns source-level Go
documentation and comments adjacent to implementation constraints.

## Governing Principles

- Comments MUST explain contracts, intent, rationale, invariants, tradeoffs,
  failure modes, or surprising implementation details.
- Comments MUST NOT narrate syntax or restate plainly readable code.
- Comment quality MUST NOT be measured by line count, percentage, or density.
- A complex implementation SHOULD first be simplified where possible; comments
  MUST NOT be used to excuse avoidable complexity or poor naming.
- Public API documentation MUST describe observable behavior rather than
  implementation trivia.
- Internal comments MUST be placed as close as practical to the constraint they
  protect.
- Broad architecture and multi-package behavior belongs in maintained docs;
  source comments SHOULD link to that material instead of duplicating it.
- Comments MUST remain accurate after the pass. Stale or misleading comments
  are defects and MUST be corrected or removed.
- Generated comments and repetitive AI prose MUST NOT be accepted without
  line-by-line human-quality review.
- The pass MUST preserve runtime behavior unless a separately tested defect is
  discovered and explicitly scoped.

## Scope

Audit every tracked Go module and package in `golib`, including:

- root packages and optional adapter packages;
- exported and unexported types, functions, methods, constants, and variables;
- package declarations and package-level documentation;
- concurrency, persistence, protocol, parsing, security, and performance code;
- tests, fuzz targets, benchmarks, examples, test helpers, and generated code;
- build-tagged and platform-specific files; and
- internal packages that encode behavior relied upon by multiple public APIs.

Vendor directories and externally generated source are excluded from manual
rewriting. Generated files MUST identify their generator and regeneration path.
Hand-maintained files MUST NOT falsely claim to be generated.

## Audit Inventory

Before editing, create a reproducible inventory for each module containing:

- packages lacking a useful package comment;
- exported declarations lacking valid Go documentation;
- exported comments that do not describe actual contracts;
- non-obvious algorithms and policy decisions without rationale;
- concurrency, ownership, lifecycle, and mutation assumptions;
- specification ambiguities or compatibility decisions;
- security-sensitive parsing, validation, cryptography, and trust boundaries;
- performance-sensitive code whose shape is intentionally non-obvious;
- persistence and distributed-system crash windows;
- TODO, FIXME, NOTE, WARNING, deprecated, and generated-code markers;
- comments contradicted by code, tests, goals, or specifications; and
- important comments that duplicate broader documentation and are likely to
  drift.

Store the audit evidence in a maintainable machine-readable or Markdown form.
The inventory MUST distinguish missing documentation from comments that require
technical investigation before they can be written truthfully.

## Package Documentation

Every package MUST have one canonical package comment that explains:

- its responsibility and primary abstraction;
- what it intentionally does not own;
- important safety, concurrency, or lifecycle properties;
- relevant specification and version where applicable;
- whether optional infrastructure or cgo dependencies are introduced; and
- the normal entry point for a new consumer.

Package comments MUST work on pkg.go.dev and through `go doc`. Avoid marketing,
unsupported completeness claims, installation tutorials, or duplicate README
content.

For packages with build variants, ensure documentation remains accurate for
every supported platform and tag combination.

## Exported API Contracts

Every exported declaration MUST have idiomatic Go documentation. Comments MUST
start with the declaration name where Go convention requires it and use complete
sentences.

Document all applicable observable behavior, including:

- valid inputs, normalization, defaults, and zero-value behavior;
- return values, typed errors, partial results, and error wrapping;
- whether nil is accepted, meaningful, rejected, or returned;
- ownership, copying, aliasing, mutation, and lifetime of inputs and outputs;
- concurrency safety and whether instances may be reused across goroutines;
- blocking behavior, cancellation, deadlines, and goroutine ownership;
- retry, idempotency, ordering, delivery, and transactional guarantees;
- resource limits, complexity, allocation behavior, and streaming behavior;
- panic behavior and conditions, if panics are part of the contract;
- security and trust assumptions visible to callers;
- stability, experimental status, deprecation, and migration guidance; and
- capability differences for provider or backend implementations.

Do not mechanically add every item to every comment. Include only applicable
contract information, but do not omit behavior a caller needs to use the API
safely.

Interfaces MUST document semantic obligations, not only method signatures.
Each interface and relevant method MUST state requirements that every
implementation must preserve. Provider and adapter implementations MUST
document deviations, unsupported capabilities, consistency models, and failure
semantics.

## Internal Rationale And Invariants

Add concise internal comments where code alone cannot safely communicate why it
has its current shape. Prioritize:

- algorithms with correctness conditions not obvious from local code;
- ordering constraints and state transitions;
- lock ordering, atomics, channels, worker shutdown, and goroutine lifetimes;
- immutable/copy-on-write boundaries and buffer ownership;
- database isolation, leases, fencing tokens, optimistic concurrency, outbox
  boundaries, and crash-recovery windows;
- retry, rate-limit, circuit-breaker, queue, and idempotency interactions;
- protocol edge cases and deliberate interpretations of ambiguous standards;
- parser depth, size, time, and memory limits;
- canonicalization, hashing, signature, key, and constant-time decisions;
- numerical precision, rounding, overflow, units, and representation choices;
- compatibility behavior retained for external consumers;
- deliberately unusual code retained because a simpler form changes behavior;
  and
- optimizations supported by benchmark or profile evidence.

A warning against changing code MUST identify the concrete invariant or failure
mode. Comments such as `do not touch`, `important`, or `magic` without an
explanation are prohibited.

When an invariant can be enforced by a type, test, assertion, static check, or
API shape, add that enforcement rather than relying only on prose. Keep the
comment when the reason still is not evident from the enforcement.

## Specifications And Compatibility

For specification-backed packages, comments adjacent to interpretation code
MUST identify:

- the specification name and pinned edition/version;
- the relevant section, production, keyword, or test-suite case;
- whether behavior is normative, recommended, implementation-defined, or an
  explicit resolution of ambiguity;
- interoperability constraints or known ecosystem behavior influencing the
  decision; and
- the tests or fixture inventory that protects the decision.

Use stable specification URLs or repository-local references. Do not paste
large normative excerpts into source comments. Do not claim compliance from a
comment alone.

Compatibility workarounds MUST name the affected system or version and the
condition for removing the workaround. Permanent interoperability behavior
MUST be documented as a contract rather than left as an unexplained workaround.

## Security-Sensitive Comments

Security comments MUST describe concrete trust boundaries and guarantees. They
SHOULD cover, where applicable:

- whether input is trusted, authenticated, canonicalized, or attacker-controlled;
- why validation occurs before allocation, parsing, comparison, or IO;
- secret ownership, redaction, erasure limitations, and accidental-copy risks;
- cryptographic primitive, randomness, nonce, key, and signature assumptions;
- SSRF, path traversal, entity expansion, decompression, regex, and parser
  resource boundaries;
- timing-sensitive comparisons and operations; and
- what is deliberately left to the caller or deployment environment.

Never put real credentials, internal endpoints, personal data, exploit payloads,
or operational secrets into comments or examples.

## Concurrency And Lifecycle Comments

Types with mutable state or background activity MUST clearly document:

- whether they are safe for concurrent use;
- which methods may run concurrently;
- who owns starting, stopping, closing, draining, and waiting;
- whether `Close` is idempotent and what happens after it returns;
- callback and middleware reentrancy expectations;
- lock ordering or callback-under-lock restrictions;
- whether caller-provided functions may block or panic;
- cancellation propagation and maximum shutdown behavior; and
- whether returned slices, maps, buffers, iterators, or snapshots remain valid
  after subsequent calls.

These guarantees MUST agree with race tests and actual implementation. Do not
label code thread-safe merely because it currently passes a narrow race test.

## Performance Comments

Comment performance-motivated implementation choices only when evidence exists.
Each such comment SHOULD identify:

- the workload or hot path being protected;
- the allocation, latency, throughput, or memory concern;
- the benchmark, profile, or complexity argument supporting the shape; and
- which apparently simpler change would regress the measured property.

Do not preserve speculative micro-optimizations through authoritative-sounding
comments. If evidence no longer supports the optimization, simplify the code or
update the rationale.

## TODO And Warning Policy

Review every `TODO`, `FIXME`, `HACK`, `NOTE`, and `WARNING` marker.

- Actionable debt MUST link to a tracked issue or include enough owner and
  trigger context to be actionable under repository policy.
- A TODO MUST state the missing behavior and why it is not safe or appropriate
  to implement immediately.
- A FIXME MUST identify the known incorrect behavior and affected boundary.
- A HACK MUST explain the external constraint, evidence, and removal condition.
- A WARNING MUST describe a concrete hazard and safe modification procedure.
- Completed, obsolete, unactionable, or speculative markers MUST be removed.
- Permanent design rationale MUST be written as ordinary documentation, not a
  perpetual TODO.

Avoid introducing a taxonomy of comment prefixes merely for visual uniformity.
Use markers only when their semantics add value.

## Examples, Tests, And Benchmarks

- Exported APIs SHOULD have runnable examples for important usage and contract
  edges, but examples MUST complement rather than substitute for API comments.
- Complex test helpers and fixtures MUST explain the behavior or external case
  they model when that is not obvious.
- Regression tests MUST identify the defect or invariant being protected when
  the assertion alone does not explain it.
- Fuzz targets MUST state their input boundary and important invariants.
- Benchmarks MUST state the operation, setup excluded from measurement, input
  shape, and comparison constraints.
- Comments MUST NOT overfit tests by narrating every arrange/act/assert step.

## Automated Enforcement

Add or standardize locally runnable checks that detect at least:

- missing package comments;
- missing or malformed exported declaration comments;
- stale generated-code headers and unavailable generators;
- invalid deprecation syntax;
- broken local and specification links where practical;
- forbidden secret patterns in comments and examples;
- TODO markers that violate repository policy; and
- documentation examples that do not compile or pass.

Use `go vet`, Staticcheck documentation checks, the repository's strict
`golangci-lint` configuration, and a small owned AST-based check where existing
tools cannot enforce repository policy cleanly. Tool rules MUST NOT contradict
one another. NilAway remains warning-only under the existing policy and is not
a substitute for documenting nil contracts.

Automated checks MUST enforce objective structure. They MUST NOT score prose,
require comments on every unexported declaration, or reward verbose filler.

## Execution Plan

1. Inventory all modules, packages, exported declarations, markers, and
   high-risk implementation areas.
2. Define and add objective comment checks without changing runtime behavior.
3. Document package contracts and exported APIs module by module.
4. Add rationale and invariant comments to security, concurrency, protocol,
   persistence, numerical, and performance-sensitive internals.
5. Review TODOs, generated files, examples, tests, fuzz targets, and benchmarks.
6. Run formatting, documentation, static-analysis, test, race, fuzz-smoke, and
   repository drift checks for every touched module.
7. Perform a second technical review specifically for correctness, usefulness,
   duplication, stale claims, and AI-generated filler.
8. Update package changelogs where public API documentation clarifies or changes
   a user-visible contract.
9. Record completion evidence and unresolved rationale questions without
   guessing answers.

Process modules in dependency-aware batches and commit incrementally. Do not
combine broad behavioral refactors with this pass. If code must change to make a
contract truthful or enforceable, isolate and test that change separately.

## Verification

For every module, run and record the exact repository-standard commands for:

- formatting and generated-file checks;
- `go doc` or equivalent package documentation inspection;
- `go vet` and strict lint/static analysis;
- unit and example tests;
- race tests for affected concurrency contracts;
- targeted fuzz smoke tests for documented hostile-input boundaries;
- architecture and repository drift checks; and
- link and documentation validation.

Review rendered pkg.go.dev-style documentation for representative foundational,
protocol, infrastructure, and application-support packages. A passing linter is
necessary but not sufficient evidence of useful documentation.

## Deliverables

- Complete package comments and exported API documentation across all modules.
- Focused internal rationale and invariant comments in high-risk code.
- Corrected or removed stale and low-value comments.
- An auditable comment inventory and completion report per module.
- Automated objective checks runnable locally and in CI.
- Updated examples and changelogs where contract clarification requires them.
- A register of unresolved questions that require specification, security, or
  domain decisions instead of invented explanations.

## Completion Criteria

This goal is complete only when every module has been audited, all exported APIs
and packages have accurate idiomatic documentation, high-risk implementation
constraints are understandable adjacent to the code, objective checks run in CI
and locally, rendered documentation is useful, stale comments are removed, and
all verification evidence is recorded.

Completion MUST be rejected if comments merely restate code, warning comments
lack concrete failure modes, concurrency or ownership guarantees are ambiguous,
specification decisions remain unexplained, generated prose has not been
technically reviewed, or any module was skipped because it appeared simple.
