# Dependency audit

The machine-readable [dependency inventory](dependencies.tsv) records every
module in the selected Go build list, including modules that are present only
because an upstream `go.mod` declares them. The inventory was reviewed on
2026-07-22.

`runtime` means at least one package from the module occurs in `go list -deps
./...`. `graph-only` means no package from the module occurs in that package
closure. Graph-only modules do not ship code in this library, but remain listed
because Minimal Version Selection can make their versions relevant to future
imports and upgrades.

The audit records the pinned version, SPDX license, version-specific license
and source URLs, owner, observed maintenance state, release mechanism,
necessity, and replacement strategy. Versions and module checksums are
authoritative in `go.mod` and `go.sum`; `go mod verify` checks downloaded bytes.
The license gate independently scans the packages that are actually built.

## Update procedure

1. Change dependencies with `go get` or `go mod tidy`, never by editing
   `go.sum`.
2. Run `make dependency-audit`. It rejects missing, unexpected, duplicate,
   incomplete, or version-drifted inventory rows.
3. Re-run `go mod why -m` and `go list -deps ./...` to confirm necessity and
   classification.
4. Review the pinned source and license, ownership, release activity,
   vulnerability advisories, transitive graph, and replacement strategy.
5. Run `make vuln dependencies license` before committing.

The only inactive project is `gopkg.in/check.v1`. It is a graph-only test edge
declared by `go.yaml.in/yaml/v3`; no package from it is compiled into or tested
through `openapi`. Its removal is controlled by the upstream YAML module.

`github.com/dlclark/regexp2/v2` permits backtracking expressions. It is used by
the sibling JSON Schema implementation for ECMAScript compatibility, so schema
evaluation must retain its explicit execution limits. Replacing it requires an
ECMAScript-compatible engine and full JSON Schema conformance evidence; a
feature-narrowing substitution is not acceptable.
