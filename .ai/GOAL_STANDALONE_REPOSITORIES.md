# GOAL: Split golib Into Standalone v1 Repositories

## Objective

Split every public package family from `github.com/faustbrian/golib/pkg/*`
into its own fully standalone public repository under
`github.com/faustbrian`.

Each resulting repository MUST be independently understandable, buildable,
testable, hardened, documented, released, and maintainable without a sibling
checkout or tooling from the former monorepo.

Existing standalone repository code, branches, tags, releases, and history are
legacy and MUST NOT be merged or retained. The verified `golib` monorepo is the
sole implementation source. Existing public module versions are immutable
external facts and MUST be audited rather than overwritten.

## Fixed Decisions

- Use one repository per top-level package family, not one per nested adapter.
- Move `pkg/<family>` to the root of `go-<family>`.
- Remove `pkg/` from all public import paths.
- Preserve intentional nested modules for optional adapters and integrations.
- Exclude `secret-store` and its nested modules for now.
- Map `pkg/rabbitstream` to `github.com/faustbrian/go-rabbitmq-streams`.
- Use the final audited repository and module counts; fail on any unexplained
  difference from the migration manifest.
- The first valid release of every root and nested module MUST be `v1.0.0`.
- Nested modules MUST use path-prefixed tags such as
  `adapters/gokafka/v1.0.0`.
- Do not publish prereleases.
- Do not preserve compatibility with unused legacy standalone implementations.
- Do not weaken coverage, mutation, security, documentation, conformance, or
  performance gates during extraction.
- Commit coherent migration batches regularly.

## Canonical Source

Before extraction:

- Fetch and identify the canonical `github.com/faustbrian/golib` `main` commit.
- Confirm required CI is fully green for that exact commit.
- Record the commit and CI run in a private migration manifest.
- Refuse to extract from an unverified or unexpectedly dirty source tree.
- Preserve unrelated work.
- Use task-owned disposable `GOCACHE`, `GOMODCACHE`, Docker resources, and
  temporary directories. Never clear or depend on shared global caches.

## Preflight Inventory

Generate a machine-readable migration manifest covering:

- Every top-level package family and destination repository.
- Every root and nested Go module.
- Releasable and intentionally non-releasable modules.
- Internal package and cross-repository dependencies.
- The module-level dependency DAG and release waves.
- Repository-level cycles that affect first-release bootstrapping.
- Existing destination branches, tags, releases, descriptions, topics,
  rulesets, settings, and default branches.
- Public versions already recorded by `proxy.golang.org` and `sum.golang.org`.
- Source, fixtures, specifications, licenses, notices, generated artifacts,
  benchmarks, examples, test utilities, and documentation owned by each family.
- Every module or package depending on the excluded `secret-store` family.

Fail if any source package, module, dependency, file, destination, or release
unit is unaccounted for.

## Public Version Safety

Deleting GitHub tags or releases does not remove versions cached by Go proxies
or the checksum database. Before assigning `v1.0.0`:

- Query the public Go proxy and checksum database for every target module path.
- Detect existing immutable versions and content collisions.
- Never move, overwrite, or republish a cached path/version with new content.
- Produce an explicit collision-resolution map before publication.
- Use a new legitimate module/repository path where a clean `v1.0.0` cannot be
  published safely.
- Treat unresolved proxy or checksum collisions as release blockers.

## Legacy Repository Replacement

For every destination repository:

- Record legacy remote refs, releases, settings, and repository metadata in the
  private migration audit.
- Treat all existing code, branches, tags, releases, and history as disposable.
- Do not merge legacy standalone history into canonical history.
- Reconstruct history only from the relevant `golib/pkg/<family>` history.
- Rewrite the package directory to the repository root.
- Add standalone infrastructure through auditable migration commits.
- Replace remote `main` with canonical reconstructed history.
- Delete obsolete remote branches, tags, and GitHub releases.
- Perform destructive operations and force updates only under applicable safety
  rules and immediately required approval.
- Verify that only intended canonical refs and releases remain afterward.

## Module Rewriting

- Rewrite `github.com/faustbrian/golib/pkg/<family>` to
  `github.com/faustbrian/go-<family>`.
- Rewrite nested modules to
  `github.com/faustbrian/go-<family>/<nested-path>`.
- Use `github.com/faustbrian/go-rabbitmq-streams` for RabbitMQ Streams.
- Rewrite imports, examples, docs, generated files, API baselines, fixtures,
  benchmarks, and interoperability tools.
- Remove workspace-only `v0.0.0` requirements.
- Remove committed `replace` directives, pseudo-versions, absolute paths,
  sibling checkout assumptions, and monorepo-relative paths.
- Preserve nested module boundaries so optional adapters do not burden root
  consumers.
- Add a repository-local `go.work` only when multiple retained modules need it
  for local development. Release verification MUST use `GOWORK=off`.
- Keep dependency versions deterministic and current without inventing
  unpublished public versions.

## Standalone Foundation

Every repository MUST contain an appropriate standalone foundation:

- `README.md`, `LICENSE`, `CHANGELOG.md`, `CONTRIBUTING.md`, `SECURITY.md`,
  `CODE_OF_CONDUCT.md`, `SUPPORT.md`, `COMPATIBILITY.md`, and `DEPRECATION.md`.
- `AGENTS.md` and a `CLAUDE.md` pointer to canonical instructions.
- `NOTICE` and third-party notices where legally required.
- `.go-version` pinned to the repository-standard Go 1.26 patch release.
- `.gitignore`, `.gitattributes`, strict lint, gitleaks, and spelling configs.
- A root `Makefile` and repository-local validation tooling.
- Complete module, package, release, and dependency manifests.
- Dependabot configuration, pull-request template, and useful issue templates.
- API compatibility baselines.
- Specification provenance and fixtures where applicable.
- Owned benchmarks, examples, generated artifacts, and test utilities.

No repository may call scripts or consume configuration that exists only in
the former monorepo.

## Documentation Contract

Every repository MUST provide:

- A precise purpose, scope, installation guide, and first-use example.
- Complete package-level and exported-symbol Go documentation.
- Detailed user-facing API and adoption documentation.
- Examples for all major scenarios that compile as documented.
- Migration guidance from prior packages or Laravel/PHP where relevant.
- Supported and intentionally unsupported behavior.
- Security, trust-boundary, concurrency, lifecycle, cancellation, retry, and
  error semantics.
- Compatibility guarantees and deprecation policy.
- FAQ and troubleshooting guidance.
- Guidance for composing related `golib` repositories.
- Honest performance comparisons with equivalent workloads and methodology.
- Specification compliance, extension support, and ambiguity decisions where
  applicable.
- Accurate workflow, CodeQL, coverage, mutation, docs, Go version, and release
  badges that point to executable standalone resources.

Documentation MUST NOT reference monorepo-only paths, obsolete repositories,
or unpublished versions as though they were available.

## CI And Hardening

Each repository MUST have one authoritative root GitHub Actions workflow that
runs the complete repository contract for every pull request and every push to
`main`. It MUST cover every root and nested module and include, where relevant:

- Formatting and generated-file drift.
- `go mod tidy` cleanliness, build, tests, and race detection.
- Exact meaningful 100% production statement coverage.
- Exact 100% viable mutation score, excluding only proven equivalent or
  non-viable mutants through an audited policy.
- Strict linting, Staticcheck, `go vet`, and advisory NilAway.
- Vulnerability scanning, Gitleaks, dependency policy, license checks, and SBOM.
- CodeQL using a build strategy that covers build-tagged production code.
- Fuzz smoke tests and retained regression corpora.
- Specification conformance and interoperability suites.
- Documentation and API compatibility validation.
- Reproducible benchmarks and regression budgets.
- Docker-backed integration tests where required.
- Release dry-run and release-readiness validation.

Required gates MUST NOT use bypass flags, reduced thresholds, hidden package
exclusions, path-trigger gaps, or advisory conversion. Evidence MUST be
content-addressed, recorded immediately and atomically, and reusable after a
history rewrite when the exact code and gate inputs are unchanged.

CI MUST expose one stable aggregate required check. Any bootstrap mechanism
used before first publication MUST be checksum-pinned, temporary, auditable,
and incapable of replacing final verification through the public Go proxy.

## GitHub Configuration

For every repository:

- Set a precise description and relevant Go/domain/protocol/storage topics.
- Configure `main` as the default branch.
- Configure branch protection or rulesets around the stable required check.
- Enable dependency graph, vulnerability alerts, Dependabot, CodeQL, secret
  scanning, and push protection where supported.
- Disable obsolete workflows, integrations, and environments.
- Ensure workflow badges refer to real root workflows and stable jobs.
- Confirm visibility, license metadata, issue settings, and security policy.

## Cross-Repository Verification

Before public publication:

- Build every release artifact into a deterministic local Go module proxy.
- Resolve and test modules in dependency-DAG order with `GOWORK=off`.
- Prove no module requires a sibling checkout or local `replace`.
- Prove no source, docs, fixtures, generated files, or examples reference
  `github.com/faustbrian/golib/pkg`.
- Run cross-repository interoperability and composition tests.
- Verify examples and clean installation from temporary external consumers.
- Verify checksums and artifacts are deterministic.
- Migrate valid mutation evidence by exact content/gate-input identity instead
  of rerunning unchanged packages merely because paths or Git history changed.

Repository-level dependency cycles MAY require a temporary checksum-pinned
bootstrap proxy for exact-head CI before the initial tags. Such a proxy MUST be
removed from the final proof. Final release proof MUST resolve official
`v1.0.0` versions from public infrastructure only.

## Release Process

Release in dependency-DAG waves:

1. Prepare and verify every repository locally.
2. Push canonical `main` for the next dependency wave.
3. Wait for CI and CodeQL on the exact pushed commit.
4. Require every repository and module release-readiness gate to pass.
5. Create signed or annotated `v1.0.0` tags without moving them later.
6. Create path-prefixed `v1.0.0` tags for nested modules.
7. Create GitHub Releases with changelog, SBOM, provenance, and attestations.
8. Verify public proxy availability and checksum identity.
9. Update dependants to official published versions.
10. Continue only when the completed wave is publicly consumable.

Never create a release tag before exact-head CI is green. Never publish a
prerelease. Never move a published tag.

## golib After Extraction

Retain `github.com/faustbrian/golib` as a source-free ecosystem coordination
repository owning:

- The canonical repository/module catalogue and dependency graph.
- Cross-repository integration and interoperability tests.
- Local workspace generation.
- Release orchestration and verification.
- Shared repository templates and template-drift detection.
- Ecosystem documentation, package selection, and composition guidance.

Remove production package source from `golib/pkg` only after every applicable
standalone repository is pushed, green, released, and publicly consumable.

## Completion Criteria

The goal is complete only when:

- Every audited package family has exactly one correctly named repository.
- Every releasable root and nested module has a valid standalone module path.
- `secret-store` is excluded and no released module depends on it.
- Legacy standalone code, branches, tags, releases, and history are removed.
- Public proxy collisions are resolved without violating module immutability.
- Every repository is independently buildable and maintainable.
- Every repository has complete licensing, governance, documentation, CI,
  CodeQL, security, and hardening foundations.
- Every required CI job passes on each exact final `main` commit.
- Every production package has meaningful 100% coverage and 100% viable
  mutation strength.
- Cross-repository verification passes without workspace or replacement help.
- Every releasable root and nested module has a valid `v1.0.0` release.
- Clean consumers install every module from the public proxy.
- The source-free `golib` coordination repository accurately represents the
  released ecosystem.
- A final audit records repository mapping, source commits, final commits,
  release tags, CI and CodeQL URLs, proxy checksums, metadata, and any remaining
  risks, and confirms there are no unaccounted packages or modules.

## Required Final Report

Report:

- Canonical source commit and CI run.
- Complete source-to-repository and module-to-tag mapping.
- Legacy refs/releases removed per repository.
- Final `main` commit, required CI run, and CodeQL state per repository.
- Published tags, GitHub Releases, SBOMs, provenance, and attestations.
- Public proxy and checksum verification per module.
- Coverage, mutation, conformance, interoperability, and benchmark status.
- Any excluded package, unresolved limitation, waiver, or residual risk.
- Confirmation that `golib/pkg` removal happened only after all release gates.
