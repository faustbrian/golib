# Goal: Establish the Canonical Go Libraries Monorepo

## Execution Contract

Execute this goal end to end in `/Users/brian/Developer/go-libraries`.
Do not stop after analysis, scaffolding, or a partial migration. Inspect the
current repository before changing it, preserve package behavior, make
focused commits throughout the work, and finish with exact verification
evidence and a written account of any genuine external blocker.

The repository contains the complete owned Go library ecosystem. It MUST
become the canonical source, development workspace, CI boundary, and release
repository for those libraries while preserving independent Go modules and
independent semantic versions.

This is a multi-module monorepo, not one giant Go module and not a framework
module that forces every package on every consumer.

## Confirmed Decisions

- The canonical repository is `github.com/faustbrian/golib`.
- Every library remains an independently consumable Go module.
- Module paths MUST follow their location in this repository, for example:
  - `github.com/faustbrian/golib/pkg/cache`
  - `github.com/faustbrian/golib/pkg/jsonapi`
  - `github.com/faustbrian/golib/pkg/queue`
  - `github.com/faustbrian/golib/pkg/authentication/jwt`
  - `github.com/faustbrian/golib/pkg/outbox/adapters/goqueue`
- Releases MUST use module-directory-prefixed tags, for example:
  - `cache/v0.1.0`
  - `jsonapi/v1.0.0`
  - `authentication/jwt/v1.0.0`
  - `outbox/adapters/goqueue/v1.0.0`
- The former standalone repositories have never had real consumers and do
  not require compatibility releases, redirects, mirrors, or staged consumer
  migration.
- Migrate paths atomically inside this repository. Archive the former
  standalone repositories only after the monorepo is verified and its first
  releases are resolvable through normal Go tooling.
- Do not delete the former repositories or rewrite their history. They remain
  historical provenance even after archival.
- Do not reconstruct or rewrite Git history merely to combine old histories.
  Record source provenance instead.
- The root `AGENTS.md` is the canonical engineering policy for all packages.
  Package-level policy files MAY exist only for documented package-specific
  additions or exceptions and MUST NOT duplicate or contradict root policy.
- The root workspace is for development convenience. Every released module
  MUST also build and test with `GOWORK=off`.
- `openrpc` is now an implemented module whose readiness MUST be evaluated
  from its package goals and verification evidence.
- `json-schema` is currently goal-only and MUST remain classified as
  planned until implementation and conformance evidence exist.
- The focused root follow-up goals in `.ai/GOAL_HARDEN.md`,
  `.ai/GOAL_SECURITY.md`, `.ai/GOAL_PERFORMANCE.md`,
  `.ai/GOAL_BENCHMARKS.md`,
  `.ai/GOAL_SUPPLY_CHAIN.md`, `.ai/GOAL_RELEASE.md`,
  `.ai/GOAL_COMPATIBILITY.md`, and `.ai/GOAL_MAINTENANCE.md` are part of this
  goal's completion contract.

## Current Inventory To Verify

The initial audit found:

- 39 top-level `go-*` package directories.
- 38 top-level Go modules.
- 50 `go.mod` files in total.
- 43 likely releasable modules when optional adapter modules are included.
- 7 likely fixture, compatibility, integration, or example modules that
  should not be independently released.
- Go directives ranging from Go 1.24 through Go 1.26 patch releases.
- Package-local GitHub workflows but no effective root workflow suite.
- Old standalone module paths throughout module declarations, imports,
  documentation, badges, and internal requirements.
- Internal requirements pinned to old pseudo-versions and a few `v1.0.0`
  versions from the unused standalone repositories.
- Nested `go.work` files and stale local-workspace assumptions.
- A tracked root `.DS_Store`.
- Missing licenses in at least `analysis` and `api-query`.
- `tabular` uses Apache-2.0 while most other implemented packages use MIT.
- Inconsistent repository metadata and package-level agent instructions.

Re-audit these facts from the current tree. Do not assume these counts remain
correct after work has started.

## Required Outcomes

### 1. Inventory And Classification

Create a machine-readable module catalog and a readable package catalog.
For every top-level package and nested module, record:

- directory;
- canonical module path;
- purpose and short description;
- lifecycle state: planned, experimental, stable, or deprecated;
- module kind: public library, optional adapter, command, example, fixture,
  compatibility harness, or integration harness;
- whether it is independently releasable;
- current semantic version or intended initial version;
- license;
- direct owned-module dependencies;
- responsible hardening and specification goals;
- relevant CI and release requirements.

The classification MUST explicitly distinguish releasable modules from test
fixtures. Do not publish fixture modules accidentally. Validate that the
owned-module dependency graph is acyclic and document intentional dependency
direction.

Record provenance for each imported package, including its former repository
URL and final known source revision where it can be verified without changing
the former repository.

### 2. Canonical Module Paths

Atomically migrate all releasable module declarations, Go imports, internal
requirements, examples, generated API baselines, documentation, badges, and
tooling references from standalone paths to paths beneath:

`github.com/faustbrian/golib`

Requirements:

- A module directory and its module path MUST agree.
- Nested optional modules MUST retain dependency isolation where that is the
  reason they are separate modules.
- Fixture module paths MAY remain `example.com/...` when they are deliberately
  non-publishable and isolated.
- Remove obsolete pseudo-version coupling to the standalone repositories.
- Do not add permanent local `replace` directives to releasable `go.mod`
  files.
- Temporary migration replacements MUST be removed before completion.
- Run `go mod tidy` in every module where the migration changes its graph.
- Verify every committed `go.mod` and `go.sum` independently.
- Detect old owned import paths in CI so they cannot return.

Because there are no consumers of the standalone module paths, do not build a
compatibility bridge or preserve obsolete paths at the cost of permanent
complexity.

### 3. Workspace Design

Add one canonical root `go.work` for active development.

- Include all production and optional adapter modules that should participate
  in cross-module development.
- Exclude disposable fixture modules unless inclusion is required for a
  documented test purpose.
- Remove redundant package-local `go.work` files or justify any that remain.
- Eliminate absolute developer-machine paths and stale sibling-directory
  replacements.
- Generate or validate `go.work` deterministically so module additions cannot
  drift silently.
- Do not rely on `go.work.sum` as a substitute for correct module sums.

Provide root commands that work from a fresh clone and do not assume sibling
repositories exist.

### 4. Go Version Policy

Verify the current stable Go release from official Go sources before making
the version decision. The initial audit was performed with Go 1.26.5.

Adopt one documented minimum-version policy across releasable modules. The
desired policy is the latest stable Go release as the minimum unless a module
has a specific, documented compatibility reason not to use it.

- Normalize `go` directives consistently.
- Use the `toolchain` directive only when it has a clear documented purpose.
- Separate the minimum language/module version from the latest patch used in
  CI when appropriate.
- Test the declared minimum and latest supported patch where those differ.
- Add drift checks so newly introduced modules cannot silently use an older
  unsupported version.

### 5. Root Governance And AI Cohesion

Create cohesive root documentation and policy:

- `README.md` with purpose, package catalog, quick start, workspace commands,
  release model, and links to package documentation;
- `AGENTS.md` with enforceable ecosystem-wide Go engineering rules;
- `CLAUDE.md` that points to the same canonical rules without maintaining a
  divergent duplicate;
- `CONTRIBUTING.md`;
- `SECURITY.md` with private vulnerability reporting;
- `CODE_OF_CONDUCT.md`;
- governance, support, compatibility, and deprecation policies where useful;
- architecture documentation for package boundaries and dependency direction;
- release and module-lifecycle documentation;
- a migration record explaining the atomic path change;
- a package creation checklist and template.

The root agent rules MUST preserve the previously agreed standards:

- explicit ownership and cleanup for goroutines, timers, tickers, bodies,
  files, transactions, locks, and other resources;
- no unbounded concurrency, queues, reads, retries, cardinality, or memory;
- context propagation and cancellation on blocking operations;
- no stored request contexts;
- no package-level mutable state without explicit synchronization and need;
- no unsafe code unless explicitly reviewed and documented;
- race tests for concurrent behavior;
- fuzzing for parsers, decoders, protocol boundaries, hostile input, state
  machines, and serialization;
- meaningful boundary, fault-injection, lifecycle, interoperability, and
  security tests rather than line-only coverage;
- benchmark discipline with allocation reporting and documented regression
  budgets for hot paths;
- strict changelog maintenance per independently released module;
- complete user-facing API, adoption, examples, cookbook, FAQ,
  troubleshooting, security, compatibility, and migration documentation;
- no hidden framework magic, service locator behavior, implicit global
  registration, or surprising background work;
- small consumer-defined interfaces and additive optional integrations;
- explicit error contracts, wrapping, classification, and redaction;
- deterministic tests and injected clocks/randomness where required;
- stable public API and semantic-version review before release.

Package-specific `AGENTS.md` files MUST be reduced to real exceptions or
replaced by pointers to root policy. Conflicts MUST be resolved in favor of a
single explicit rule, not left ambiguous.

### 6. Standard Repository Shape

Define and enforce a standard package shape while allowing justified
package-specific files. Every implemented public package MUST have:

- a complete README with installation, quick start, public API overview,
  adoption guidance, examples, and links to detailed docs;
- an accurate license;
- `CHANGELOG.md` following one consistent format;
- security and contribution guidance, inherited from root where appropriate;
- `.ai/GOAL.md` and `.ai/GOAL_HARDEN.md` retained as execution history and
  product requirements;
- API examples that compile;
- package documentation suitable for pkg.go.dev;
- accurate CI, coverage, documentation, security, and release badges where
  those signals actually exist.

Do not display misleading badges. GitHub Actions badges are workflow-level,
not proof that every matrix job passed. Document the job matrix and provide
machine-readable per-module results where finer detail is required.

Preserve mixed licensing correctly. A root license MUST NOT silently relicense
Apache-2.0 or third-party-derived code. Add missing package licenses only after
verifying the intended license and copyright attribution. Distinguish
`NOTICE` requirements from third-party license inventories.

Remove committed editor, OS, coverage, mutation, build, and temporary artifacts
unless they are intentional test fixtures. Add a root `.gitignore` that covers
the monorepo without hiding required fixtures.

### 7. Root Tooling

Provide root commands, preferably through a small auditable script set and
`Makefile`, for:

- listing and validating modules;
- formatting all modules;
- tidying and verifying module files;
- testing changed modules and affected dependants;
- testing every releasable module independently with `GOWORK=off`;
- testing integration and fixture modules intentionally;
- running race tests;
- running fuzz smoke tests and package-specific longer fuzz campaigns;
- collecting per-module meaningful coverage;
- running linters, security checks, API compatibility checks, documentation
  checks, mutation checks, and benchmarks;
- detecting dependency cycles and forbidden package direction;
- detecting old standalone module paths and local absolute paths;
- determining changed modules from Git history;
- computing affected reverse dependencies;
- validating changelog requirements;
- validating release tags and release order;
- producing a release plan without publishing anything.

Scripts MUST be portable across Linux and macOS where practical, fail loudly,
quote paths safely, avoid network access unless the operation requires it, and
be covered by tests where non-trivial logic is introduced. Prefer a small Go
command over fragile shell when the logic becomes substantial.

### 8. Static Analysis And Security Tooling

Standardize strict, non-contradictory local and CI tooling. At minimum,
evaluate and configure the current maintained versions of:

- `gofmt` and `go vet`;
- `golangci-lint` with a deliberate strict rule set;
- Staticcheck;
- `govulncheck`;
- `gosec`;
- the owned `analysis` policy suite;
- NilAway as warning-only until its signal quality justifies promotion;
- API compatibility tooling for stable public modules;
- dependency, license, secret, and workflow-security checks.

Requirements:

- Every tool MUST be runnable locally through documented root commands.
- CI and local commands MUST use the same configuration.
- Pin tool versions reproducibly.
- Do not enable overlapping rules that emit contradictory requirements.
- Document intentional suppressions next to their rationale.
- Suppression counts MUST be observable and prevented from silently growing.
- Warning-only tools MUST remain visible in CI summaries.
- Security-critical findings MUST fail CI.
- Do not weaken a package's existing hardening merely to make the unified
  configuration pass.

### 9. Testing And Hardening

Treat each package's `.ai/GOAL.md`, `.ai/GOAL_HARDEN.md`, and supplemental
goals as required product contracts. Build a traceable readiness report for
every package showing whether implementation and evidence satisfy each goal.

Meaningful 100% production-code coverage remains a requirement where already
specified. It MUST NOT be achieved through tests that execute lines without
asserting behavior. Review coverage by behavioral risk, branch, error path,
resource lifecycle, and invariant.

For every releasable module, run as applicable:

- unit tests;
- integration and interoperability tests;
- `go test -race`;
- fuzz seed and bounded fuzz smoke tests;
- package-specific full coverage checks;
- mutation testing or equivalent assertion-quality evidence;
- examples as tests;
- API compatibility checks;
- hostile-input and resource-limit tests;
- leak and cancellation tests;
- deterministic benchmarks with `-benchmem`;
- specification conformance suites where a formal specification exists.

Formal specification packages such as JSON:API and JSON-RPC MUST be audited
against their normative specifications, extensions, recommendations, error
semantics, interoperability fixtures, and documented conformance matrices.
Do not infer compliance from coverage alone.

Do not claim every package is hardened because a root aggregate test passed.
Evidence MUST remain attributable to individual modules and goals.

### 10. CI Architecture

Replace ineffective nested workflow placement with canonical root workflows.
Port all real package workflow behavior before removing old workflow files.

CI MUST provide:

- fast pull-request validation for changed modules and affected dependants;
- a full ecosystem validation mode that runs all modules;
- isolated `GOWORK=off` tests for releasable modules;
- workspace integration tests;
- formatting, module metadata, lint, static analysis, vulnerability,
  dependency, license, secret, and workflow-security checks;
- race, fuzz-smoke, conformance, coverage, and documentation gates;
- scheduled heavier fuzzing, mutation, and benchmark jobs where pull-request
  latency would otherwise be unreasonable;
- concurrency cancellation for superseded runs;
- least-privilege permissions;
- pinned action versions or immutable SHAs according to the documented
  supply-chain policy;
- dependency caching without caching generated correctness evidence;
- clear per-module summaries and retained diagnostic artifacts;
- no dependence on untrusted pull-request secrets;
- a reproducible local equivalent for every required CI gate.

Keep CI cost controlled through change-aware execution, reverse-dependency
analysis, and scheduled full runs. Change-aware CI MUST fail closed: changes
to root policy, shared scripts, module discovery, tool versions, or workspace
configuration MUST trigger the full relevant matrix.

Consolidate dependency automation at the root. Choose one maintained approach
and configure every releasable module without duplicate bots fighting each
other. Validate that root configuration supports nested modules correctly.

### 11. Release Engineering

Create a safe independent-module release process.

- Validate that tags use the complete relative module directory prefix.
- Support semantic versions and `/v2` or later module-path rules correctly.
- Determine release order from the owned-module dependency graph.
- Require clean module files, changelog entries, API compatibility review,
  and full module verification before tagging.
- Update owned dependency requirements to released monorepo versions rather
  than unresolved local revisions.
- Provide a dry-run release plan that shows modules, old versions, proposed
  versions, tags, dependencies, and commands without mutating Git or GitHub.
- Never reuse one unprefixed root tag for multiple modules.
- Do not publish fixture or compatibility-harness modules.
- Verify released modules through a clean temporary consumer with
  `GOWORK=off` and the public Go proxy path where applicable.
- Generate checksums, SBOMs, provenance, and signed attestations for released
  binaries where the repository publishes commands or artifacts.
- Do not invent binary release machinery for source-only libraries where Go
  module tags are sufficient.

Choose initial monorepo versions deliberately. Since the old repositories had
no consumers, compatibility does not constrain the choice, but package
maturity and existing changelog claims still matter. Document whether a
module starts at `v0.1.0`, preserves a justified `v1.0.0`, or remains
unreleased.

### 12. GitHub Repository Cohesion

Configure `faustbrian/golib` as the canonical public project:

- accurate description, website, topics, and default branch;
- issue and pull-request templates suitable for a multi-module repository;
- module/path selectors in issue forms;
- CODEOWNERS or an equivalent ownership model where useful;
- labels for package, change type, security, release, and lifecycle;
- branch protection and required checks aligned with the root workflows;
- private vulnerability reporting and security policy;
- release documentation that explains prefixed tags;
- package discoverability through the root catalog and pkg.go.dev links.

Former standalone repositories MAY be archived after all of these are true:

- their final source revisions are recorded;
- the monorepo contains the intended current source;
- canonical paths contain no old import dependencies;
- first monorepo module tags resolve from clean consumers;
- documentation points to the monorepo;
- no package still relies on a standalone repository workflow or release.

No compatibility bridge is required because the standalone repositories had
no real consumers.

### 13. Consumer Readiness

Do not migrate Track, Postal, Location, or application code as part of this
goal unless explicitly requested. Prepare the libraries for those migrations:

- document canonical imports and version selection;
- provide one clean integration example for a service consuming several
  owned modules with `GOWORK=off`;
- prove that consumers are not required to clone the monorepo or use a
  workspace;
- prove optional adapters do not force unrelated heavy dependencies;
- document how application repositories should use released versions rather
  than local replacements;
- provide an adoption checklist for the first Postal migration.

## Important Design Constraints

- Do not collapse independent packages into one root module.
- Do not create a framework service container or hidden registration system.
- Do not create broad shared abstractions solely to make the monorepo look
  uniform.
- Do not force all storage, transport, telemetry, authentication, queue, or
  cloud dependencies into every consumer.
- Do not introduce dependency cycles between foundational modules.
- Do not treat a root `go.work` pass as release proof.
- Do not silently drop existing package-specific CI, security, conformance,
  mutation, fuzzing, benchmark, or documentation requirements.
- Do not archive former repositories before released canonical paths resolve.
- Do not push, tag, release, archive repositories, or change GitHub protection
  settings without the approvals required by repository policy.
- Preserve an auditable commit history. Do not reset, rebase, force-push, or
  rewrite the current monorepo history for cosmetic reasons.

## Suggested Execution Phases

### Phase 1: Baseline

1. Verify repository, branch, remote, and worktree state.
2. Inventory every package, module, nested module, goal, workflow, license,
   dependency, generated artifact, and release claim.
3. Classify modules and build the dependency graph.
4. Record current verification failures without changing behavior.
5. Commit the inventory, decisions, root policies, and migration plan.

### Phase 2: Canonical Paths And Workspace

1. Migrate module declarations and imports atomically.
2. Normalize owned requirements and remove obsolete local assumptions.
3. Add the canonical root workspace.
4. Tidy and verify every module.
5. Add drift checks for paths, module inventory, versions, and replacements.
6. Commit the functional monorepo migration.

### Phase 3: Tooling And CI

1. Build root orchestration commands.
2. Port package workflow behavior into root workflows.
3. Standardize tool versions and non-conflicting configurations.
4. Add changed-module and reverse-dependency selection.
5. Add isolated and workspace test modes.
6. Commit tooling and CI in focused, reviewable units.

### Phase 4: Package Standardization And Hardening Audit

1. Normalize package metadata and documentation without erasing legitimate
   package differences.
2. Resolve missing or ambiguous licenses.
3. Reconcile root and package agent rules.
4. Execute every package goal and hardening traceability audit.
5. Fix real implementation, testing, documentation, security, performance,
   or conformance gaps discovered by the audit.
6. Commit fixes by coherent package or dependency layer.

### Phase 5: Release Readiness

1. Define initial versions and dependency-ordered release plan.
2. Validate module-prefixed tags in dry-run mode.
3. Test clean external consumers with `GOWORK=off`.
4. Validate GitHub metadata and archival prerequisites.
5. Produce the final readiness report and unresolved external action list.

## Verification Requirements

Before completion, provide fresh evidence for at least:

```text
git status --short --branch
go version
go work edit -json
<root module inventory validation command>
<root old-path and absolute-path drift command>
<root format command>
<root module tidy/check command>
<root isolated GOWORK=off test command>
<root workspace integration test command>
<root race command>
<root fuzz-smoke command>
<root coverage command>
<root lint and static-analysis command>
<root vulnerability and security command>
<root documentation command>
<root API compatibility command>
<root benchmark command>
<root release dry-run command>
```

Use the actual commands created by the repository, not placeholders, in the
completion report. Run the narrowest useful checks during development and the
full local release-equivalent suite before claiming readiness.

Hosted CI is the final external confirmation, not a substitute for local
verification. If Docker, external services, credentials, GitHub permissions,
or publication are unavailable, complete every unaffected local task and
report the exact remaining external action.

## Completion Criteria

This goal is complete only when:

- `golib` is the only active source repository for these packages;
- all releasable module paths match their monorepo directories;
- no releasable code or module metadata references old standalone paths;
- the root workspace works from a fresh clone;
- every releasable module independently builds and tests with `GOWORK=off`;
- root CI enforces workspace and isolated correctness;
- module discovery, dependency direction, versions, tags, changelogs, and
  package metadata cannot drift silently;
- package goals and hardening goals have traceable evidence and no unreported
  implementation gaps;
- formal specification claims have explicit conformance evidence;
- meaningful coverage, race, fuzz, mutation, security, documentation,
  compatibility, and performance requirements are enforced as specified;
- release tooling produces valid dependency-ordered module-prefixed tags;
- a clean consumer can resolve released canonical modules without local
  replacements or the root workspace;
- former repositories are either intentionally retained or ready for archival
  under documented criteria;
- the working tree is clean and all local verification results are reported
  honestly.

Do not declare completion based on file presence, green aggregate tests,
coverage percentages alone, or unverified workflow configuration.
