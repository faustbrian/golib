# Repository standards

- Minimum Go is the exact stable version pinned in `.go-version` and `go.mod`.
- Dependencies and tools use reviewed versions; workflow actions use immutable
  commit SHAs.
- Formatting, tidy, vet, lint, Staticcheck, tests, race, fuzz, coverage,
  mutation, conformance, docs, vulnerabilities, workflows, and benchmarks are
  reproducible through `make` targets.
- Production code may not use `unsafe`, cgo, or `go:linkname`.
- Core packages perform no implicit I/O and install no global mutable state.
- Conventional commits include a rationale body; release notes come from the
  changelog.
- Security reports remain private until coordinated disclosure.
- Licenses and provenance are reviewed whenever code, specification inputs, or
  dependencies change.

Repository settings should enable vulnerability alerts, dependency review,
secret scanning, signed releases, protected branches, required reviews, and
the blocking workflow jobs documented here.
