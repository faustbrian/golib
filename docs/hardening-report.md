# Repository hardening report

Audit date: 2026-08-02

## Scope and status

This report covers the normalization and strict hardening of the complete
`github.com/faustbrian/golib` multi-module repository. The authoritative
catalog currently contains 119 modules, of which 94 are independently
releasable, and 654 Go packages, of which 521 are production packages. Exact
statement coverage applies to the 519 production packages with executable
statements; declaration-only production packages remain cataloged but have no
statement denominator.

The repository migration, manifests, canonical module paths, root workspace,
root command surface, unified CI workflow, governance, and strict gate policy
are implemented. Final release readiness remains **in progress** until every
module has current evidence, the aggregate repository check passes from the
final tree, and the authoritative GitHub Actions run is green. This report must
not be used as a release approval while that status remains in progress.

## Repository contract

| Property | Implemented contract |
| --- | --- |
| Canonical import root | `github.com/faustbrian/golib` |
| Layout | Public modules under `pkg/<concern>` with independent `go.mod` files and directory-prefixed tags |
| First public version | Stable `v1.0.0`; no prerelease or public `v0.x` baseline |
| Workspace | Root `go.work` for development; every module also verified with `GOWORK=off` |
| Coverage | Exact 100% per cataloged executable production package; no aggregate averaging |
| Mutation | Exact 100% viable mutants killed and exact 100% mutant coverage |
| CI | One authoritative `.github/workflows/ci.yml` with attributable module jobs and a fail-closed required summary |
| Evidence identity | Complete content fingerprints and log checksums; Git revisions are traceability metadata only |
| Release proof | Dependency-ordered local `v1.0.0` proxy, isolated module validation, and external clean-consumer resolution |

The machine-readable sources are `modules.json`, `packages.json`,
`specifications.json`, and `benchmarks.json`. `docs/goal-traceability.md` binds
each root and package goal to implementation artifacts and the canonical gate
set. `docs/quality.md`, `docs/ci.md`, and `docs/releases.md` define the enforced
coverage, mutation, CI-selection, and release behavior.

## Toolchain

The repository pins the following release-verification inputs in
`.golib/versions.env`:

| Tool or runtime | Version |
| --- | --- |
| Go | 1.26.5 |
| golangci-lint | 2.12.2 |
| Staticcheck | 0.7.0 |
| NilAway | `9fd1b8d7bac8` pseudo-version |
| govulncheck | 1.6.0 |
| Gremlins | 0.6.0 |
| Gitleaks | 8.30.1 |
| go-licenses | 2.0.1 |
| CycloneDX Go | 1.10.0 |
| actionlint | 1.7.12 |
| apidiff | `764159d718ef` pseudo-version |
| PostgreSQL image | `postgres:18.4-alpine` |
| Valkey image | `valkey/valkey:9.1.0-alpine` |
| Redis image | `redis:8.6.4-alpine` |
| NATS image | `nats:2.14.2-alpine` |
| NSQ image | `nsqio/nsq:v1.3.0` |
| RabbitMQ image | `rabbitmq:4.3.2-management-alpine` |

CI provisions additional pinned interoperability runtimes from catalog policy
rather than relying on host-global tools. Local execution documents the same
requirements in `docs/troubleshooting.md`.

## Verification commands

The final audit uses the repository-owned command surface rather than ad hoc
package commands:

```text
make inventory
make workspace-check
make repository-check
make ci
make release-dry-run MODULES=<module-or-selection>
```

`make ci` expands the complete catalog and runs each applicable canonical gate:
format, tidy, Go safety, vet, isolated tests, race, exact coverage, strict lint,
Staticcheck, vulnerability, secrets, licenses, SBOM, fuzz, exact mutation,
advisory NilAway, documentation, API compatibility, conformance,
interoperability, and benchmark verification. Each gate writes an atomic JSON
record and complete log under `.artifacts/<module>/evidence`, addressed by its
complete input fingerprint.

## Package-level outcomes

The final package-attributable result is represented by the module matrix and
its uploaded evidence, not by a repository-wide average. At this intermediate
checkpoint:

- 84 of 94 releasable modules have current clean-consumer release proof.
- The remaining release-proof set is limited to Kafka and its owned consumers,
  Knapsack and its Go Money adapter, and Verkle Tree.
- Every completed mutation campaign enforces 100% efficacy and 100% mutant
  coverage. Kafka's final integration-backed campaign is still running.
- Knapsack requires regenerated native, peak-RSS, and BoxPacker benchmark
  evidence against the final owned dependency content.
- Verkle Tree requires final evidence after its current stateless witness work
  stabilizes.

These counts are a progress checkpoint, not final evidence. Before release the
section will be replaced with the exact final matrix result and completion
timestamps from the final tree.

## Specification and interoperability evidence

Specification-bearing modules declare their normative sources, pinned corpora,
and independent implementations in `modules.json` and
`specifications.json`. The enforced scope includes JSON Schema official suites
and Bowtie, JSON:API, JSON-RPC, OpenAPI, OpenRPC, Test262 for ECMAScript regular
expressions, Ethereum trie implementations, RFC 9162 Merkle trees, Apache
Woden for WSDL, and the W3C XML Schema Test Suite through the pinned JAXP
environment. Missing runtimes, skipped fixtures, malformed reports, and empty
result sets fail rather than becoming warnings.

Raw conformance and interoperability outputs remain package-owned where the
upstream format is part of the proof. The root CI uploads each selected
module's evidence independently so one aggregate success cannot hide an
omitted or failed corpus.

## Exclusions and advisory findings

No coverage or mutation threshold exception is permitted. Generated or
declaration-only source may be excluded from executable statement accounting
only through explicit catalog classification. Equivalent or invalid mutants
require the exact, dated, expiring record described in `docs/quality.md`; no
blanket source, function, directory, or mutation-operator exclusion is an
accepted mechanism.

NilAway is the only intentionally advisory analyzer. It still executes and
publishes attributable findings under a no-regression policy, but it does not
replace any mandatory compile, race, coverage, mutation, lint, or security
gate.

## Release readiness

Release readiness is currently **not established**. It requires all 94
releasable modules to have current dependency-ordered `v1.0.0` dry-run and
clean-consumer proof, all root and package gates to pass from the final tree, a
clean worktree, and the final GitHub Actions matrix plus required summary and
CodeQL jobs to succeed. The report will record the exact final revision and CI
run only after those conditions are true.
