# Changelog

All notable changes to this project are documented here. The project follows
semantic versioning after v1; pre-v1 compatibility decisions are described in
[the compatibility policy](docs/compatibility.md).

## Unreleased

### Distribution

- Include the canonical MIT licence in the independently published module.

### Changed

- Use a deterministic execution budget for default fuzz smoke campaigns while
  allowing explicit duration overrides for extended fuzzing.
- Made command exit-code and vettool fallback orchestration independently
  testable while preserving the standalone CLI behavior.
- Treat stderr write failures as terminal command errors without falling
  through to analyzer execution or invoking the exit callback twice.

### Added

- Deterministic analyzer platform with strict versioned configuration,
  suppressions, reviewed policy exceptions, JSON, SARIF, standalone, and
  vettool execution.
- Governed architecture, context, HTTP, lifecycle, observability, API, safety,
  and security rule families.
- Exact production statement coverage, mutation, fuzz, race, benchmark,
  documentation, compatibility, vulnerability, and reproducible-build gates.
- Deterministic six-platform release archives, checksums, embedded versions,
  local verification, and a tag-only least-privilege publication workflow.
- Rule governance, rollout, command/API reference, contributor, security,
  compatibility, custom-rule, and FAQ documentation.
- Typed detection of `context.WithoutCancel` below approved composition roots,
  closing the deliberate-detachment gap left by `go vet` `lostcancel`.
- Mutation enforcement for shared configuration, driver, governance, and every
  shipped analyzer package, including boundary fixtures that distinguish
  package-selection, process-control, and variadic-sink decisions.
- Exact generated-file exclusion paths, preventing untrusted generated headers
  and hidden suppression directives from bypassing analysis outside reviewed
  generator outputs.
- Configured exported interface role naming with typed alias and constraint
  handling, exact compatibility names, and non-overlapping package trees.
- Deterministic validation of configured layer and bounded-context dependency
  cycles that are invisible to Go's package import-cycle check.
- Detection of goroutines proven to start from immediately executed
  package-global initializers while accepting stored and indirect callbacks.
- Test-boundary recognition for process-control policy, preventing panic,
  `log.Fatal`, and `os.Exit` fixtures from becoming production diagnostics.
- Maintained security threat model covering target execution, configuration,
  paths, reports, suppressions, resource exhaustion, and supply chain.
- Independent allocation and race gates so race instrumentation cannot create
  platform-sensitive allocation-budget failures.
- Explicit allocation ceilings for every shipped analyzer plus a tightened
  five-second and 256-MiB checked-in corpus budget.
- A complete owned-repository corpus gate that rejects mixed-revision evidence
  and preserves private deterministic reports with exact revision fingerprints.
- Candidate-specific cold, warm, and peak-memory budgets for both complete
  owned-corpus passes, retained with the private release evidence.
- JSON and SARIF writer benchmarks over 1,000-diagnostic reports plus an
  executable zero-fact-surface contract for every shipped analyzer.
- Immutable committed-HEAD corpus snapshots for release evidence while sibling
  worktrees continue changing.
- Stable JSON and SARIF empty-array encoding for diagnostic, exception,
  suppression, rule, and result inventories.
- Precise acceptance of `http.DefaultTransport` only when it is immediately
  asserted to `*http.Transport` and cloned into caller-owned state.
- Fuzz coverage for JSON and SARIF encoding, path rejection, injection safety,
  empty inventories, and source-bearing field exclusion.
- Required semantic-versioned evidence for every consuming-policy promotion
  from advisory to blocking, preventing silent CI escalation.
- A release evidence index covering every rule's precision, ownership,
  promotion, corpus, suppression, performance, and security posture.
- A source-reviewed false-positive sample report for every rule emitted by the
  complete owned-corpus discovery baseline.
- A complete per-analyzer semantic near-miss manifest enforcing alias,
  embedding, generic, interface, and closure fixtures for every shipped rule.
- Registry rejection of duplicated external authority records that could encode
  contradictory ownership or compatible-configuration prescriptions.
- Suppression parsing now rejects duplicated metadata keys instead of retaining
  an ambiguous last value in the audit inventory.
- A canonical `.go-version` contract now fails local or workflow toolchain
  drift instead of relying on independently maintained version strings.
- Concurrency-safe diagnostic emission budgets that stop checker accumulation
  at 100,000 findings before the reporting layer revalidates the same bound.
- Allocation budgets isolated from coverage instrumentation while remaining
  enforced by the ordinary test phase before exact coverage runs.
- Explicit lock must-analysis ceilings of 4,096 CFG blocks and 256 lock
  identities, with deterministic analyzer failure beyond either boundary.
- Offline `sync-policy` check and update commands plus reproducible Make targets
  for canonical organization-policy synchronization and drift enforcement.
- Advisory typed detection of attacker-controlled values reaching configured
  metric label-name positions, distinct from high-cardinality label values.
- Aggregate allocation ceiling adjusted from 160 to 180 for independently
  governed analyzers; the current aggregate uses 177 allocations.
- Configurable typed transaction rollback ownership that requires an immediate
  deferred rollback after the exact terminating constructor error guard.
- A least-privilege, commit-SHA-pinned CodeQL job now keeps the independent
  security query suite enabled for pull requests and the main branch, using a
  reviewed manual Go build supported by CodeQL without executing target code.
- Canonical corpus fixtures are now protected from user-level global ignore
  rules and verified as tracked before the corpus runner tests execute.
- A blocking Windows analyzer leg now complements Linux and macOS CI, with
  workflow-policy enforcement preventing silent loss of platform coverage.
- The strict golangci-lint suite now enables mature module, interface, metric,
  and SQL ownership authorities instead of duplicating their semantics.
- Corpus and performance runners can resolve policies from a canonical policy
  checkout while analyzing a separate absolute repository root.
- Owned corpus runs can opt into bounded, isolated local module replacements
  without editing target dependency manifests.
