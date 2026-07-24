# Goal: Normalize And Strictly Harden The `golib` Monorepo

## Execution Contract

Execute this goal end to end in `/Users/brian/Developer/go-libraries`.
The canonical remote repository is:

`https://github.com/faustbrian/golib`

Do not stop after inventory, renaming, workflow scaffolding, or fixing the
first failing packages. Complete the repository migration, make every module
pass the same strict quality contract, prove that CI enforces that contract,
and leave the repository usable from a fresh clone without sibling
repositories, machine-specific paths, hidden setup, or bypass flags.

This goal is corrective. It supersedes conflicting repository names, module
paths, directory naming, CI topology, coverage thresholds, mutation
thresholds, and completion claims in older goal files. Older goals remain as
historical requirements and MUST NOT be rewritten merely to hide drift.

Do not claim that a package is complete because files exist, tests pass, or a
previous hardening prompt was executed. Completion requires fresh executable
evidence against every requirement below.

## Non-Negotiable Decisions

- The repository name and canonical import root are
  `github.com/faustbrian/golib`.
- This is a multi-module monorepo. Independently consumable modules retain
  independent `go.mod` files and independent semantic versions.
- The former `go-` directory prefix existed for standalone repositories and
  MUST be removed now that the libraries live beneath one descriptive
  repository root.
- Public library directories MUST live beneath `pkg/` and use concern names
  such as `pkg/authentication`, `pkg/jsonrpc`, `pkg/jsonapi`, `pkg/queue`,
  `pkg/postgres`, and `pkg/service`.
- Repository commands MUST live beneath `cmd/`; non-public shared tooling
  MUST live beneath `internal/`; repository scripts MUST live beneath
  `scripts/`. Public libraries MUST NOT be mixed into the repository root.
- Repository-owned executable names MUST NOT restore the former standalone
  `go-*` prefix. Executables that need an ownership-qualified tooling or
  interoperability identity MUST use an explicit `golib-*` name, such as
  `golib-analysis`; ordinary domain-specific application commands MAY retain
  an unprefixed name when it is already unambiguous.
- Canonical module paths MUST follow the final directory structure, for
  example `github.com/faustbrian/golib/pkg/authentication` and
  `github.com/faustbrian/golib/pkg/authentication/jwt`.
- Fixture-only modules MAY use non-public fixture paths when this prevents
  accidental publication and is documented in the module catalog.
- Every production module MUST enforce meaningful 100% Go statement coverage.
- Every mutation-tested production module MUST enforce a 100% mutation score
  for all viable mutants. A threshold below 100% is forbidden.
- Quality gates MUST fail closed. Missing tools, unavailable dependencies,
  skipped packages, malformed output, timeouts, unclassified mutants, stale
  evidence, and unexecuted commands MUST NOT be reported as success.
- Local commands and CI MUST enforce the same rules. CI MUST NOT use weaker
  thresholds or permissive flags.
- CI MUST be orchestrated by one authoritative root workflow. Package-local
  workflows that GitHub cannot execute MUST be removed after their required
  behavior has been consolidated.
- A green aggregate job is insufficient. Results MUST remain attributable to
  every affected module.
- There MUST be no permanent `replace` directives, old owned pseudo-versions,
  sibling repository assumptions, or absolute developer paths in releasable
  modules.
- No package may be declared hardened while a required gate is failing,
  blocked, skipped, warning-only, or unverified.

## Known Baseline To Re-Audit

The audit preceding this goal found 59 top-level libraries and 81 non-fixture
Go modules. Recalculate the inventory before changing files; counts may have
changed.

It also found:

- 40 top-level libraries passing their currently configured local gates;
- 18 top-level libraries failing at least one configured gate;
- `wsdl` locally unverified because its Woden interoperability check lacked a
  Java runtime;
- only 10 of 81 modules using paths beneath the intended monorepo namespace;
- 71 modules retaining standalone repository paths;
- no authoritative root `go.work`, root command surface, package manifests,
  repository README, or root agent policy;
- 154 nested workflow files that GitHub does not execute as repository
  workflows;
- only 18 root workflow files and no complete, coherent package CI topology;
- variable mutation thresholds ranging below 100%;
- production coverage thresholds that vary by package;
- gates that pass through exclusions, environment shortcuts, stale evidence,
  broad warning modes, or scans of the wrong repository scope.

Treat this as discovery evidence, not immutable truth. Produce a fresh report
that identifies every module and the exact current result of every gate.

## Phase 1: Establish A Complete Inventory

Create authoritative, machine-readable repository manifests for every
top-level library and every nested module. Record at minimum:

- final directory;
- canonical module path;
- Go package names;
- purpose and lifecycle status;
- module kind: public library, adapter, command, fixture, example,
  interoperability harness, benchmark harness, or internal tool;
- whether independently releasable;
- semantic version and tag prefix;
- direct owned-module dependencies;
- reverse owned-module dependencies;
- external runtime dependencies;
- required services and interoperability tools;
- applicable specification, conformance corpus, and provenance;
- coverage, mutation, fuzz, race, benchmark, security, documentation, and
  release gates;
- current goal files and their implementation status.

The inventory MUST detect undeclared modules, duplicate module paths,
directory/module mismatches, dependency cycles, fixtures accidentally marked
for release, and modules omitted from CI.

Build an explicit owned-module dependency graph. Changed-package CI selection
MUST include reverse dependants whose compatibility can be affected by a
change.

## Phase 2: Rename Libraries And Canonicalize Modules

Rename every top-level `go-*` directory by removing only the repository-era
`go-` prefix. Preserve the remainder of the established descriptive name.
Review collisions and ambiguous names before moving files; do not silently
merge unrelated modules.

Update atomically:

- all `module` directives;
- owned imports in Go source and tests;
- nested module paths;
- owned requirements;
- documentation and examples;
- generated API baselines and golden files where paths are contractual;
- badges and pkg.go.dev links;
- workflow and tool configuration;
- scripts, manifests, release metadata, provenance records, and changelogs;
- specification and interoperability harnesses;
- Docker and development configuration where module paths are embedded.

Required public-library path form:

`github.com/faustbrian/golib/pkg/<library>[/<nested-module>]`

Remove all obsolete references to `github.com/faustbrian/go-*` after the
migration, except in an explicit historical provenance document. Historical
references MUST be excluded from forbidden-import checks only by exact file
and purpose, never by broad repository-wide suppression.

Run `go mod tidy` for every module and commit all required `go.sum` data.
Every module MUST pass independently with `GOWORK=off`; workspace success is
not a substitute for a valid published module graph.

## Phase 3: Create The Root Workspace And Command Surface

Create one canonical root `go.work` containing all active production and
adapter modules. Fixture, corpus, example, benchmark, and interoperability
modules MUST be included or excluded according to explicit catalog policy.

Create a root command surface that provides deterministic commands for:

- inventory and manifest validation;
- formatting and formatting checks;
- module tidy verification;
- isolated tests;
- workspace integration tests;
- race tests;
- exact coverage checks;
- mutation tests;
- fuzz smoke tests and extended fuzzing;
- lint and static analysis;
- vulnerability, secret, license, and supply-chain checks;
- documentation, examples, API compatibility, specification conformance,
  and interoperability checks;
- benchmarks and regression comparison;
- complete CI-equivalent validation;
- release dry-runs and clean-consumer resolution.

Commands MUST support one module, an explicit module set, all changed modules
with reverse dependants, and the entire repository. Selection MUST be
deterministic and visible in logs.

Running the complete local CI command from a fresh clone MUST exercise the
same mandatory checks as CI. It MUST fail when any expected module or result
is absent.

## Phase 4: Enforce Meaningful 100% Coverage

Every production package in every releasable module MUST reach exactly 100%
Go statement coverage. Do not average results across packages or modules.
One package at 95% MUST fail even if aggregate module coverage rounds to
100%.

Coverage policy MUST:

- identify production packages from the repository manifest rather than from
  whichever packages happened to execute;
- fail if a production package is missing from the profile;
- merge integration and unit profiles correctly without double counting;
- reject rounded values below exact 100%;
- exclude generated code only when generation provenance is clear and the
  exclusion is narrowly cataloged and reviewed;
- distinguish non-executable declarations from executable production logic;
- forbid tests whose only purpose is executing lines without verifying
  meaningful behavior;
- require boundary, error, lifecycle, hostile-input, concurrency, and cleanup
  assertions appropriate to the code;
- prevent build tags, package filters, environment variables, cached profiles,
  or command flags from silently omitting production paths;
- publish per-package coverage evidence in CI.

Go's native coverage is statement-based, not complete branch/path coverage.
Compensate with mutation testing, table-driven boundary tests, property tests,
fuzzing, state-transition tests, and explicit error-path review. Do not market
statement coverage as proof that all behavior is tested.

## Phase 5: Enforce 100% Mutation Effectiveness

Standardize mutation testing across every module containing mutable
production behavior. Configure one supported mutation toolchain and one
canonical result format unless a documented technical limitation requires a
package-specific adapter.

Mutation policy MUST:

- require 100% of viable generated mutants to be killed;
- set the configured threshold to exactly 100%, never 70%, 80%, 90%, 95%, or
  an inherited tool default;
- fail on any surviving viable mutant;
- fail on mutation tool crashes, malformed reports, unexplained timeouts,
  missing packages, unclassified outcomes, or an unexpectedly empty mutant
  set;
- fail when mutation execution is skipped due to changed-file heuristics,
  environment flags, package filters, or missing tools;
- run against all affected modules and relevant reverse dependants;
- store a machine-readable per-mutant report as CI evidence;
- use deterministic timeouts and isolate genuinely expensive integration
  mutants without lowering standards;
- prohibit blanket exclusions by directory, file type, function name, or
  annotation merely to improve the score.

Equivalent, duplicate, uncompilable, or otherwise invalid mutants MAY be
removed from the viable denominator only through a narrow reviewed record
containing:

- exact module, file, line or stable mutation identifier;
- mutation transformation;
- classification and technical rationale;
- evidence that the mutant cannot represent an observable behavior change;
- reviewer and review date;
- expiry or revalidation condition.

Exclusions MUST be exceptional, machine-validated, visible in CI, and fail on
staleness. A broad ignore flag, allow-survivors switch, threshold override,
`continue-on-error`, shell `|| true`, warning-only mode, or report-only job is
not an exclusion mechanism and is forbidden.

If the selected mutation tool cannot enforce this contract reliably, improve
or replace the tooling. Do not weaken the contract to fit the tool.

## Phase 6: Remediate Every Known Red Or Unverified Library

At minimum, investigate and resolve the previously observed failures without
reducing any gate:

- `authentication`, `idempotency`, and `localized`: repair isolated module
  graphs and all missing owned dependency sums;
- `money` and `rule-engine`: make `go mod tidy -diff` clean and commit complete
  dependency checksums;
- `rate-limit` and `settings`: raise meaningful production coverage to exact
  100%;
- `geo` and `postgres`: upgrade or otherwise remove the reported vulnerable
  `golang.org/x/text` dependency path and rerun vulnerability analysis;
- `queue` and `queue-control-plane`: resolve every lint, security, context,
  credential, integer-conversion, error-handling, and stale-path finding;
- `knapsack`: regenerate benchmark and evidence manifests against the final
  canonical source revision and make stale evidence fail deterministically;
- `service`: restore or replace missing consolidation scripts through the
  canonical root command surface;
- `calendar`: remove dependence on unsupported host Ruby behavior and make
  documentation checks reproducible from the declared toolchain;
- `ecma-regexp`: vendor, fetch reproducibly, or otherwise provision Test262
  through a checksum-pinned process instead of relying on a pre-existing
  `/tmp` directory;
- `feature-flags`: make PostgreSQL and Valkey integration gates reproducible
  locally and in CI without allowing their absence to pass;
- `password`: scope secret scanning correctly to repository inputs without
  accidentally scanning unrelated historical or workspace content, while
  preserving strict secret detection;
- `webhook`: repair the invalid root workflow permission and ensure package
  tools validate the canonical workflow rather than an unrelated scope;
- `wsdl`: provision the pinned Java/Woden interoperability environment in CI
  and documented local tooling, then obtain a real result rather than an
  unverified status.

Re-audit all other libraries. The above list is not permission to ignore new
or previously hidden failures.

## Phase 7: Replace Fragmented Workflows With One Root CI Workflow

Create one authoritative root CI workflow, expected at
`.github/workflows/ci.yml`. Remove package-local workflow files after every
required check has been represented in the root orchestrator.

The workflow MUST trigger only once per relevant event and MUST not create
multiple overlapping workflow runs for the same package change.

It MUST:

- trigger for pull requests and pushes affecting Go modules, shared tooling,
  manifests, goals, documentation, or CI configuration;
- calculate changed modules from the merge base;
- expand the set to affected reverse dependants;
- run a visible dynamic matrix with one attributable result per module;
- run the same standardized strict gate contract for every selected module;
- run every module when root tooling, shared policies, dependency manifests,
  workspace configuration, or the workflow itself changes;
- run the complete repository on `main`, on a schedule, and before release;
- expose one stable required summary check that fails if any expected matrix
  job is absent, cancelled, skipped, warning-only, or unsuccessful;
- use pinned action revisions and pinned tool versions with automated update
  policy;
- use least-privilege permissions;
- use explicit concurrency cancellation only where cancellation cannot hide a
  required result;
- upload per-module coverage, mutation, benchmark, conformance, and failure
  evidence with retention appropriate to audit needs;
- avoid unsafe fork secret exposure;
- make service containers or test dependencies version-pinned and
  health-checked;
- prevent cache keys from reusing stale coverage, mutation, generated, or
  conformance evidence;
- remain runnable through an equivalent local command.

Do not maintain copied package job definitions. Shared behavior belongs in
root scripts or local composite actions invoked by the single workflow.

Release automation MAY remain separate only where GitHub event and permission
boundaries make that necessary. It MUST NOT duplicate or weaken CI; release
must consume or rerun the same complete strict gate. Any additional workflow
requires explicit documentation of why it cannot safely be part of the
single CI orchestrator.

## Phase 8: Standardize The Remaining Hardening Contract

In addition to exact coverage and mutation results, every applicable module
MUST pass:

- `gofmt` and deterministic generated-file checks;
- `go vet`, strict `staticcheck`, and the canonical strict linter set;
- isolated `GOWORK=off go test ./...`;
- `go test -race` for all packages and especially concurrent behavior;
- goroutine, timer, ticker, response-body, file, transaction, lock, and
  connection leak checks;
- fuzz smoke tests for every registered fuzz target and extended deterministic
  fuzz campaigns for parsers and hostile boundaries;
- vulnerability scanning with no known reachable vulnerability accepted by
  default;
- secret, license, provenance, dependency, and supply-chain validation;
- API compatibility baselines for stable modules;
- specification conformance and official fixtures where a compliance claim is
  made;
- interoperability with pinned independent implementations where applicable;
- documentation builds, executable examples, links, and user-facing API
  completeness;
- representative, adversarial, allocation-aware benchmarks with honest
  competitor equivalence and regression budgets;
- clean consumer resolution from outside the workspace;
- release dry-runs using module-directory-prefixed tags.

NilAway and other intentionally advisory analyzers MAY remain warning-only
only when repository policy explicitly designates them as advisory. Advisory
tools MUST still run, publish findings, and have a no-regression policy. They
MUST NOT replace mandatory compile, coverage, mutation, lint, race, security,
or test gates.

## Phase 9: Repository Governance And Documentation

Create or update root documentation for the final `golib` identity and layout:

- `README.md` with package catalog, selection guidance, examples, workspace
  setup, command reference, quality guarantees, and release model;
- root `AGENTS.md` as the canonical engineering policy;
- `CLAUDE.md` as a non-duplicating pointer to canonical policy;
- `CONTRIBUTING.md`, `SECURITY.md`, code of conduct, support policy,
  compatibility policy, and deprecation policy;
- module architecture and dependency-direction documentation;
- exact coverage and mutation policies, including the reviewed invalid-mutant
  process;
- CI package selection and reverse-dependency behavior;
- package creation, rename, split, merge, and retirement procedures;
- release and tag conventions for independent modules;
- migration notes from former standalone repositories and from the temporary
  `go-*` monorepo directory names;
- troubleshooting for local services and pinned interoperability tooling.

Package READMEs, badges, changelogs, pkg.go.dev links, examples, and API docs
MUST reflect final names and actual enforced workflows. Do not publish badges
for jobs or metrics that do not exist.

## Phase 10: Release And Consumer Proof

For every releasable module:

1. Build and test it independently with `GOWORK=off`.
2. Resolve it from a clean external consumer without local replacements.
3. Run examples against the canonical path.
4. Validate the proposed directory-prefixed semantic-version tag.
5. Validate owned dependency release order.
6. Confirm fixtures and harness modules cannot be released accidentally.
7. Confirm changelog and API compatibility state match the proposed release.

Do not archive former standalone repositories until canonical module paths and
initial releases are resolvable through normal Go tooling. Since those
repositories had no real consumers, do not create permanent compatibility
modules or preserve misleading old paths.

## Required Deliverables

Produce and commit:

1. The complete module and package manifests.
2. The final `pkg/` library directory and canonical module layout.
3. The root workspace and standardized local command surface.
4. The single authoritative root CI workflow and removal of inert nested
   workflows.
5. Exact per-package coverage enforcement and reports.
6. Exact per-package mutation enforcement and per-mutant reports.
7. Fixes and regressions for every failing or unverified package.
8. Updated root and package documentation, badges, policies, and changelogs.
9. A goal traceability matrix covering all root and package goal files.
10. A final hardening report containing commands, tool versions, package-level
    outcomes, exclusions, external interoperability evidence, and release
    readiness.

## Required Verification

Before completion, run from a fresh clone or equivalently clean environment:

- repository inventory and manifest validation;
- forbidden old-path and `go-` directory-name checks;
- root workspace validation;
- `go mod tidy -diff` for every module;
- isolated `GOWORK=off` tests for every module;
- workspace integration tests;
- race tests;
- exact 100% per-production-package coverage checks;
- exact 100% viable-mutant checks;
- fuzz smoke and required extended fuzz campaigns;
- all static analysis and strict lint gates;
- vulnerability, secret, license, dependency, and supply-chain checks;
- documentation, examples, API compatibility, specification conformance, and
  interoperability checks;
- benchmark correctness and regression checks;
- the same root command invoked by CI;
- workflow validation and a real GitHub Actions run of the final matrix;
- release dry-runs and clean external consumer resolution.

After the last source, test, documentation, workflow, dependency, or generated
artifact change, rerun every affected gate. Earlier green evidence is stale.

## Completion Criteria

This goal is complete only when:

- no public library is mixed into the repository root;
- every releasable module uses `github.com/faustbrian/golib/pkg/...`;
- no obsolete owned import, pseudo-version, local replacement, absolute path,
  or sibling-repository dependency remains;
- every production package reports exact 100% meaningful statement coverage;
- every viable mutant is killed and the mutation score is exact 100%;
- every required gate passes without reduced thresholds, bypass flags,
  warning substitutions, missing tools, hidden skips, or unexplained
  exclusions;
- the previously failing 18 libraries are green;
- `wsdl` interoperability is verified rather than blocked;
- one root CI workflow selects affected packages and reverse dependants and
  enforces the same complete contract;
- every package is represented in CI and the stable summary job fails closed;
- the complete scheduled/main/release matrix passes;
- clean consumers resolve every releasable module;
- all root and package goals are traceable to implementation and evidence;
- repository documentation accurately describes the final system;
- the final worktree is clean and the final report contains exact, fresh,
  package-attributable verification evidence.

Anything less is partial progress and MUST be reported as such. Do not call
the repository hardened, complete, release-ready, or fully migrated while any
criterion above remains red, skipped, blocked, warning-only, stale, or
unverified.
