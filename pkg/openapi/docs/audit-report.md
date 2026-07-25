# Audit and release evidence

This report closes the evidence-driven hardening audit performed through
2026-07-22. The audited implementation and workflow revision is
`c659f0804079fc6d5e420130097119d7d21883e8`. Later report or benchmark-evidence
commits do not change the audited production behavior.

The labels in this report are intentional:

- **Specification requirement** comes from pinned normative text or an
  incorporated standard.
- **Observed fact** is established by current source, fixtures, or executable
  evidence.
- **Package policy** is a documented restriction or guarantee selected by this
  module.
- **Inference** is a conclusion from those requirements, facts, and policies.

## Scope and traceability

**Observed fact:** The repository contains 34 production and internal Go
packages, 149 Go test files, 27 fuzz targets, 23 benchmarks, and 12 selected Go
module dependencies. The generated normative ledger contains 2,381 requirements
and the object-field inventory contains 1,493 rows. Every normative row has an
`implemented` evidence row linking implementation, test, documentation, and a
review note. There is no coverage exclusion for production behavior.

| Document line | Normative rows |
| --- | ---: |
| Swagger 2.0 | 73 |
| OpenAPI 3.0.0 | 213 |
| OpenAPI 3.0.1 | 211 |
| OpenAPI 3.0.2 | 212 |
| OpenAPI 3.0.3 | 220 |
| OpenAPI 3.0.4 | 269 |
| OpenAPI 3.1.0 | 217 |
| OpenAPI 3.1.1 | 286 |
| OpenAPI 3.1.2 | 302 |
| OpenAPI 3.2.0 | 378 |

The clause-level record is
[`specification/conformance/evidence.tsv`](../specification/conformance/evidence.tsv).
Object and field presence is separately generated in
[`specification/conformance/object-fields.tsv`](../specification/conformance/object-fields.tsv).
Accepted errata and ambiguous-source decisions are isolated in
[`specification-decisions.md`](specification-decisions.md).

**Specification requirement:** Normative prose takes precedence over official
schemas, examples, registries, and independent behavior.

**Observed fact:** [`specification/manifest.json`](../specification/manifest.json)
pins every imported artifact by immutable revision or retrieval date, source,
license, license source, destination, and SHA-256. The offline provenance gate
rejects changed, missing, duplicated, escaping, symlinked, or non-regular
artifacts. The synchronization script downloads to temporary paths, verifies
the expected digest, and replaces the destination only after verification.

**Inference:** Official schemas and popular descriptions can expose a defect,
but neither can silently redefine a normative requirement or be modified to
make a test pass.

## Findings and corrections

The audit found and corrected defects that aggregate coverage and self-round
trips did not expose. The complete user-visible list is in
[`CHANGELOG.md`](../CHANGELOG.md). Material classes were:

- strict JSON Unicode identity, duplicate-key handling, JSON-equivalent YAML,
  cancellation during YAML syntax construction, exact numbers, and parser
  sentinel overflow;
- pre-allocation limits for parsing, semantic values, parameters, schemas,
  validation, references, composition, diffing, conversion, serialization,
  diagnostics, generated evidence, and direct `jsonvalue.Value` marshaling;
- URI, JSON Pointer, reference identity, legal-cycle termination, single-flight
  schema compilation, cancellable waiters, and validator-owned schema limits;
- default-deny file and HTTP resolution, post-symlink containment, IANA
  special-purpose address denial, DNS-pinned dialing, redirect reauthorization,
  credential stripping, disabled ambient proxy and decompression, media-type
  enforcement, cumulative budgets, cleanup, and sensitive error redaction;
- version-isolated model and Schema Object behavior, explicit conversion loss,
  directional request and response compatibility, bounded resolved comparison,
  and stable deterministic diagnostics;
- complete mutation shards, active adversarial fuzz targets, exact production
  coverage, dependency and license inventories, reproducible performance
  budgets, pinned interoperability tools, and public ecosystem descriptions.

**Package policy:** Parsing and validation perform no implicit file or network
access. Ambiguous syntax is rejected. Conversion, composition, dereferencing,
and generation never hide known loss. All untrusted traversals require explicit
bounds; documented defaults are conservative, while reviewed large inputs may
use isolated caller-selected bounds.

## Security, concurrency, and lifecycle outcome

**Observed fact:** The threat model in [`security.md`](security.md) enumerates
malicious syntax, schemas, references, servers, files, amplification, injection,
secret reflection, integer overflow, cancellation, race, and lifecycle risks.
Failure-injection, fuzz, race, cancellation, deterministic-parallel, ownership,
file, response-body, timer, and temporary-file tests exercise the applicable
boundaries. Parsed values and generated models are immutable; returned
collections and diagnostics do not expose mutable internal storage. Callbacks
are not deliberately invoked while package locks are held.

**Inference:** Known amplification and authority paths are bounded under the
documented options. Aggregate process safety still requires callers to bound
simultaneous top-level operations, retain appropriate deadlines, authorize only
necessary roots and hosts, and contextually escape preserved rich text.

## Interoperability and performance outcome

**Observed fact:** The isolated interoperability module pins exact versions and
complete checksums for `getkin/kin-openapi`, `pb33f/libopenapi`, and applicable
Swagger packages. It tests matched parse, model, validate, and round-trip
surfaces. Unmodified, license-preserved Swagger Petstore and GitHub REST API
descriptions add public scale evidence with exact expected diagnostic classes.
Differences and implementation limitations are classified in
[`interoperability.md`](interoperability.md); no competitor result is treated as
authority.

**Observed fact:** The performance suite covers tiny, typical, large, invalid,
deep, schema-heavy, reference-heavy, cyclic, warm, cold, file, and loopback HTTP
workloads. It records latency, throughput, allocations, peak process memory,
and semantic assertions. Blocking budgets use allocation counts rather than
host-dependent wall time. Methodology and raw evidence are in
[`performance.md`](performance.md).

## Verification result

Fresh final verification on Darwin arm64 with Go 1.26.5 used these commands:

| Command | Result |
| --- | --- |
| `make check-all` | Passed |
| `make fuzz` | Passed all 27 short campaigns |
| `make interoperability` | Passed pinned matrix and public corpus |
| `make performance` | Passed every semantic and allocation budget |
| `make benchmark` | Passed all 23 benchmarks |
| `make mutation MUTATION_PATH=./validate MUTATION_TIMEOUT_COEFFICIENT=60` | Passed with no unresolved mutant |
| `make mutation MUTATION_PATH=./internal/specification/cmd/specmatrix MUTATION_INTEGRATION=true` | Passed with 21 killed and no unresolved mutant |
| `GOOS=linux GOARCH=amd64 go build ./...` | Passed |
| Windows test-binary cross-compilation for every package | Passed |

`make check-all` includes format, tidy, vet, unit, example, race, generated,
conformance, provenance, API compatibility, exact 100% package and aggregate
statement coverage, strict golangci-lint, Staticcheck, visible advisory NilAway,
govulncheck, dependency, license, performance, and workflow checks. The CI
workflow repeats the test suite on Linux, macOS, and Windows; a failure on a
pushed revision remains a release blocker. The only conditional skips are two
Windows symlink-containment cases when the host denies symlink creation, as
documented in the public limitations.

## Release determination and residual limits

**Observed fact:** No local release gate, normative ledger row, official
artifact, mutation, coverage branch, known security finding, or documented
implementation finding remains unresolved at the audited revision.

**Inference:** The audited tree satisfies the package's stated release gates.
This conclusion is revision-bound and falsifiable: fixture drift, a new
normative correction, a failing cross-platform job, a vulnerability, or a new
adversarial counterexample reopens the corresponding gate.

Deliberate scope and evidence limits are documented in the README. In
particular, Overlay and Arazzo are separate specifications; remote authority is
caller-granted; rich text is preserved rather than sanitized; impossible
cross-version conversions report loss; public descriptions are not normative;
and finite tests, fuzzing, mutation, and differential comparison are strong
evidence rather than mathematical proof.
