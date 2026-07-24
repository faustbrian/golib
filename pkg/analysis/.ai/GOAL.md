# Goal: Organization-Grade Go Static Analysis

## Objective

Build a serious open-source suite of deterministic `go/analysis` analyzers that
enforces architecture, dependency direction, context propagation, lifecycle,
resource ownership, secure API usage, and organization-wide Go engineering
policy across all shared packages and services.

The project MUST complement the compiler, `go vet`, Staticcheck, golangci-lint,
gosec, govulncheck, CodeQL, race tests, fuzzing, and advisory NilAway. It MUST
not duplicate mature checks without a concrete semantic gap.

## Product Principles

- Every diagnostic represents a precise, documented, testable policy violation.
- False-positive resistance is more important than maximizing finding count.
- Rules operate on Go syntax, types, facts, and control/data flow as needed;
  textual matching is insufficient for semantic policy.
- Diagnostics provide stable rule IDs, exact locations, rationale, and safe
  remediation guidance.
- Suggested fixes are offered only when semantics can be preserved reliably.
- Organization-specific policy is configuration; reusable analysis remains OSS.
- Suppressions are narrow, explicit, attributable, and auditable.

## Analyzer Platform

- Standard `golang.org/x/tools/go/analysis` analyzers.
- Standalone multichecker command.
- `go vet -vettool` compatible binary.
- golangci-lint v2 module plugin only if the plugin lifecycle remains stable and
  reproducible; standalone execution remains supported.
- `analysistest` fixtures for every accepted and rejected construct.
- Machine-readable JSON/SARIF output through a stable reporting layer.
- Stable rule metadata: ID, category, severity, default status, rationale,
  remediation, introduced version, and configuration schema.

## Initial Rule Families

### Architecture And Dependencies

- Enforce configured layer and bounded-context import directions.
- Forbid infrastructure dependencies from domain/application packages.
- Forbid backend client use outside approved adapters.
- Enforce package-specific forbidden imports and module dependencies.
- Detect cycles or boundary bypass that ordinary Go import-cycle checks miss.
- Support explicit package patterns and reviewed exceptions.

### Context And Blocking Operations

- Require `context.Context` propagation on configured blocking public APIs.
- Forbid `context.Background()` and `context.TODO()` below approved composition
  roots and test boundaries.
- Detect context stored in structs where lifecycle ownership is unsafe.
- Detect dropped cancellation functions and replacement with unrelated contexts
  where existing analyzers do not cover the policy.
- Require request context for outbound HTTP operations.

### Lifecycle And Process Control

- Forbid `panic`, `log.Fatal`, and `os.Exit` outside approved entrypoints.
- Forbid package `init()` where explicit construction is required.
- Detect hidden background goroutines in constructors and package globals.
- Flag unbounded goroutine fan-out patterns where statically provable.
- Require documented ownership or cleanup for configured resource constructors.
- Forbid internal locks held across configured callback or I/O calls where
  analysis can establish the flow with acceptable precision.

### HTTP, SQL, Logging, And Secrets

- Forbid default HTTP clients/transports and requests lacking explicit context
  or approved timeout policy.
- Forbid direct SQL and backend-specific clients outside configured packages.
- Detect known secret-bearing values passed to `slog`, telemetry, errors, URLs,
  or formatting APIs using typed annotations/configured constructors.
- Forbid attacker-controlled metric label names and configured high-cardinality
  values where type/data-flow evidence is sufficient.
- Detect ignored cleanup, response body, rows, transaction, and rollback paths
  not already handled precisely by standard analyzers.

### Safety And Public API Policy

- Forbid production imports or directives involving `unsafe`, cgo, and
  `go:linkname`.
- Enforce approved interface placement and naming policies where configured.
- Detect exported interfaces that are provider-owned or excessively broad using
  configurable evidence thresholds, initially advisory if precision is low.
- Enforce stable error wrapping and backend-error boundary policy not covered by
  `errorlint`.
- Detect mutable package globals and unsafe singleton/service-locator patterns.
- Enforce repository-specific deprecation and forbidden-API migrations.

## Configuration

- Versioned strict schema with unknown-key rejection.
- Package/module patterns, entrypoints, layers, contexts, adapter roots,
  generated-code policy, rule severity, and narrow exceptions.
- Deterministic path semantics independent of invocation directory.
- Config inheritance/composition only if conflict precedence is explicit.
- Configuration validation command and human-readable rule inventory.
- Canonical organization policy may live in `mono`; repositories MUST have a
  reproducible sync or drift check rather than silently copied configuration.

## Suppression Policy

- Go-native suppression comments MUST name the exact rule ID and include a
  non-empty reason.
- File/package/global suppression is forbidden by default.
- Suppression parser rejects unknown rules, malformed directives, stale
  suppressions, duplicate directives, and unrelated locations.
- Optional expiry and issue reference metadata.
- Generated code exclusions require recognized generated headers and explicit
  policy; hand-written files cannot impersonate generated code silently.
- A suppression inventory is emitted for CI review and trend tracking.

## Rule Governance And Conflict Prevention

- Maintain a matrix against compiler, vet, Staticcheck, golangci-lint linters,
  gosec, CodeQL, and NilAway.
- Do not add a rule when an existing blocking tool enforces the same semantics
  reliably and can be configured consistently.
- Every overlap states the canonical authority and compatible configuration.
- Contradictory rules are release blockers.
- New rules begin advisory when precision has not been proven across the full
  repository corpus.
- Promotion to blocking requires clean corpus evidence, migration guidance, and
  an explicit versioned policy decision.

## NilAway Integration Policy

- NilAway remains a separately pinned advisory analyzer.
- This project MAY normalize NilAway reports into shared SARIF or dashboards but
  MUST NOT hide its exit status or transform findings into blocking failures.
- Do not reimplement broad nil-flow analysis merely to avoid NilAway's advisory
  status; add precise package-owned nil rules only for deterministic local
  contracts.

## Performance And Scalability

- Reuse type information and facts efficiently across analyzers.
- Avoid repeated whole-program loading when a shared driver can perform it once.
- Bound fact size, configuration, diagnostics, traces, and data-flow exploration.
- Support changed-package execution without making full-module CI optional.
- Benchmark cold/warm runs on small packages, large libraries, and service
  modules; publish wall time and peak memory.
- Analyzer overhead budgets are enforced so strict local execution remains
  practical.

## Security And Supply Chain

- No analyzer executes target code, arbitrary configuration programs, or
  untrusted plugins.
- Paths, source snippets, diagnostics, and SARIF MUST not expose repository
  secrets unnecessarily.
- Tool releases are reproducible, checksummed, signed where available, and
  pinned by consuming repositories.
- Dependencies remain minimal and audited.
- GitHub Actions are commit-SHA pinned with least-privilege permissions.

## Non-Goals

- No formatter, compiler fork, language server, code-review bot, or hosted SaaS.
- No replacement for vet, Staticcheck, gosec, CodeQL, race testing, fuzzing,
  govulncheck, or human architectural review.
- No noisy subjective style catalog.
- No regex-only rules where typed semantic analysis is required.
- No claim of Rust-equivalent ownership, borrow, or data-race proof.

## Package Shape

- `analysis`: shared metadata, configuration, diagnostics, suppression, reports.
- `analyzers/<rule>`: one cohesive package per analyzer or tightly coupled family.
- `policy`: rule sets and conflict/ownership metadata.
- `cmd/analysis`: standalone multichecker and reporting command.
- `goplugin`: optional pinned golangci-lint integration.
- `analysistestkit`: reusable fixtures and policy corpus helpers.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Required evidence:

- positive, negative, near-miss, alias, generics, build-tag, and multi-package
  fixtures for every diagnostic
- suggested-fix golden tests that compile and preserve intended semantics
- malformed config and suppression fuzzing
- analyzer no-panic fuzzing across parsed/type-checked syntax
- full corpus tests against every owned Go repository with zero unexplained
  findings and stable expected advisory output
- mutation testing of diagnostic and suppression decisions
- determinism tests across concurrency, path, platform, and map order
- benchmarks for each analyzer and aggregate cold/warm wall time and peak memory

## Documentation Deliverables

- Five-minute standalone, vettool, and CI quickstarts.
- Complete rule catalog with rationale, bad/good examples, remediation,
  configuration, severity, overlaps, and suppression guidance.
- Guides for architecture policies, custom rules, repository rollout, advisory
  promotion, SARIF, performance, and troubleshooting.
- Contributor guide for analyzer design and false-positive evaluation.
- Compatibility, security, governance, FAQ, examples, and maintained changelog.
- Every command, exported API, rule, and user-facing scenario MUST be documented.

## Automation And Release

Use the latest stable Go release as the minimum at implementation time. Pin all
tools and dependencies. GitHub Actions MUST run formatting, vet, Staticcheck,
strict golangci-lint, advisory NilAway, tests, meaningful 100% coverage, race,
fuzz smoke, mutation checks, corpus matrices, vulnerability scans, benchmarks,
docs, API/rule compatibility, reproducible builds, and releases. Every blocking
command MUST be reproducible locally through documented `make` targets.

## Execution Plan

1. Specify analyzer metadata, config, suppression, report, and governance models.
2. Implement architecture, dependency, unsafe, process-control, and context rules.
3. Implement HTTP, SQL, lifecycle, logging, and public-API rule families.
4. Integrate standalone, vettool, SARIF, and optional golangci-lint execution.
5. Prove precision and performance against the complete owned repository corpus.
6. Publish complete rule documentation and release v1.

## Acceptance Criteria

- Every blocking diagnostic is precise, documented, tested, and low-noise.
- Every rule has an explicit owner and no contradictory tool configuration.
- Local and CI execution use identical pinned binaries and configuration.
- Suppressions are narrow, reasoned, auditable, and drift-checked.
- The complete repository corpus passes with no unexplained blocking finding.
- Meaningful 100% coverage and every required CI gate pass.
