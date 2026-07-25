# Security and threat model

## Trust boundaries

The analyzer binary, pinned Go toolchain, checked-in policy, and release
workflow are trusted. Target repositories, Go source, generated headers,
package metadata, build tags, configuration input, and report consumers are
potentially untrusted. The tool has the invoking user's filesystem permissions;
it is not a sandbox and must not be run with broader credentials than needed.

`analysis` parses and type-checks target code through Go tooling. It never
executes target binaries, package initializers, tests, generators, or arbitrary
configuration programs. The project does not load analyzer or configuration
plugins. The optional golangci-lint module plugin remains unshipped while its
lifecycle is outside the reproducible compatibility contract.

## Threats and controls

| Threat | Control | Residual risk and operation |
| --- | --- | --- |
| Target-code execution | Analysis uses syntax, types, SSA, CFGs, and package metadata only; no target `go run`, test, generator, or plugin execution | Go package loading may invoke the Go command and read module metadata; run in a least-privilege checkout with an explicit module-download policy |
| Configuration injection | Strict single-document YAML decoding rejects unknown keys and executable configuration | A valid policy can intentionally weaken rules; policy changes require code review and compatibility review |
| Path traversal or disclosure | Configuration and report paths are cleaned, repository-relative, and rejected when they escape the analysis root | Package names and relative paths remain sensitive repository metadata |
| Source or secret disclosure | JSON and SARIF omit source snippets and diagnostic traces; messages use governed metadata rather than source values | A filename, package, rule, or suppression reason may reveal design information; protect reports like source artifacts |
| SARIF or JSON injection | Standard encoders escape untrusted strings; stable schemas and path validation precede emission | Downstream renderers remain separate trust boundaries and must stay patched |
| Forged generated code | Exclusion requires explicit policy, an exact trusted repository-relative path, and a recognized generated header before the package clause; unlisted forged headers and their suppressions remain analyzed | A reviewed generator can still emit unsafe code at an authorized output path; generated output remains subject to generator and supply-chain review |
| Suppression bypass | Directives require an exact known rule, adjacent diagnostic, non-empty reason, unique location, valid optional expiry, and matching finding | A semantically poor but syntactically valid reason requires human review; inventories support audit and trend checks |
| Resource exhaustion | Configuration input, diagnostics, suppressions, SSA traces, static fan-out proofs, corpus entries, and benchmark budgets are bounded | Extremely large valid packages still consume parser and type-checker resources; CI timeouts and representative corpus budgets remain required |
| Dependency or release compromise | Dependencies and tools are pinned, actions use commit SHAs, releases are reproducible and checksummed, and publication has scoped permissions | Consumers must verify checksums and signatures when available and retain independent dependency, vulnerability, and CodeQL gates |
| Advisory escalation | Rule metadata defaults to advisory; configured reporting separates severity from blocking status; NilAway runs separately with visible advisory status | Raw multichecker and vettool execution use Go vet exit semantics, so use configured `check` when advisory status must be preserved |

## Report handling

Reports intentionally contain no source snippets, values, data-flow traces, or
absolute repository paths. Retain JSON, SARIF, exception, and suppression
inventories only as long as required by the organization's engineering and
security evidence policy. Do not publish private report artifacts merely
because they are machine-readable.

## Supply-chain verification

Release verification builds every archive twice, compares bytes, and emits a
SHA-256 checksum manifest. Publishing verifies the pushed tag and grants write
permission only to the release job. Consumers should pin an exact version and
checksum, use the same artifact locally and in CI, keep gosec, govulncheck and
CodeQL independently enabled, and sign or verify provenance when the release
environment provides a trusted identity.

The blocking CI workflow runs the pinned CodeQL Go query suite with a reviewed
manual `go build -trimpath ./...` step. Go does not support CodeQL's `none`
mode; explicit compilation gives CodeQL complete production-package evidence
without running target binaries, package initializers, tests, generators, or
arbitrary build scripts. Its job receives only read access to repository
contents and write access to code-scanning results. Local release-equivalent
gates continue to run the enabled gosec integration in the pinned
golangci-lint binary and the separately pinned govulncheck command without
hiding either authority behind this project's diagnostics.

`make workflow-policy` tests and enforces the workflow trust boundary locally.
Every external action must use a full commit SHA, every workflow must default
to read-only contents, and the only write scopes are CodeQL security results
and tagged-release publication in their dedicated jobs.

Security defects include target execution, path escape, source disclosure,
suppression bypass, unbounded attacker-controlled analysis, report injection,
and artifact-integrity failures. Follow [the private reporting process](../SECURITY.md)
without attaching proprietary source or diagnostic output.
