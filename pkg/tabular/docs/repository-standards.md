# Repository Standards

This repository follows the shared maintenance baseline used by the
`faustbrian/go-*` OSS packages.

## Mandatory Root Files

Every repository contains `.gitattributes`, `.gitignore`,
`.golangci.yml`, `AGENTS.md`, `CHANGELOG.md`, `CLAUDE.md`,
`CODE_OF_CONDUCT.md`, `CONTRIBUTING.md`, `LICENSE`, `Makefile`, `NOTICE`,
`README.md`, `ROADMAP.md`, `SECURITY.md`, `THIRD_PARTY_NOTICES.md`,
`llms.txt`, and `llms-full.txt`.

AI planning and execution briefs live in `.ai/GOAL.md` and
`.ai/GOAL_HARDEN.md`, keeping internal agent material separate from the
package's public documentation surface.

`NOTICE` identifies project and inherited ownership. `THIRD_PARTY_NOTICES.md`
separately records detailed source provenance and third-party attribution.
Both remain present even when no additional third-party source requires
attribution. A package may retain a different approved OSS license when
provenance requires it.

## Mandatory Documentation

The shared taxonomy is lowercase kebab-case and includes a documentation
index, quickstart, adoption guide, API reference, architecture, examples,
cookbook, FAQ, troubleshooting, migration, compatibility, performance,
hardening, security, Go safety and concurrency, and releasing guide.
Package-specific documents extend this taxonomy without renaming shared
concepts.

## Mandatory Automation

Every repository provides pinned-SHA workflows for CI, benchmarks, scheduled
fuzzing, security, and tagged releases. CI tests Go 1.25.x as the supported
minimum line and current stable Go. Dependency review runs on pull requests;
reachable dependency scanning uses `govulncheck`.

The common Make interface is `format`, `format-check`, `test`,
`test-race`, `coverage`, `vet`, `lint`, `fuzz`, `benchmark`,
`safety`, `docs`, `vuln`, `check`, and semantic release targets.

The package family shares the `GO-SAFETY-1` baseline. It forbids `unsafe`,
cgo, and `go:linkname` in production code and standardizes ownership,
goroutine lifecycle, race, fuzz, resource-bound, leak, and benchmark evidence.

## Approved Package-Specific Differences

- `jsonapi` carries JSON:API feature, conformance, extension/profile,
  recommendation, and threat-model documentation.
- `jsonrpc` carries protocol conformance and middleware documentation.
- `queue` carries backend, delivery, lifecycle, failure, and integration
  documentation plus a live-backend integration workflow. Its fork provenance
  requires detailed third-party notices.
- `wire` carries format, dependency, and audit-evidence documentation.
- `tabular` carries format and ingest-limit documentation. It uses
  Apache-2.0 and retains XLS provenance notices.

Code, dependencies, fuzz targets, benchmark inputs, and domain-specific
security guidance are expected to differ. Shared policy wording and automation
structure must not drift without updating this contract across all affected
repositories.
