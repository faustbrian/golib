# Repository standards

- `make safety`, `make integration`, and `make check` match maintained CI gates.
- Production Go source satisfies GO-SAFETY-1: no `unsafe`, cgo, or
  `go:linkname`.
- Exact coverage instruments every module package and includes real PostgreSQL.
- Behavioral changes use red-green-refactor; database semantic claims use
  integration tests.
- Fuzz regressions remain checked into `testdata/fuzz`.
- Documentation, generated `llms.txt` bundles, changelog, compatibility, and
  security evidence change with public behavior.
- Commits are Conventional Commits with an explanatory body.
