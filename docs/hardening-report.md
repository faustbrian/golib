# Repository hardening report

Audit date: 2026-08-24

## Scope and status

This report covers the normalization and strict hardening of the complete
`github.com/faustbrian/golib` multi-module repository. The authoritative
catalog currently contains 142 modules, of which 111 are independently
releasable, and 697 Go packages, of which 552 are production packages. Exact
statement coverage applies to the 550 production packages with executable
statements; declaration-only production packages remain cataloged but have no
statement denominator.

The repository migration, manifests, canonical module paths, root workspace,
root command surface, unified CI workflow, governance, and strict gate policy
are verified for the current 142-module catalog. All 111 independently
releasable modules also have current `v1.0.0` rehearsal and clean-consumer
proof. Public release readiness remains **not established** because the
separate operational-assurance, source-documentation, comparative-performance,
and public-release programs remain incomplete. This report is normalization
and local release-rehearsal evidence, not release approval.

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
| Go | 1.26.6 |
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
| ripgrep | 15.2.0 checksum-pinned Linux archive in CI |
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
make workspace-test
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
its uploaded evidence, not by a repository-wide average. The Kafka and Verkle
Tree specialist campaigns have reached their current boundaries and their
independently produced evidence is consumed here without restarting unaffected
content-identical campaigns. Verkle Tree's separate cryptographic-security goal
remains future work and therefore no security-audit, production-suitability,
stable-v1, or Ethereum-compatibility claim is made for that module.

Current local goal audits verify all 104 goal-bearing modules. The final owned
module, `pkg/search/adapters/opensearch`, has current input-bound evidence for
all 21 canonical gates, including its real-cluster test, race, exact 1908/1908
statement coverage, fuzz, retained 1377/1377 mutation result, conformance,
interoperability, and comparative benchmarks.

The current root catalog validates 142 module records, 697 package records,
and cohesion policy for all 111 releasable modules. OpenSearch specification
governance and its security, compatibility, recovery, and operations evidence
are complete. All 104 goal-bearing modules now have current goal-traceability
records; Kafka and Verkle Tree retain their documented bounded support and
security non-claims.

## Prior CI proof and current verification

The latest complete package-matrix run
[`32704788074`](https://github.com/faustbrian/golib/actions/runs/32704788074)
passed at prior revision `5464cb0f14a4beae3eba1a155571e35bb22be631`. It
contains 141
attributable active-module jobs plus module selection, the repository contract,
CodeQL, Kafka Linux arm64, and the fail-closed `Required` summary, for 146
successful jobs. The catalog's 142nd module,
`pkg/analysis/testdata/coverage`, is an explicitly non-production fixture with
no applicable gates and is intentionally excluded from CI selection. Every
selected module uploaded its own durable evidence artifact.

Content-addressed mutation checkpoints were reused only when their complete
gate-input fingerprints and verifier identities matched or an exact reviewed
identity migration applied. Release-rehearsal artifacts use a separate
namespace, and restoration scans trusted prior verification artifacts from
newest to oldest so a partial or stale run cannot shadow reusable proof. The
formerly timing-sensitive `pkg/concurrency-limit` campaign restored and reused
its exact checkpoint in that run rather than executing again. The subsequent
additive JSON-RPC typed-dispatch change requires a replacement complete matrix;
the first run for that change failed closed because its source-documentation
manifest was stale. The manifest and exact operational-assurance digest
migrations are corrected in the current tree, but no replacement CI result is
claimed here yet.

## Release rehearsal proof

The prior workflow-dispatch run
[`32697157280`](https://github.com/faustbrian/golib/actions/runs/32697157280)
passed at `fe2fe4f309bf4d88303ba53c71382cb062498482`. All 111 releasable
modules passed in independently attributable jobs, alongside module selection,
the repository contract, and the fail-closed `Required` summary, for 114
successful jobs and 111 uploaded release artifacts. It is now superseded for
release-readiness purposes because `pkg/jsonrpc` production source changed
after that revision. A replacement 111-module rehearsal is required after the
current complete matrix passes. CodeQL and Kafka arm64 were intentionally
skipped because the rehearsal consumes the normal matrix rather than
duplicating it.

Each module planned its required initial directory-prefixed `v1.0.0` tag,
validated dependency-first owned-module ordering, passed isolated tidy, test,
and API checks, built a deterministic task-owned local source proxy, and
resolved its public package from a clean external consumer with `GOWORK=off`
and no module replacement. The rehearsal created no tag and published no
artifact.

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

Release readiness is currently **not established**. Operational assurance is
`not ready`: 2 of 11 scenarios pass and five residual risks remain open. All
111 releasable modules now have dependency-ordered local `v1.0.0` rehearsal
and deterministic clean-consumer proof. Release readiness still requires the
outstanding operational-assurance scenarios and risks, completion of the
source-documentation and comparative-performance programs, public proxy and
checksum resolution, signed tags and provenance, and explicit release
authority. No tag or public release was created.
