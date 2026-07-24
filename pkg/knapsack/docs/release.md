# Release and compatibility policy

The initial minimum language and toolchain is Go 1.26, controlled by the
repository-wide version files. Public types, typed errors, canonical encoding,
objective semantics, coordinates, and proof statuses are contracts.

Every change must pass `make check`. A workspace release candidate must pass
`make release-check`, which adds complete mutation execution, benchmark
budgets, deterministic CycloneDX SBOM generation, and reproducible source
artifact verification. A public version tag must pass `make publish-check`,
which additionally rejects local replacements and placeholder dependency
versions. `make check` includes secret scanning. NilAway runs in
its own explicitly warning-only CI job until every finding is classified and
its signal is approved; `make nilaway` preserves the analyzer exit status.
Release evidence also includes a
clean-checkout run, corpus provenance, dependency and vulnerability review,
documentation, examples, race, fuzz, and meaningful 100% production statement
coverage. The `make leak` gate repeatedly cancels both solvers under the race
detector and proves production packages contain no unmanaged goroutine launches.
The `make fuzz` gate uses reviewed exact execution counts from
`specification/fuzz-budgets.tsv`; CI multiplies them by five and the protected
release workflow multiplies them by fifteen without wall-clock shutdown races.
Both workflows pin external actions to reviewed commit hashes. The ordinary
gate runs actionlint and a source-level test rejects mutable action tags or
undocumented revisions.

Source archives pin the most recent commit that changed the module before
archiving. This keeps their commit timestamp deterministic when unrelated
monorepo siblings advance concurrently. The reproducibility gate also rejects
unstaged or staged module changes before selecting that snapshot.

The current source archive is reproducible but is intentionally workspace-only:
`go.mod` resolves `math` and `measurement` through local `v0.0.0`
replacements. Archive reproducibility therefore certifies the monorepo source,
not an isolated consumer build. Publishing and pinning those sibling modules
remains a prerequisite for a public version tag and is enforced separately.

`make dependency-review` compares every compiled non-standard module with
`specification/dependency-licenses.tsv`, pins each license hash and SPDX
classification, and separately records whether the resolved version is ready
for workspace or public use. It fails for missing or changed licenses and
accepts explicitly recorded workspace-only replacements.
`make dependency-publish-review` fails closed for those replacements. Both
current sibling dependencies have pinned MIT license evidence and remain
intentionally workspace-only.

Performance or quality claims require raw evidence with the machine, Go
version, execution revision, complete input fingerprint, seed, fixture hash,
constraints, objective,
verification, work, and allocations. Unsupported reference behavior must be
removed from both sides or reported separately.

Canonical encoding changes require a new version and migration documentation.
A released decoder must retain its current and immediately previous schema.
Initial v1 has no predecessor; its hashed request and plan fixtures establish
the N-1 contract that the first schema transition must continue to decode.
A heuristic improvement may change placements while preserving semantics;
consumers requiring identical bytes must pin module version, normalized
request, options, and seed.
