# Changelog

All notable repository-level changes are documented here. Module behavior is
documented in each module's changelog.

## Unreleased

### Changed

- Isolate root digest fixtures from the repository Go wrapper so the
  CI-equivalent clean module environment exercises fixture-local modules.
- Pass isolated module flags to supported outer Go commands without leaking a
  temporary modfile into nested test processes or unsupported documentation
  and versioned-tool commands.
- Install versioned analysis tools outside the target module, then execute
  them against its isolated module graph so nested modules are linted instead
  of being misreported as empty.
- Use deterministic execution counts for canonical fuzz smoke gates while
  preserving explicit duration overrides for extended fuzz campaigns.
- Recognize valid fuzz targets regardless of parameter naming, honor explicit
  `fuzz-smoke` targets, and fail when a declared fuzz gate executes nothing.
- Reuse reset-safe mutation checkpoints by exact package inputs and report
  digests so unrelated repository changes cannot trigger redundant campaigns.
- Resolve mutation fingerprint dependencies through the canonical workspace so
  isolated callers and local runs produce the same content identity, with
  exact legacy-fingerprint migration for existing checkpoints.
- Match the webhook public-vector secret allowlist at both repository and
  module scan roots without broadening it beyond the exact fixture.
- Consolidated the standalone libraries into independently versioned modules
  under `pkg/` with canonical `github.com/faustbrian/golib/pkg/...` paths.
- Replaced fragmented package workflows with one changed-module and
  reverse-dependant CI matrix backed by the root command surface.
- Isolate local-proxy module checks behind temporary module and checksum files,
  so owned source changes cannot invalidate committed release checksums while
  external dependency drift remains fail-closed.
- Standardized repository governance, module inventory, dependency graph,
  exact coverage, mutation, security, service, and release policies.
- Keep content-addressed mutation proof valid when only owned module archive
  checksums change, while retaining executable code and observer inputs.
- Added a portable production-source safety scan and fuzz discovery that do
  not depend on ripgrep, and made the safety and advisory NilAway checks part
  of the canonical module contract.
- Replaced regex-based unsafe detection with a syntax-aware scanner that
  distinguishes imports and directives from ordinary string literals.
- Execute a module's explicit API compatibility script when no equivalent
  Make target exists, while still failing declared gates with no command.
- Run module fallbacks only when no matching Make target exists, so failures
  in package-owned documentation, fuzz, API, or interoperability gates cannot
  be masked by a successful fallback.
- Added repository-level workflow linting and build-constraint-aware gate
  selection so explicit harness modules are not assigned inapplicable checks.
- Classified every discovered package by module ownership, lifecycle,
  executable production behavior, and exact-coverage applicability, with
  fail-closed filesystem and build-constraint discovery.
- Declared required integration test tags and pinned backend identity
  variables in the module catalog so local and CI test, race, and coverage
  gates exercise the same backend-dependent behavior.
- Replaced package-local mutation dispatch with one manifest-driven runner
  that mutates every executable production package and accepts only nonempty
  reports containing real killed outcomes at exact 100% thresholds.
- Defined content-addressed gate evidence so history rewrites and unrelated
  commits cannot trigger expensive reruns when every behavior-affecting input
  is unchanged, and required atomic per-unit checkpoints as soon as results
  are received.
- Separate mutation campaign inputs from evidence-orchestration bookkeeping,
  and migrate the exact report-hash-pinned checkpoints retained across the
  repository's history-only reset without rerunning proven mutant campaigns.
- Re-anchor the exact mutation checkpoint migration ledger after the approved
  follow-up squash removed its previous replacement commit object.
- Replace the reset ledger's commit-object dependency with a deterministic
  repository-content fingerprint so repeated history-only rewrites cannot
  invalidate exact retained evidence.
- Narrowed mutation fingerprints to the compiled source, tests, embedded
  data, conventional fixture corpora, module manifests, owned dependencies,
  and exact tooling used by integration-mode mutation runs. Documentation
  edits now preserve valid evidence, while executable module changes correctly
  invalidate every checkpoint whose mutant command runs `go test ./...`.
  Content-identical legacy checkpoints migrate through current or historical
  input comparison without executing mutants again.
- Reuse one immutable full-module coverage profile across package-attributable
  mutation runs, avoiding repeated integration-suite coverage collection while
  preserving the same `go test ./...` observer set for every mutant.
- Correct interoperability-tool discovery for modules beneath `pkg/` and keep
  the XSD catalog aligned with its XML Schema 1.0 Second Edition scope.
- Fail when a module declares interoperability tools without an executable
  gate instead of reporting the missing proof as not applicable.
- Route benchmark checks through package-owned harnesses, persist attributable
  output atomically, and fail when no Go benchmark actually ran.
- Run generic benchmark fallbacks against the checked-out workspace so owned
  dependencies are measured before their initial public tags exist.
- Override package-local `GOWORK=off` defaults for benchmark dispatch so every
  owned dependency resolves from the same canonical checkout.
- Prevented root module documentation checks from recursively invoking the
  repository-wide documentation orchestrator.
- Replaced revision-only mutation artifacts with content-addressed,
  per-package atomic checkpoints that survive interruptions and unrelated
  repository history changes.
- Added deterministic local module-proxy verification so isolated pre-release
  gates resolve the current owned dependency graph without workspace or
  module replacements, while public tag resolution remains a separate gate.
- Added dependency-ordered module execution and an isolated tidy command so
  owned dependency checksums are generated from the final source snapshot.
- Made every non-tidy isolated gate read-only for module metadata so checks
  cannot silently repair missing requirements or checksums while they run.
- Bound isolated module-cache storage to one temporary gate lifecycle while
  reusing the host download cache for immutable external dependencies.
- Refresh only owned release-candidate checksums during explicit tidy runs so
  source-snapshot changes cannot weaken external dependency authentication.
- Validate the repository license once while excluding only the canonical
  owned-module namespace from `go-licenses`; external dependency licenses
  remain fail-closed.
- Added canonical module export baselines and an `api-update` command so every
  declared API compatibility gate has an executable fail-closed implementation.
- Run WSDL interoperability through the same digest-pinned Java container
  locally and in CI instead of masking host-runtime dependencies in CI setup.
- Scope repository secret scanning away from generated artifacts and narrowly
  documented public fixtures while retaining the complete default rule set.
- Require every releasable module to carry a nonempty module-local licence so
  normal Go module archives and generated SBOMs retain distribution terms.
- Add a first-class fail-closed conformance gate for every public module that
  declares a specification or official corpus, separate from interoperability,
  with atomic attributable output for both gates.
- Persist every module gate result immediately as an atomic log and
  machine-readable checkpoint, and reuse successful evidence only after its
  complete-input fingerprint and log checksum validate. Revalidation preserves
  the original execution revision instead of rerunning proof after
  history-only changes, while interrupted gates discard only their incomplete
  temporary unit and exit immediately.
- Inventory every historical goal with a requirement hash, implementation
  evidence, and the canonical verification contract. Emit a fail-closed
  module traceability report only after every gate checkpoint remains current
  and its attributable log checksum validates.
- Limit mutation fingerprints for owned dependency modules to production
  inputs, because their test suites are not observers of another module's
  mutants. Tests and fixtures from every package in the module under mutation
  remain part of its complete input identity.
- Make the canonical module runner compatible with the Bash version shipped
  by macOS instead of requiring Bash 4 array-loading builtins.
- Apply the canonical root secret-scanning policy to every module gate rather
  than silently falling back to Gitleaks defaults from nested directories.
- Keep fuzz discovery within the selected `go.mod` boundary so independently
  checked child modules are not misreported as parent packages.
- Export the canonical pinned tool manifest to package-owned gates and require
  every API compatibility script to use the same current `apidiff` revision.
