# Goal: Maintain Long-Term Monorepo Cohesion

## Mission

Keep `golib` coherent over years of independent package evolution
without turning it into one tightly coupled framework or allowing duplicated
rules, tooling, and dependencies to drift.

## Canonical Governance

- Root `AGENTS.md` is the canonical engineering policy.
- Package instructions contain only real package-specific additions.
- `CLAUDE.md` points to canonical policy rather than copying it.
- Root tooling and CI configuration are authoritative.
- Package goals remain product contracts and are never discarded after
  implementation.
- Every implementation change updates the affected module changelog.

## Module Lifecycle

Maintain a machine-readable catalog containing owner, purpose, lifecycle,
module type, license, version, dependencies, release status, and goals.
Define criteria for planned, experimental, stable, deprecated, and archived
modules.

New modules require evidence that the concern is reusable, independently
versionable, not already covered, and worth the release and maintenance cost.
Merges or removals require consumer and compatibility analysis.

## Drift Prevention

Continuously detect:

- undeclared modules and missing catalog entries;
- stale workspace membership;
- forbidden old import paths and local replacements;
- Go version and tool-version divergence;
- duplicated or contradictory lint rules;
- inconsistent licenses, changelogs, badges, READMEs, security files, and
  package documentation;
- dependency cycles and layering violations;
- package-local workflow files that are not executed;
- stale generated API baselines, schemas, fixtures, and conformance matrices;
- growing suppressions, warnings, flaky tests, and skipped checks;
- excessive CI cost or release latency.

## Dependency And Boundary Review

Review the owned dependency graph regularly. Foundational modules MUST not
depend on higher-level service or integration packages. Optional adapters MUST
remain additive. Shared packages MUST not become dumping grounds, service
containers, global registries, or hidden framework magic.

## Maintenance Cadence

Define recurring work for dependency updates, supported Go releases,
vulnerability review, fixture/specification updates, fuzzing, mutation tests,
benchmarks, API baselines, documentation audits, stale issue review, and
deprecation cleanup.

Automate routine evidence gathering but retain human review for API design,
security, specification interpretation, package boundaries, and releases.

## AI Cohesion

Keep enough root context for coding agents to understand package boundaries,
commands, quality rules, and current decisions without duplicating entire
documents into every package. Add executable drift checks wherever a written
rule can be enforced mechanically.

## Completion Criteria

This goal is complete when catalog and workspace state are authoritative,
drift checks cover all standardized metadata and tooling, package boundaries
remain acyclic and intentional, recurring maintenance is documented and
runnable, and adding or changing a module cannot silently bypass ecosystem
standards.
